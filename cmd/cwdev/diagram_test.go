package main

// Contract tests for `cwdev diagram-svg` (contract diagram-svg-v1): pure
// helpers, argument and spec errors, and real-tool exports. The pipeline
// tests with fake node and browser executables live in
// diagram_fake_test.go.

import (
	"bytes"
	"encoding/base64"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

const dsvgMarkerText = `<pre id="cwdev-svg-export"`

// dsvgASCIILower lowercases ASCII letters only, keeping byte offsets.
func dsvgASCIILower(b []byte) []byte {
	out := make([]byte, len(b))
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		out[i] = c
	}
	return out
}

// dsvgLastBodyIndex returns the index of the last "</body>" in any case.
func dsvgLastBodyIndex(b []byte) int {
	return bytes.LastIndex(dsvgASCIILower(b), []byte("</body>"))
}

// dsvgMarker is the capture marker as --dump-dom serialises it (R4).
func dsvgMarker(status string, payload []byte) string {
	return dsvgMarkerText + ` data-status="` + status + `">` + base64.StdEncoding.EncodeToString(payload) + `</pre>`
}

// dsvgDOM is a dumped DOM holding the given marker elements.
func dsvgDOM(markers ...string) []byte {
	return []byte("<html><head><title>viewer</title></head><body><main>viewer</main>" + strings.Join(markers, "") + "</body></html>")
}

// dsvgCheckInjected asserts the R4 shape of injectCaptureScript's result:
// original[:i] + "<script>" + S + "</script>" + original[i:] for the last
// "</body>" at i, with a non-empty S that is one element's text and never
// contains the capture marker.
func dsvgCheckInjected(t *testing.T, original, got []byte) {
	t.Helper()
	i := dsvgLastBodyIndex(original)
	if i < 0 {
		t.Fatalf("test input has no </body>: %q", original)
	}
	head := append(append([]byte{}, original[:i]...), "<script>"...)
	tail := append([]byte("</script>"), original[i:]...)
	if len(got) <= len(head)+len(tail) || !bytes.HasPrefix(got, head) || !bytes.HasSuffix(got, tail) {
		t.Fatalf("injected page is not original[:last </body>] + <script>S</script> + rest:\noriginal %q\n     got %q", original, got)
	}
	script := got[len(head) : len(got)-len(tail)]
	if bytes.Contains(script, []byte(dsvgMarkerText)) {
		t.Errorf("capture script text contains the literal marker %s", dsvgMarkerText)
	}
	if bytes.Contains(dsvgASCIILower(script), []byte("</script")) {
		t.Errorf("capture script text closes its element early, so more than one element was inserted")
	}
}

// viewer-contract (R4): injectCaptureScript inserts one script immediately
// before the last </body> (any case) and changes no other byte, without
// writing into the caller's slice.
func TestDiagramSVGContractInjectCaptureScript(t *testing.T) {
	for _, viewer := range []string{
		"<!doctype html><html><body><p>hi</p></body></html>",
		"<HTML><BODY>upper</BODY></HTML>\n",
		"<html><body>mixed</Body>\n</html>",
		"<body><script>var s = \"</body>\";</script><p>\u00e9 \u00fc \u65e5\u672c</p></body></html>",
		"<body>first</body><body>second</body>",
		"</body>",
		string(dsvgFakeViewer("inject", "flowchart")),
	} {
		input := []byte(viewer)
		backing := make([]byte, len(input), len(input)+4096)
		copy(backing, input)
		got, err := injectCaptureScript(backing)
		if err != nil {
			t.Fatalf("injectCaptureScript(%q): %v", viewer, err)
		}
		dsvgCheckInjected(t, input, got)
		if !bytes.Equal(backing, input) {
			t.Errorf("injectCaptureScript modified its input %q into %q", input, backing)
		}
	}
}

// viewer-contract (R4): a page without </body> is rejected.
func TestDiagramSVGContractInjectCaptureScriptNeedsBody(t *testing.T) {
	for _, viewer := range []string{"", "<html></html>", "<html><body><p>open</p>", "</bod y>", "<body/>"} {
		if got, err := injectCaptureScript([]byte(viewer)); err == nil {
			t.Errorf("injectCaptureScript(%q) = %q, want an error", viewer, got)
		}
	}
}

