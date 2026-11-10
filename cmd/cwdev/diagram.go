package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// diagramSpec is one archify JSON specification and the SVG it produces.
type diagramSpec struct {
	path        string // absolute path of the specification
	diagramType string
	quality     string // meta.quality_profile, empty when the spec has none
	target      string // where the SVG is written or compared
}

// diagramTools are the executables an export needs.
type diagramTools struct {
	node, chrome, archify string
	root                  string
	timeout               time.Duration
	noSandbox             bool
}

const diagramUsage = "usage: cwdev diagram-svg [--root DIR] [--out FILE] [--check] [--timeout DURATION] SPEC.json | --all [--root DIR] [--check] [--timeout DURATION]"

// diagramSVG exports archify specs to the SVG the viewer's Export → SVG · Auto
// button downloads, or with --check reports committed SVGs that differ.
func diagramSVG(ctx context.Context, arguments []string) error {
	flags := flag.NewFlagSet("diagram-svg", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	all := flags.Bool("all", false, "export every docs/diagrams/*.json under --root")
	root := flags.String("root", ".", "repository checkout holding third_party/archify and docs/diagrams")
	out := flags.String("out", "", "write the SVG here instead of next to the spec")
	check := flags.Bool("check", false, "write nothing; fail when a target differs from a fresh export")
	timeout := flags.Duration("timeout", 3*time.Minute, "limit for each diagram's finalize and browser run")
	var positional []string
	for rest := arguments; ; {
		if err := flags.Parse(rest); err != nil {
			return fmt.Errorf("diagram-svg: %w; %s", err, diagramUsage)
		}
		if flags.NArg() == 0 {
			break
		}
		positional = append(positional, flags.Arg(0))
		rest = flags.Args()[1:]
	}
	switch {
	case *timeout <= 0:
		return fmt.Errorf("diagram-svg: --timeout must be positive; %s", diagramUsage)
	case *all && len(positional) > 0:
		return fmt.Errorf("diagram-svg: --all takes no spec path; %s", diagramUsage)
	case *all && *out != "":
		return fmt.Errorf("diagram-svg: --out cannot be used with --all; %s", diagramUsage)
	case !*all && len(positional) == 0:
		return fmt.Errorf("diagram-svg: a spec path or --all is required; %s", diagramUsage)
	case len(positional) > 1:
		return fmt.Errorf("diagram-svg: exactly one spec path is allowed, got %d; %s", len(positional), diagramUsage)
	}

	var paths []string
	if *all {
		matches, err := filepath.Glob(filepath.Join(*root, "docs", "diagrams", "*.json"))
		if err != nil {
			return err
		}
		if len(matches) == 0 {
			return fmt.Errorf("diagram-svg: no specs found in %s", filepath.Join(*root, "docs", "diagrams"))
		}
		paths = matches // Glob returns lexical order
	} else {
		paths = positional
	}
	specs := make([]diagramSpec, 0, len(paths))
	for _, path := range paths {
		spec, err := readDiagramSpec(path, *out)
		if err != nil {
			return err
		}
		specs = append(specs, spec)
	}
	absoluteRoot, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	archify := filepath.Join(absoluteRoot, "third_party", "archify", "bin", "archify.mjs")
	if info, err := os.Stat(archify); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("diagram-svg: archify CLI not found at %s; pass --root with the repository checkout", archify)
	}

	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return fmt.Errorf("diagram-svg: supports macOS and Linux only, not %s", runtime.GOOS)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		return fmt.Errorf("diagram-svg: node not found on PATH; install Node 18 or newer: %w", err)
	}
	chrome, err := findChrome(runtime.GOOS, os.LookupEnv, isExecutableFile)
	if err != nil {
		return fmt.Errorf("diagram-svg: %w", err)
	}
	tools := diagramTools{
		node: node, chrome: chrome, archify: archify, root: absoluteRoot, timeout: *timeout,
		noSandbox: os.Getenv("ARCHIFY_CHROME_NO_SANDBOX") == "1" || os.Geteuid() == 0,
	}

	// Export everything before writing anything, so a failure in one spec
	// leaves every target as it was.
	exports := make([][]byte, len(specs))
	for index, spec := range specs {
		svg, err := tools.export(ctx, spec)
		if err != nil {
			return fmt.Errorf("diagram-svg: %s: %w", spec.path, err)
		}
		exports[index] = normalizeExport(svg)
	}

	if *check {
		var stale []string
		for index, spec := range specs {
			current, err := os.ReadFile(spec.target)
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("diagram-svg: read %s: %w", spec.target, err)
			}
			if err != nil || !bytes.Equal(current, exports[index]) {
				stale = append(stale, spec.target)
				continue
			}
			fmt.Printf("up to date: %s\n", spec.target)
		}
		if len(stale) > 0 {
			return fmt.Errorf("diagram-svg: %d SVG(s) out of date with a fresh export: %s; regenerate with cwdev diagram-svg", len(stale), strings.Join(stale, ", "))
		}
		return nil
	}
	for index, spec := range specs {
		if err := writeFileAtomic(spec.target, exports[index]); err != nil {
			return fmt.Errorf("diagram-svg: %w", err)
		}
		fmt.Printf("wrote %s\n", spec.target)
	}
	return nil
}