// pipeline (R4): extractExport returns the bytes of exactly one ok marker.
func TestDiagramSVGContractExtractExportReturnsPayload(t *testing.T) {
	for _, payload := range [][]byte{
		dsvgFakeSVG("extract"),
		[]byte("<svg>\u00e9\u65e5</svg>\n"),
		{0x00, 0xff, '<', '>', '&'},
		[]byte("x"),
	} {
		// The marker's outerHTML alone (what Runtime.evaluate returns) and a
		// page containing it.
		for _, dom := range [][]byte{[]byte(dsvgMarker("ok", payload)), dsvgDOM(dsvgMarker("ok", payload))} {
			got, err := extractExport(dom)
			if err != nil {
				t.Fatalf("extractExport(%q): %v", dom, err)
			}
			if !bytes.Equal(got, payload) {
				t.Fatalf("extractExport(%q) = %q, want %q", dom, got, payload)
			}
		}
	}
}

// failures (R4): the marker's error status, a missing marker, several
// markers, bad base64 and an empty ok payload are errors.
func TestDiagramSVGContractExtractExportErrors(t *testing.T) {
	message := "export failed: viewer \u2014 no svg element"
	_, err := extractExport(dsvgDOM(dsvgMarker("error", []byte(message))))
	if err == nil || !strings.Contains(err.Error(), message) {
		t.Errorf("error marker: got %v, want an error containing %q", err, message)
	}
	_, err = extractExport(dsvgDOM())
	if err == nil || !strings.Contains(err.Error(), "no export") {
		t.Errorf("no marker: got %v, want an error containing %q", err, "no export")
	}
	ok := dsvgMarker("ok", dsvgFakeSVG("one"))
	for name, dom := range map[string][]byte{
		"two ok markers":        dsvgDOM(ok, dsvgMarker("ok", dsvgFakeSVG("two"))),
		"ok and error markers":  dsvgDOM(ok, dsvgMarker("error", []byte("boom"))),
		"invalid base64":        dsvgDOM(dsvgMarkerText + ` data-status="ok">@@not base64@@</pre>`),
		"unpadded base64":       dsvgDOM(dsvgMarkerText + ` data-status="ok">YWI</pre>`),
		"empty ok payload":      dsvgDOM(dsvgMarkerText + ` data-status="ok"></pre>`),
		"invalid error payload": dsvgDOM(dsvgMarkerText + ` data-status="error">%%%</pre>`),
	} {
		if got, err := extractExport(dom); err == nil {
			t.Errorf("%s: extractExport = %q, want an error", name, got)
		}
	}
}

// pipeline + viewer-contract (R4): a dumped DOM still holds the injected
// script's own text; extraction sees exactly the one marker the script
// appended.
func TestDiagramSVGContractInjectedPageExtractsOneMarker(t *testing.T) {
	page, err := injectCaptureScript(dsvgFakeViewer("composite", "flowchart"))
	if err != nil {
		t.Fatal(err)
	}
	want := dsvgFakeSVG("composite")
	got, err := extractExport(dsvgInsertBeforeLastBody(page, dsvgMarker("ok", want)))
	if err != nil {
		t.Fatalf("extractExport of an injected page: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("extractExport = %q, want %q", got, want)
	}
	if _, err := extractExport(page); err == nil || !strings.Contains(err.Error(), "no export") {
		t.Fatalf("injected page without a marker: got %v, want an error containing %q", err, "no export")
	}
}

// viewer-contract (R4): validateExport accepts an XML-declared dual-theme
// SVG with its background rectangle, optionally followed by ASCII
// whitespace.
func TestDiagramSVGContractValidateExportAccepts(t *testing.T) {
	valid := string(dsvgFakeSVG("valid"))
	for _, svg := range []string{valid, valid + "\n", valid + "\r\n", valid + " \t\n\n"} {
		if err := validateExport([]byte(svg)); err != nil {
			t.Errorf("validateExport(%q): %v", svg, err)
		}
	}
}

// viewer-contract (R4): each broken rule makes validateExport fail.
func TestDiagramSVGContractValidateExportRejects(t *testing.T) {
	valid := string(dsvgFakeSVG("valid"))
	declaration := `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
	for name, svg := range map[string]string{
		"empty":                     "",
		"no xml declaration":        strings.TrimPrefix(valid, declaration),
		"leading space":             " " + valid,
		"no svg element":            `<?xml version="1.0"?><g class="c-bg-rect">prefers-color-scheme</g></svg>`,
		"no closing svg":            strings.TrimSuffix(valid, "</svg>"),
		"comment after svg":         valid + "<!-- trailer -->",
		"text after svg":            valid + "x",
		"non-ascii space after svg": valid + "\u00a0",
		"script element":            strings.Replace(valid, "</svg>", "<script>alert(1)</script></svg>", 1),
		"upper-case script":         strings.Replace(valid, "</svg>", "<SCRIPT>alert(1)</SCRIPT></svg>", 1),
		"mixed-case script":         strings.Replace(valid, "</svg>", "<Script src=\"x.js\"/></svg>", 1),
		"single theme":              strings.ReplaceAll(valid, "prefers-color-scheme", "prefers-contrast"),
		"no background rectangle":   strings.ReplaceAll(valid, "c-bg-rect", "bg-rect"),
	} {
		if err := validateExport([]byte(svg)); err == nil {
			t.Errorf("%s: validateExport(%q) accepted it", name, svg)
		}
	}
}

// determinism (R5): normalizeExport appends one "\n" exactly when the input
// does not end in "\n", and is idempotent.
func TestDiagramSVGContractNormalizeExport(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"", "\n"},
		{"abc", "abc\n"},
		{"abc\n", "abc\n"},
		{"abc\n\n", "abc\n\n"},
		{"abc\r\n", "abc\r\n"},
		{"abc\r", "abc\r\n"},
		{"</svg> ", "</svg> \n"},
		{string(dsvgFakeSVG("n")), string(dsvgFakeSVG("n")) + "\n"},
	} {
		got := normalizeExport([]byte(c.in))
		if string(got) != c.want {
			t.Errorf("normalizeExport(%q) = %q, want %q", c.in, got, c.want)
		}
		if again := normalizeExport(got); !bytes.Equal(again, got) {
			t.Errorf("normalizeExport is not idempotent on %q: %q", got, again)
		}
	}
}

const (
	dsvgMacChrome   = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	dsvgMacChromium = "/Applications/Chromium.app/Contents/MacOS/Chromium"
	dsvgMacEdge     = "/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge"
)

func dsvgPathList(dirs ...string) string {
	return strings.Join(dirs, string(os.PathListSeparator))
}

// dsvgAllCandidates lists every candidate findChrome may try for PATH
// /opt/a:/opt/b.
func dsvgAllCandidates() []string {
	candidates := []string{dsvgMacChrome, dsvgMacChromium, dsvgMacEdge}
	for _, dir := range []string{"/opt/a", "/opt/b"} {
		for _, name := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "microsoft-edge", "microsoft-edge-stable"} {
			candidates = append(candidates, filepath.Join(dir, name))
		}
	}
	return candidates
}

// dsvgFindChrome calls findChrome with an injected environment and set of
// executables, and returns the paths it probed.
func dsvgFindChrome(goos string, env map[string]string, executables []string) (string, []string, error) {
	var probed []string
	lookupEnv := func(key string) (string, bool) {
		value, ok := env[key]
		return value, ok
	}
	isExecutable := func(path string) bool {
		probed = append(probed, path)
		return slices.Contains(executables, path)
	}
	got, err := findChrome(goos, lookupEnv, isExecutable)
	return got, probed, err
}

// dsvgPoisonRealEnvironment makes the real environment useless to
// findChrome, so it must use the injected lookupEnv.
func dsvgPoisonRealEnvironment(t *testing.T) {
	t.Setenv("PATH", "/nonexistent-dsvg-real-path")
	t.Setenv("ARCHIFY_CHROME", "/nonexistent-dsvg-real-env/chrome")
}

// failures (R2): findChrome's candidate order for macOS, Linux and other
// systems.
func TestDiagramSVGContractFindChromeOrder(t *testing.T) {
	dsvgPoisonRealEnvironment(t)
	ab := map[string]string{"PATH": dsvgPathList("/opt/a", "/opt/b")}
	for _, c := range []struct {
		name        string
		goos        string
		env         map[string]string
		executables []string
		want        string // "" means an error
	}{
		{"darwin-env-wins", "darwin", map[string]string{"ARCHIFY_CHROME": "/opt/custom/chrome", "PATH": ab["PATH"]}, []string{"/opt/custom/chrome", dsvgMacChrome}, "/opt/custom/chrome"},
		{"linux-env-wins", "linux", map[string]string{"ARCHIFY_CHROME": "/opt/custom/chrome", "PATH": ab["PATH"]}, []string{"/opt/custom/chrome", "/opt/a/google-chrome"}, "/opt/custom/chrome"},
		{"darwin-chrome-first", "darwin", ab, []string{dsvgMacChrome, dsvgMacChromium, dsvgMacEdge, "/opt/a/google-chrome"}, dsvgMacChrome},
		{"darwin-chromium-second", "darwin", ab, []string{dsvgMacChromium, dsvgMacEdge, "/opt/a/microsoft-edge"}, dsvgMacChromium},
		{"darwin-ignores-path-chrome", "darwin", ab, []string{"/opt/a/google-chrome", "/opt/a/chromium", dsvgMacEdge, "/opt/a/microsoft-edge"}, dsvgMacEdge},
		{"darwin-path-edge", "darwin", ab, []string{"/opt/a/google-chrome", "/opt/a/microsoft-edge-stable", "/opt/b/microsoft-edge"}, "/opt/b/microsoft-edge"},
		{"darwin-path-edge-stable", "darwin", ab, []string{"/opt/b/microsoft-edge-stable"}, "/opt/b/microsoft-edge-stable"},
		{"darwin-nothing", "darwin", ab, []string{"/opt/a/google-chrome", "/opt/b/chromium-browser"}, ""},
		{"darwin-no-path", "darwin", map[string]string{}, []string{dsvgMacChrome}, dsvgMacChrome},
		{"darwin-no-path-edge", "darwin", map[string]string{}, []string{dsvgMacEdge}, dsvgMacEdge},
		{"linux-names-before-dirs", "linux", ab, []string{"/opt/a/chromium", "/opt/b/google-chrome"}, "/opt/b/google-chrome"},
		{"linux-stable-second", "linux", ab, []string{"/opt/a/chromium-browser", "/opt/a/chromium", "/opt/b/google-chrome-stable"}, "/opt/b/google-chrome-stable"},
		{"linux-chromium-third", "linux", ab, []string{"/opt/a/chromium-browser", "/opt/b/chromium"}, "/opt/b/chromium"},
		{"linux-chromium-browser-fourth", "linux", ab, []string{"/opt/b/chromium-browser", "/opt/a/microsoft-edge"}, "/opt/b/chromium-browser"},
		{"linux-ignores-mac-apps", "linux", ab, []string{dsvgMacChrome, dsvgMacChromium, dsvgMacEdge, "/opt/b/microsoft-edge-stable"}, "/opt/b/microsoft-edge-stable"},
		{"linux-edge-before-edge-stable", "linux", ab, []string{"/opt/a/microsoft-edge-stable", "/opt/b/microsoft-edge"}, "/opt/b/microsoft-edge"},
		{"linux-skips-empty-path-entries", "linux", map[string]string{"PATH": dsvgPathList("", "/opt/b", "")}, []string{"google-chrome", "/opt/b/chromium"}, "/opt/b/chromium"},
		{"linux-nothing", "linux", ab, []string{dsvgMacChrome, dsvgMacEdge}, ""},
		{"linux-no-path", "linux", map[string]string{}, []string{dsvgMacChrome}, ""},
		{"freebsd-like-linux", "freebsd", ab, []string{dsvgMacChrome, "/opt/b/google-chrome"}, "/opt/b/google-chrome"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, probed, err := dsvgFindChrome(c.goos, c.env, c.executables)
			if c.want == "" {
				if err == nil || !strings.Contains(err.Error(), "ARCHIFY_CHROME") {
					t.Fatalf("findChrome = %q, %v; want an error mentioning ARCHIFY_CHROME (probed %q)", got, err, probed)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("findChrome = %q, %v; want %q (probed %q)", got, err, c.want, probed)
			}
		})
	}
}

// failures (R2): a set ARCHIFY_CHROME (even empty) that is not executable is
// an error and no other candidate is tried, although every candidate would
// be accepted.
func TestDiagramSVGContractFindChromeSetButUnusable(t *testing.T) {
	dsvgPoisonRealEnvironment(t)
	candidates := dsvgAllCandidates()
	for _, goos := range []string{"darwin", "linux"} {
		for _, value := range []string{"/opt/broken/chrome", "", "relative/missing-chrome"} {
			env := map[string]string{"ARCHIFY_CHROME": value, "PATH": dsvgPathList("/opt/a", "/opt/b")}
			got, probed, err := dsvgFindChrome(goos, env, candidates)
			if err == nil || !strings.Contains(err.Error(), "ARCHIFY_CHROME") {
				t.Errorf("%s, ARCHIFY_CHROME=%q: findChrome = %q, %v; want an error mentioning ARCHIFY_CHROME", goos, value, got, err)
			}
			for _, path := range probed {
				if slices.Contains(candidates, path) {
					t.Errorf("%s, ARCHIFY_CHROME=%q: fell back to candidate %q", goos, value, path)
				}
			}
		}
	}
}

// failures (R2): a relative ARCHIFY_CHROME is returned made absolute.
func TestDiagramSVGContractFindChromeMakesEnvAbsolute(t *testing.T) {
	dsvgPoisonRealEnvironment(t)
	relative := filepath.Join("rel", "chrome")
	absolute, err := filepath.Abs(relative)
	if err != nil {
		t.Fatal(err)
	}
	for _, goos := range []string{"darwin", "linux"} {
		got, probed, err := dsvgFindChrome(goos, map[string]string{"ARCHIFY_CHROME": relative}, []string{relative, absolute})
		if err != nil || got != absolute {
			t.Errorf("%s: findChrome = %q, %v; want %q (probed %q)", goos, got, err, absolute, probed)
		}
	}
}

// interface (R1): argument and spec errors fail before node or the browser
// is looked up or started, and write nothing. ARCHIFY_CHROME points at a
// missing file, so reaching tool resolution would produce an error naming
// ARCHIFY_CHROME (see TestDiagramSVGContractValidSpecReachesToolResolution),
// and the fake node records any start. Subtest names avoid "archify" and
// "spec" because temporary paths include them.
func TestDiagramSVGContractArgumentErrors(t *testing.T) {
	const good = `{"diagram_type": "flowchart-fake", "meta": {"quality_profile": "standard"}}`
	withSpec := func(name, content string) func(t *testing.T, e *dsvgEnv) []string {
		return func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, dsvgWriteSpec(t, e.diagrams, name, content)}
		}
	}
	goodPath := func(e *dsvgEnv) string { return filepath.Join(e.diagrams, "good.json") }
	for _, c := range []struct {
		name    string
		prepare func(t *testing.T, e *dsvgEnv) []string
		want    string // lower-case substring of the error, optional
	}{
		{"no-input", func(t *testing.T, e *dsvgEnv) []string { return []string{"--root", e.root} }, ""},
		{"all-with-path", func(t *testing.T, e *dsvgEnv) []string { return []string{"--all", "--root", e.root, goodPath(e)} }, ""},
		{"two-paths", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, goodPath(e), dsvgWriteSpec(t, e.diagrams, "second.json", good)}
		}, ""},
		{"all-with-out", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--all", "--root", e.root, "--out", filepath.Join(e.root, "x.svg")}
		}, ""},
		{"unknown-flag", func(t *testing.T, e *dsvgEnv) []string { return []string{"--root", e.root, "--bogus", goodPath(e)} }, ""},
		{"zero-timeout", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, "--timeout", "0s", goodPath(e)}
		}, ""},
		{"bare-zero-timeout", func(t *testing.T, e *dsvgEnv) []string { return []string{"--root", e.root, "--timeout=0", goodPath(e)} }, ""},
		{"negative-timeout", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, "--timeout", "-1s", goodPath(e)}
		}, ""},
		{"unparsable-timeout", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, "--timeout", "soon", goodPath(e)}
		}, ""},
		{"txt-suffix", withSpec("good.txt", good), ""},
		{"absent-file", func(t *testing.T, e *dsvgEnv) []string {
			return []string{"--root", e.root, filepath.Join(e.diagrams, "absent.json")}
		}, ""},
		{"unreadable-file", func(t *testing.T, e *dsvgEnv) []string {
			path := filepath.Join(e.diagrams, "folder.json")
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
			return []string{"--root", e.root, path}
		}, ""},
		{"invalid-json", withSpec("bad.json", `{"diagram_type": `), ""},
		{"no-diagram-type", withSpec("bad.json", `{"title": "x"}`), ""},
		{"empty-diagram-type", withSpec("bad.json", `{"diagram_type": ""}`), ""},
		{"numeric-diagram-type", withSpec("bad.json", `{"diagram_type": 7}`), ""},
		{"null-diagram-type", withSpec("bad.json", `{"diagram_type": null}`), ""},
		{"json-array", withSpec("bad.json", `[]`), ""},
		{"json-null", withSpec("bad.json", `null`), ""},
		{"unknown-quality", withSpec("bad.json", `{"diagram_type": "x", "meta": {"quality_profile": "fancy"}}`), ""},
		{"capitalised-quality", withSpec("bad.json", `{"diagram_type": "x", "meta": {"quality_profile": "Standard"}}`), ""},
		{"empty-quality", withSpec("bad.json", `{"diagram_type": "x", "meta": {"quality_profile": ""}}`), ""},
		{"numeric-quality", withSpec("bad.json", `{"diagram_type": "x", "meta": {"quality_profile": 5}}`), ""},
		{"all-without-json", func(t *testing.T, e *dsvgEnv) []string {
			if err := os.Remove(goodPath(e)); err != nil {
				t.Fatal(err)
			}
			dsvgWriteFile(t, filepath.Join(e.diagrams, "README.md"), "no diagrams yet\n", 0o644)
			return []string{"--all", "--root", e.root}
		}, "spec"},
		{"all-without-directory", func(t *testing.T, e *dsvgEnv) []string {
			if err := os.RemoveAll(e.diagrams); err != nil {
				t.Fatal(err)
			}
			return []string{"--all", "--root", e.root}
		}, ""},
		{"missing-cli-script", func(t *testing.T, e *dsvgEnv) []string {
			if err := os.Remove(e.script); err != nil {
				t.Fatal(err)
			}
			return []string{"--root", e.root, goodPath(e)}
		}, "archify"},
		{"missing-cli-script-all", func(t *testing.T, e *dsvgEnv) []string {
			if err := os.RemoveAll(filepath.Join(e.root, "third_party")); err != nil {
				t.Fatal(err)
			}
			return []string{"--all", "--root", e.root}
		}, "archify"},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			dsvgWriteSpec(t, e.diagrams, "good.json", good)
			t.Setenv("ARCHIFY_CHROME", filepath.Join(e.records, "no-such-browser"))
			args := c.prepare(t, e)
			before := dsvgSnapshot(t, e.root)
			output, err := dsvgRun(t, args...)
			if err == nil {
				t.Fatalf("diagram-svg %q succeeded; output: %s", args, output)
			}
			if strings.Contains(err.Error(), "ARCHIFY_CHROME") {
				t.Errorf("diagram-svg %q resolved the browser before rejecting its input: %v", args, err)
			}
			if c.want != "" && !strings.Contains(strings.ToLower(err.Error()), c.want) {
				t.Errorf("error %q does not mention %q", err, c.want)
			}
			if n := len(dsvgReadRecords(t, e.records, "node")); n != 0 {
				t.Errorf("node started %d times for rejected input %q", n, args)
			}
			dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		})
	}
}

// Control for TestDiagramSVGContractArgumentErrors: in the same environment,
// valid input passes validation and fails at browser resolution, before
// node starts and without writing (R1, R2).
func TestDiagramSVGContractValidSpecReachesToolResolution(t *testing.T) {
	for _, c := range []struct {
		name, spec string
		all, check bool
	}{
		{name: "plain", spec: `{"diagram_type": "flowchart-fake"}`},
		{name: "standard", spec: `{"diagram_type": "flowchart-fake", "meta": {"quality_profile": "standard"}}`},
		{name: "showcase", spec: `{"diagram_type": "flowchart-fake", "meta": {"quality_profile": "showcase"}}`},
		{name: "every", spec: `{"diagram_type": "flowchart-fake"}`, all: true},
		{name: "compare", spec: `{"diagram_type": "flowchart-fake"}`, check: true},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := dsvgSetup(t)
			path := dsvgWriteSpec(t, e.diagrams, "good.json", c.spec)
			t.Setenv("ARCHIFY_CHROME", filepath.Join(e.records, "no-such-browser"))
			args := []string{"--root", e.root, "--timeout", "30s"}
			if c.check {
				args = append(args, "--check")
			}
			if c.all {
				args = append(args, "--all")
			} else {
				args = append(args, path)
			}
			before := dsvgSnapshot(t, e.root)
			output, err := dsvgRun(t, args...)
			if err == nil || !strings.Contains(err.Error(), "ARCHIFY_CHROME") {
				t.Fatalf("diagram-svg %q: got %v, want an error mentioning ARCHIFY_CHROME; output: %s", args, err, output)
			}
			if n := len(dsvgReadRecords(t, e.records, "node")); n != 0 {
				t.Errorf("node started %d times although the browser was not resolved", n)
			}
			dsvgAssertSnapshot(t, "root", before, dsvgSnapshot(t, e.root))
		})
	}
}

// dsvgIsExecutable applies R2's real rule: a regular file (symbolic links
// followed) with an execute bit.
func dsvgIsExecutable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// Real-tool behaviour, interface + determinism + viewer-contract (R1, R4,
// R5): a real export of decision-flow.json passes validateExport, ends in a
// newline and carries no script; a second export is byte-identical; --check
// right after it passes and writes nothing; --check after the file changes
// fails with `out of date`. Four real exports in total.
func TestDiagramSVGContractRealExport(t *testing.T) {
	if testing.Short() {
		t.Skip("real archify exports take several seconds each")
	}
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not on PATH")
	}
	if _, err := findChrome(runtime.GOOS, os.LookupEnv, dsvgIsExecutable); err != nil {
		t.Skipf("no Chrome found: %v", err)
	}
	root := filepath.Join("..", "..")
	spec := filepath.Join(root, "docs", "diagrams", "decision-flow.json")
	if _, err := os.Stat(spec); err != nil {
		t.Fatalf("contract fixture: %v", err)
	}
	out := filepath.Join(t.TempDir(), "decision-flow.svg")
	args := []string{"--root", root, "--out", out, spec}

	output, err := dsvgRun(t, args...)
	if err != nil {
		t.Fatalf("real export failed: %v\noutput: %s", err, output)
	}
	first, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateExport(first); err != nil {
		t.Errorf("real export breaks the viewer contract: %v", err)
	}
	if !bytes.HasSuffix(first, []byte("\n")) {
		t.Errorf("real export does not end in a newline")
	}
	if bytes.Contains(dsvgASCIILower(first), []byte("<script")) {
		t.Errorf("real export carries a script")
	}

	second := filepath.Join(filepath.Dir(out), "second.svg")
	output, err = dsvgRun(t, "--root", root, "--out", second, spec)
	if err != nil {
		t.Fatalf("second real export failed: %v\noutput: %s", err, output)
	}
	if data, err := os.ReadFile(second); err != nil || !bytes.Equal(data, first) {
		t.Fatalf("two exports of the same spec differ (err %v)", err)
	}

	output, err = dsvgRun(t, append([]string{"--check"}, args...)...)
	if err != nil {
		t.Fatalf("--check right after the export failed: %v\noutput: %s", err, output)
	}
	dsvgAssertReported(t, output, "up to date: ", out)
	if again, err := os.ReadFile(out); err != nil || !bytes.Equal(again, first) {
		t.Fatalf("--check changed %s (err %v)", out, err)
	}

	modified := append(append([]byte{}, first...), '\n')
	if err := os.WriteFile(out, modified, 0o644); err != nil {
		t.Fatal(err)
	}
	output, err = dsvgRun(t, append([]string{"--check"}, args...)...)
	if err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("--check after modifying %s: got %v, want an error containing %q; output: %s", out, err, "out of date", output)
	}
	if again, err := os.ReadFile(out); err != nil || !bytes.Equal(again, modified) {
		t.Fatalf("failing --check rewrote %s (err %v)", out, err)
	}
}