// readDiagramSpec validates the spec at path and reads the fields finalize
// needs. out, when not empty, overrides the default target next to the spec.
func readDiagramSpec(path, out string) (diagramSpec, error) {
	if !strings.HasSuffix(path, ".json") {
		return diagramSpec{}, fmt.Errorf("diagram-svg: spec %s must end in .json", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return diagramSpec{}, fmt.Errorf("diagram-svg: read spec: %w", err)
	}
	var document struct {
		DiagramType any `json:"diagram_type"`
		Meta        *struct {
			QualityProfile any `json:"quality_profile"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return diagramSpec{}, fmt.Errorf("diagram-svg: spec %s is not a JSON object: %w", path, err)
	}
	diagramType, ok := document.DiagramType.(string)
	if !ok || diagramType == "" {
		return diagramSpec{}, fmt.Errorf("diagram-svg: spec %s has no diagram_type string", path)
	}
	spec := diagramSpec{diagramType: diagramType, target: strings.TrimSuffix(path, ".json") + ".svg"}
	if document.Meta != nil && document.Meta.QualityProfile != nil {
		quality, _ := document.Meta.QualityProfile.(string)
		if quality != "standard" && quality != "showcase" {
			return diagramSpec{}, fmt.Errorf("diagram-svg: spec %s has meta.quality_profile %v; want standard or showcase", path, document.Meta.QualityProfile)
		}
		spec.quality = quality
	}
	if spec.path, err = filepath.Abs(path); err != nil {
		return diagramSpec{}, err
	}
	if out != "" {
		spec.target = out
	}
	return spec, nil
}

// export runs finalize and the viewer's SVG export for one spec and returns
// the validated bytes the viewer would download.
func (tools diagramTools) export(parent context.Context, spec diagramSpec) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, tools.timeout)
	defer cancel()
	work, err := os.MkdirTemp("", "cwdev-diagram-svg-")
	if err != nil {
		return nil, err
	}
	defer removeWorkDir(work)

	stem := strings.TrimSuffix(filepath.Base(spec.path), ".json")
	viewerPath := filepath.Join(work, stem+".html")
	err = tools.finalize(ctx, spec, work, viewerPath)
	var chromeFailure browserCheckChromeFailure
	if errors.As(err, &chromeFailure) {
		// archify's browser-check could not run Chrome; its receipt says the
		// failure is about the host, not the diagram, and archify's own
		// supported fix is one retry. Every other gate result still fails.
		retryWork := filepath.Join(work, "retry")
		if mkErr := os.Mkdir(retryWork, 0o700); mkErr != nil {
			return nil, err
		}
		viewerPath = filepath.Join(retryWork, stem+".html")
		retryErr := tools.finalize(ctx, spec, retryWork, viewerPath)
		if retryErr != nil {
			return nil, fmt.Errorf("%w\nretried once after browser-check could not run Chrome; the first attempt failed with: %s", retryErr, chromeFailure.diagnostics)
		}
		fmt.Fprintf(os.Stderr, "cwdev: diagram-svg: %s: browser-check could not run Chrome on the first attempt and passed on one retry; first attempt:%s\n", spec.path, chromeFailure.diagnostics)
	} else if err != nil {
		return nil, err
	}
	viewer, err := os.ReadFile(viewerPath)
	if err != nil || len(viewer) == 0 {
		return nil, fmt.Errorf("archify finalize exited 0 but wrote no viewer HTML at %s", viewerPath)
	}

	injected, err := injectCaptureScript(viewer)
	if err != nil {
		return nil, err
	}
	capturePath := filepath.Join(work, stem+".capture.html")
	if err := os.WriteFile(capturePath, injected, 0o600); err != nil {
		return nil, err
	}
	page := url.URL{Scheme: "file", Path: filepath.ToSlash(capturePath)}
	marker, err := captureWithChrome(ctx, tools.chrome, chromeArgs(filepath.Join(work, "profile"), page.String(), tools.noSandbox))
	if err := timedOut(ctx, tools.timeout, "Chrome; the viewer reported no export", err); err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	svg, err := extractExport(marker)
	if err != nil {
		return nil, err
	}
	if err := validateExport(svg); err != nil {
		return nil, err
	}
	return svg, nil
}

// finalize runs archify finalize for spec, writing viewerPath, with its
// temporary files below work. A failure that archify's receipt attributes
// only to Chrome failing to run browser-check is a
// browserCheckChromeFailure.
func (tools diagramTools) finalize(ctx context.Context, spec diagramSpec, work, viewerPath string) error {
	finalize := []string{tools.archify, "finalize", spec.diagramType, spec.path, viewerPath}
	if spec.quality != "" {
		finalize = append(finalize, "--quality", spec.quality)
	}
	command := exec.CommandContext(ctx, tools.node, finalize...)
	command.Dir = tools.root
	// archify's own temporary files (its browser profile among them) go
	// below work, so they are removed with it even when node is killed.
	command.Env = append(os.Environ(), "ARCHIFY_CHROME="+tools.chrome, "TMPDIR="+work)
	// Killing node's whole process group also stops the browser archify's
	// own checks start.
	ownProcessGroup(command)
	command.Cancel = func() error { return killProcessGroup(command) }
	command.WaitDelay = 5 * time.Second
	output, err := command.CombinedOutput()
	if errors.Is(err, exec.ErrWaitDelay) && command.ProcessState != nil && command.ProcessState.Success() {
		// node succeeded; only a helper that left its process group still
		// held the output, which WaitDelay stopped waiting for.
		err = nil
	}
	// Stop and wait out anything node left in its group (archify's browser
	// after a kill) so it cannot write into work while it is removed.
	_ = killProcessGroup(command)
	waitProcessGroupGone(command, processGroupGrace)
	if err := timedOut(ctx, tools.timeout, "finalize", errors.New(tail(output))); err != nil {
		return err
	}
	if err != nil {
		diagnostics := finalizeDiagnostics(viewerPath)
		failure := fmt.Errorf("archify finalize failed: %w%s\n%s", err, diagnostics, tail(output))
		if browserCheckCouldNotRunChrome(viewerPath) {
			return browserCheckChromeFailure{error: failure, diagnostics: diagnostics}
		}
		return failure
	}
	if viewer, err := os.Stat(viewerPath); err != nil || viewer.Size() == 0 {
		return fmt.Errorf("archify finalize exited 0 but wrote no viewer HTML at %s\n%s", viewerPath, tail(output))
	}
	return nil
}

// browserCheckChromeFailure is a finalize failure whose receipt blames only
// Chrome failing to run browser-check; diagnostics is what the receipt said.
type browserCheckChromeFailure struct {
	error
	diagnostics string
}

func (failure browserCheckChromeFailure) Unwrap() error { return failure.error }

// chromeRuntimeCodes are the archify diagnostics that report Chrome failing
// to complete browser-check (a protocol or event timeout, a crash, a slow
// startup), as opposed to a finding about the diagram.
var chromeRuntimeCodes = map[string]bool{
	"viewer/browser-check-runtime":  true,
	"viewer/chrome-startup-timeout": true,
}

// browserCheckCouldNotRunChrome reports whether the finalize receipt next to
// viewerPath failed at browser-check with only Chrome runtime errors.
func browserCheckCouldNotRunChrome(viewerPath string) bool {
	receipt, ok := readFinalizeReceipt(viewerPath)
	if !ok || receipt.FailedStage != "browser-check" {
		return false
	}
	errorsSeen := 0
	for _, diagnostic := range receipt.Diagnostics {
		if diagnostic.Severity != "error" {
			continue
		}
		if !chromeRuntimeCodes[diagnostic.Code] {
			return false
		}
		errorsSeen++
	}
	return errorsSeen > 0
}

// processGroupGrace bounds the wait for a killed process group to exit.
const processGroupGrace = 5 * time.Second

// removeWorkDir removes the temporary directory, retrying briefly for a
// process that was still exiting, and warns on stderr when it cannot,
// so a leftover directory is never silent.
func removeWorkDir(work string) {
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = os.RemoveAll(work); err == nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Fprintf(os.Stderr, "cwdev: diagram-svg: could not remove temporary directory %s: %v\n", work, err)
}

// timedOut reports the deadline as the failure when ctx expired, keeping
// what the step last reported as the cause.
func timedOut(ctx context.Context, limit time.Duration, step string, cause error) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if cause != nil && strings.TrimSpace(cause.Error()) != "" && cause.Error() != ctx.Err().Error() {
			return fmt.Errorf("timed out after %s during %s (raise --timeout): %v", limit, step, cause)
		}
		return fmt.Errorf("timed out after %s during %s (raise --timeout)", limit, step)
	}
	return ctx.Err()
}

// finalizeDiagnostics returns the diagnostics of the finalize receipt archify
// writes next to its output, which explain a failed gate; its plain output
// only names the receipt, which is removed with the work directory.
func finalizeDiagnostics(viewerPath string) string {
	receipt, ok := readFinalizeReceipt(viewerPath)
	if !ok {
		return ""
	}
	var lines []string
	if receipt.FailedStage != "" {
		lines = append(lines, "failed stage: "+receipt.FailedStage)
	}
	for _, diagnostic := range receipt.Diagnostics {
		line := fmt.Sprintf("%s %s: %s", diagnostic.Severity, diagnostic.Code, diagnostic.Message)
		if diagnostic.Subject.Path != "" {
			line += " (at " + diagnostic.Subject.Path + ")"
		}
		// A runtime failure's cause (a timed-out DevTools method, a crash)
		// is only in its evidence.
		var evidence struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(diagnostic.Evidence, &evidence) == nil && evidence.Reason != "" {
			line += "; reason: " + evidence.Reason
		}
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return ""
	}
	return "\n" + strings.Join(lines, "\n")
}

// finalizeReceipt is the part of archify's finalize receipt cwdev reads.
type finalizeReceipt struct {
	FailedStage string `json:"-"`
	Diagnostics []struct {
		Code     string `json:"code"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Subject  struct {
			Path string `json:"path"`
		} `json:"subject"`
		Evidence json.RawMessage `json:"evidence"`
	} `json:"diagnostics"`
}

// readFinalizeReceipt reads the finalize receipt archify writes next to
// viewerPath. A failedStage that is not a string reads as "".
func readFinalizeReceipt(viewerPath string) (finalizeReceipt, bool) {
	var receipt finalizeReceipt
	data, err := os.ReadFile(strings.TrimSuffix(viewerPath, ".html") + ".finalize.json")
	if err != nil {
		return receipt, false
	}
	var raw struct {
		FailedStage any `json:"failedStage"`
	}
	if json.Unmarshal(data, &raw) != nil || json.Unmarshal(data, &receipt) != nil {
		return receipt, false
	}
	receipt.FailedStage, _ = raw.FailedStage.(string)
	return receipt, true
}

// tail keeps the end of a child's output for an error message.
func tail(output []byte) string {
	const limit = 4000
	text := strings.TrimSpace(string(output))
	if len(text) > limit {
		text = "…" + text[len(text)-limit:]
	}
	return text
}

// chromeArgs opens one headless page at a fixed desktop size with a private
// profile, driven over the DevTools pipe.
func chromeArgs(profile, page string, noSandbox bool) []string {
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--disable-background-networking",
		"--disable-component-update",
		"--disable-default-apps",
		"--disable-sync",
		"--metrics-recording-only",
		"--no-first-run",
		"--no-default-browser-check",
		"--force-device-scale-factor=1",
		"--window-size=1440,900",
		"--user-data-dir=" + profile,
		"--remote-debugging-pipe",
	}
	if noSandbox {
		args = append([]string{"--no-sandbox"}, args...)
	}
	return append(args, page)
}

// writeFileAtomic replaces path with data through a temporary file in the
// same directory, so a reader never sees a partial file.
func writeFileAtomic(path string, data []byte) (err error) {
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(file.Name())
		}
	}()
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Chmod(0o644); err != nil {
		file.Close()
		return err
	}
	if err = file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

// captureScript runs inside the viewer. It hooks the blob URL the viewer
// creates for its download, clicks the viewer's own Export → SVG · Auto item
// and reports the downloaded bytes, or the viewer's error, in one <pre>
// element that the command reads over the DevTools protocol. The anchor click is suppressed so no file
// is saved.
const captureScript = `(function () {
  'use strict';
  var script = document.currentScript;
  if (script) script.remove();
  var reported = false;
  function base64(bytes) {
    var text = '';
    for (var i = 0; i < bytes.length; i += 0x8000) {
      text += String.fromCharCode.apply(null, bytes.subarray(i, i + 0x8000));
    }
    return btoa(text);
  }
  function report(status, bytes) {
    if (reported) return;
    reported = true;
    var element = document.createElement('pre');
    element.id = 'cwdev-svg-' + 'export';
    element.setAttribute('data-status', status);
    element.textContent = base64(bytes);
    document.documentElement.appendChild(element);
  }
  function fail(message) { report('error', new TextEncoder().encode(String(message))); }
  var requested = false;
  var watchdog = null;
  var createObjectURL = URL.createObjectURL;
  URL.createObjectURL = function (blob) {
    if (requested && blob && typeof blob.type === 'string' && blob.type.indexOf('image/svg+xml') === 0) {
      clearTimeout(watchdog);
      var root = document.documentElement;
      if (root.getAttribute('data-last-export-format') !== 'svg') {
        fail('viewer export receipt names format ' + root.getAttribute('data-last-export-format') + ', not svg');
      } else if (root.getAttribute('data-last-export-canonical') !== 'true') {
        fail('viewer reported a non-canonical SVG export (viewer state leaked into it)');
      } else {
        blob.arrayBuffer().then(function (buffer) { report('ok', new Uint8Array(buffer)); },
          function (error) { fail('could not read the exported SVG: ' + error); });
      }
    }
    return createObjectURL.apply(this, arguments);
  };
  var click = HTMLAnchorElement.prototype.click;
  HTMLAnchorElement.prototype.click = function () {
    if (this.hasAttribute('download') && /^blob:/.test(this.href)) return;
    return click.apply(this, arguments);
  };
  window.alert = function (message) { fail('viewer export failed: ' + message); };
  function exportSvg() {
    var item = document.querySelector('#export-menu button[data-format="svg"]');
    if (!item) { fail('the viewer has no Export > SVG Auto item'); return; }
    if (item.disabled) { fail('the viewer disabled its Export > SVG Auto item'); return; }
    requested = true;
    try { item.click(); } catch (error) { fail('Export > SVG Auto threw: ' + error); return; }
    watchdog = setTimeout(function () { fail('Export > SVG Auto produced no download'); }, 10000);
  }
  window.addEventListener('load', function () {
    var fonts = document.fonts ? document.fonts.ready : Promise.resolve();
    fonts.then(function () {
      requestAnimationFrame(function () { requestAnimationFrame(exportSvg); });
    }, function (error) { fail('fonts failed to load: ' + error); });
  });
})();`

var bodyClose = regexp.MustCompile(`(?i)</body>`)

// injectCaptureScript returns viewer with the capture script inserted
// immediately before the last </body>.
func injectCaptureScript(viewer []byte) ([]byte, error) {
	matches := bodyClose.FindAllIndex(viewer, -1)
	if len(matches) == 0 {
		return nil, errors.New("viewer HTML has no </body> to inject the capture script before")
	}
	at := matches[len(matches)-1][0]
	result := make([]byte, 0, len(viewer)+len(captureScript)+len("<script></script>"))
	result = append(result, viewer[:at]...)
	result = append(result, "<script>"+captureScript+"</script>"...)
	return append(result, viewer[at:]...), nil
}

var exportMarker = regexp.MustCompile(`<pre id="cwdev-svg-export" data-status="(ok|error)">([^<]*)</pre>`)

// extractExport returns the SVG bytes the capture script reported in the
// DOM that Chrome dumped.
func extractExport(dom []byte) ([]byte, error) {
	matches := exportMarker.FindAllSubmatch(dom, -1)
	switch len(matches) {
	case 0:
		return nil, errors.New("no export: the viewer's Export > SVG Auto produced nothing before Chrome dumped the page")
	case 1:
	default:
		return nil, fmt.Errorf("the page holds %d capture markers, want one", len(matches))
	}
	payload, err := base64.StdEncoding.DecodeString(string(matches[0][2]))
	if err != nil {
		return nil, fmt.Errorf("capture marker payload is not base64: %w", err)
	}
	if string(matches[0][1]) == "error" {
		return nil, fmt.Errorf("viewer export failed: %s", payload)
	}
	if len(payload) == 0 {
		return nil, errors.New("no export: the viewer's SVG download was empty")
	}
	return payload, nil
}

var scriptTag = regexp.MustCompile(`(?i)<script`)

// validateExport checks the viewer's SVG export contract: a standalone SVG
// document with both colour themes, an opaque background and no script.
func validateExport(svg []byte) error {
	switch {
	case !bytes.HasPrefix(svg, []byte("<?xml")):
		return errors.New("export does not start with an XML declaration")
	case !bytes.Contains(svg, []byte("<svg")):
		return errors.New("export has no <svg> element")
	case !bytes.HasSuffix(bytes.TrimRight(svg, " \t\r\n\f\v"), []byte("</svg>")):
		return errors.New("export does not end with </svg>")
	case scriptTag.Match(svg):
		return errors.New("export contains a <script> element")
	case !bytes.Contains(svg, []byte("prefers-color-scheme")):
		return errors.New("export is not dual-theme: no prefers-color-scheme rule")
	case !bytes.Contains(svg, []byte("c-bg-rect")):
		return errors.New("export has no opaque background rectangle (c-bg-rect)")
	}
	return nil
}

// normalizeExport adds the final newline the repository's .editorconfig
// requires; it is the only change to the viewer's bytes.
func normalizeExport(svg []byte) []byte {
	if bytes.HasSuffix(svg, []byte("\n")) {
		return svg
	}
	return append(svg[:len(svg):len(svg)], '\n')
}

// findChrome mirrors archify's findChrome (third_party/archify/bin/
// visual-check.mjs) for macOS and Linux: ARCHIFY_CHROME wins outright, then
// the standard Chrome and Chromium locations, then Microsoft Edge.
func findChrome(goos string, lookupEnv func(string) (string, bool), isExecutable func(string) bool) (string, error) {
	if configured, ok := lookupEnv("ARCHIFY_CHROME"); ok {
		if configured != "" && isExecutable(configured) {
			return filepath.Abs(configured)
		}
		return "", fmt.Errorf("ARCHIFY_CHROME=%q is not an executable file", configured)
	}
	pathList, _ := lookupEnv("PATH")
	onPath := func(name string) string {
		for _, directory := range filepath.SplitList(pathList) {
			if directory == "" {
				continue
			}
			if candidate := filepath.Join(directory, name); isExecutable(candidate) {
				return candidate
			}
		}
		return ""
	}
	var found string
	if goos == "darwin" {
		for _, candidate := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		} {
			if isExecutable(candidate) {
				found = candidate
				break
			}
		}
	} else {
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
			if found = onPath(name); found != "" {
				break
			}
		}
	}
	if found == "" && goos == "darwin" {
		if candidate := "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"; isExecutable(candidate) {
			found = candidate
		}
	}
	for _, name := range []string{"microsoft-edge", "microsoft-edge-stable"} {
		if found != "" {
			break
		}
		found = onPath(name)
	}
	if found == "" {
		return "", errors.New("no Chrome, Chromium or Microsoft Edge found; install one or set ARCHIFY_CHROME to its executable")
	}
	return filepath.Abs(found)
}

// isExecutableFile accepts a regular file, following symlinks, with an
// execute bit.
func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}
