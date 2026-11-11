//go:build unix

package skillcheck

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	ossignal "os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const installerTemplateMarker = "<!-- installer-template -->"

// extractInstallerTemplate returns the body of the sh fence that directly
// follows the single installer-template marker line.
func extractInstallerTemplate(document string) (string, error) {
	lines := strings.Split(document, "\n")
	marker := -1
	for index, line := range lines {
		if line != installerTemplateMarker {
			continue
		}
		if marker >= 0 {
			return "", errors.New("installer template marker appears more than once")
		}
		marker = index
	}
	if marker < 0 {
		return "", errors.New("installer template marker is missing")
	}
	if marker+1 >= len(lines) || lines[marker+1] != "```sh" {
		return "", errors.New("installer template marker is not followed by a sh fence")
	}
	for end := marker + 2; end < len(lines); end++ {
		if lines[end] == "```" {
			return strings.Join(lines[marker+2:end], "\n") + "\n", nil
		}
	}
	return "", errors.New("installer template fence is not closed")
}

func TestInstallerTemplateExtraction(t *testing.T) {
	t.Parallel()
	valid := "# Title\n\n" + installerTemplateMarker + "\n```sh\nset -eu\necho \"$x\"\n```\nafter\n```\n"
	got, err := extractInstallerTemplate(valid)
	if err != nil || got != "set -eu\necho \"$x\"\n" {
		t.Fatalf("valid template: %q, %v", got, err)
	}
	for name, document := range map[string]string{
		"missing marker":        "```sh\necho hi\n```\n",
		"indented marker":       " " + installerTemplateMarker + "\n```sh\necho hi\n```\n",
		"duplicate marker":      installerTemplateMarker + "\n```sh\necho hi\n```\n" + installerTemplateMarker + "\n",
		"blank before fence":    installerTemplateMarker + "\n\n```sh\necho hi\n```\n",
		"other language fence":  installerTemplateMarker + "\n```bash\necho hi\n```\n",
		"fence with trailing":   installerTemplateMarker + "\n```sh \necho hi\n```\n",
		"missing opening fence": installerTemplateMarker + "\necho hi\n```\n",
		"missing closing fence": installerTemplateMarker + "\n```sh\necho hi\n``` \n",
		"marker at end":         "text\n" + installerTemplateMarker,
	} {
		if template, err := extractInstallerTemplate(document); err == nil {
			t.Errorf("%s: extracted %q", name, template)
		}
	}
}

type installerShell struct {
	name string
	argv func() ([]string, bool)
}

// installerMode is one invocation mode. The child never gets a terminal on
// stdin: it gets the null device, the script file, or a pipe.
type installerMode struct {
	name      string
	scriptArg bool // T1a: <shell> /path/script.sh
	stdinFile bool // T1b: <shell> < /path/script.sh
	piped     bool // T1c: script bytes | <shell> -s
}

type installerRelease int

const (
	releaseGood installerRelease = iota
	releaseMissingArchive
	releaseChecksumMismatch
	releaseNoSumsLine
	releaseMissingSums
)

type installerScenario struct {
	name     string
	release  installerRelease
	existing bool
	quoted   bool
}

func lookShell(name string, fallbacks ...string) (string, bool) {
	if path, err := exec.LookPath(name); err == nil {
		return path, true
	}
	for _, path := range fallbacks {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path, true
		}
	}
	return "", false
}

var installerShells = []installerShell{
	{"T1-bin-sh", func() ([]string, bool) { return []string{"/bin/sh"}, true }},
	{"T1-bash", func() ([]string, bool) {
		path, ok := lookShell("bash")
		return []string{path}, ok
	}},
	{"T1-bash-posix", func() ([]string, bool) {
		path, ok := lookShell("bash")
		return []string{path, "--posix"}, ok
	}},
	// The PATH bash may be a newer bash; /bin/bash (3.2 on macOS) runs on its own.
	{"T1-bin-bash", func() ([]string, bool) {
		_, ok := lookShell("/bin/bash")
		return []string{"/bin/bash"}, ok
	}},
	{"T1-bin-bash-posix", func() ([]string, bool) {
		_, ok := lookShell("/bin/bash")
		return []string{"/bin/bash", "--posix"}, ok
	}},
	{"T1-dash", func() ([]string, bool) {
		path, ok := lookShell("dash", "/bin/dash")
		return []string{path}, ok
	}},
	{"T1-busybox-sh", func() ([]string, bool) {
		path, ok := lookShell("busybox")
		return []string{path, "sh"}, ok
	}},
}

var installerModes = []installerMode{
	{name: "T1a-script-argument", scriptArg: true},
	{name: "T1b-T7-stdin-file", stdinFile: true},
	{name: "T1c-T7-piped-s", piped: true},
}

var installerScenarios = []installerScenario{
	{name: "T2-T3-T5-fresh-install", release: releaseGood},
	{name: "T3-T5-replaces-existing", release: releaseGood, existing: true},
	{name: "T6-T3-T5-quoted-paths", release: releaseGood, quoted: true},
	{name: "T6-T3-T5-quoted-paths-replaces-existing", release: releaseGood, existing: true, quoted: true},
	{name: "T4a-T5-missing-archive", release: releaseMissingArchive},
	{name: "T4a-T5-missing-archive-keeps-existing", release: releaseMissingArchive, existing: true},
	{name: "T4b-T5-checksum-mismatch", release: releaseChecksumMismatch},
	{name: "T4b-T5-checksum-mismatch-keeps-existing", release: releaseChecksumMismatch, existing: true},
	{name: "T4c-T5-no-sums-line", release: releaseNoSumsLine},
	{name: "T4c-T5-no-sums-line-keeps-existing", release: releaseNoSumsLine, existing: true},
	{name: "T4d-T5-missing-sums", release: releaseMissingSums},
	{name: "T4d-T5-missing-sums-keeps-existing", release: releaseMissingSums, existing: true},
}

func TestShellInstallerTemplate(t *testing.T) {
	t.Parallel()
	if (runtime.GOOS != "darwin" && runtime.GOOS != "linux") || (runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64") {
		t.Skipf("installer template targets darwin/linux on amd64/arm64, not %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "skills", "shell-portability", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	template, err := extractInstallerTemplate(string(data))
	if err != nil {
		t.Fatal(err)
	}
	runInstallerTemplate(t, []byte(template))
}

func runInstallerTemplate(t *testing.T, template []byte) {
	archiveName := fmt.Sprintf("tool_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	for _, shell := range installerShells {
		t.Run(shell.name, func(t *testing.T) {
			t.Parallel()
			argv, ok := shell.argv()
			if !ok {
				if shell.name == "T1-bin-sh" {
					t.Fatal("/bin/sh must be available")
				}
				t.Skipf("%s is not installed", shell.name)
			}
			for _, mode := range installerModes {
				t.Run(mode.name, func(t *testing.T) {
					t.Parallel()
					for _, scenario := range installerScenarios {
						t.Run(scenario.name, func(t *testing.T) {
							t.Parallel()
							runInstallerScenario(t, argv, mode, scenario, archiveName, template)
						})
					}
				})
			}
			t.Run("T8-signal-cleanup", func(t *testing.T) {
				t.Parallel()
				for _, mode := range installerModes {
					if mode.stdinFile {
						continue
					}
					for _, signal := range []syscall.Signal{syscall.SIGTERM, syscall.SIGINT} {
						t.Run(mode.name+"/"+signalName(signal), func(t *testing.T) {
							t.Parallel()
							runInstallerSignal(t, argv, mode, signal, archiveName, template)
						})
					}
				}
			})
		})
	}
}

func signalName(signal syscall.Signal) string {
	if signal == syscall.SIGINT {
		return "SIGINT"
	}
	return "SIGTERM"
}

func installerArchive(t *testing.T, payload []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	archive := tar.NewWriter(zipper)
	header := &tar.Header{Name: "tool", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(payload)), ModTime: time.Unix(1700000000, 0)}
	if err := archive.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func writeInstallerRelease(t *testing.T, dir, archiveName string, archive []byte, release installerRelease) {
	t.Helper()
	other := func(name string) string { return sha256Hex([]byte("other "+name)) + "  " + name + "\n" }
	// A neighbouring name that only shares a prefix with the archive must never
	// be taken for the archive's own line.
	decoy := archiveName + ".asc"
	var sums string
	switch release {
	case releaseGood, releaseMissingArchive:
		sums = other("tool_windows_amd64.zip") + other(decoy) + sha256Hex(archive) + "  " + archiveName + "\n" + other("README.md")
	case releaseChecksumMismatch:
		sums = other("tool_windows_amd64.zip") + sha256Hex(append([]byte("tampered"), archive...)) + "  " + archiveName + "\n" + other("README.md")
	case releaseNoSumsLine:
		sums = other("tool_windows_amd64.zip") + sha256Hex(archive) + "  " + decoy + "\n" + other("README.md")
	}
	if release != releaseMissingArchive {
		writeFixture(t, filepath.Join(dir, archiveName), archive)
	}
	if release != releaseMissingSums {
		writeFixture(t, filepath.Join(dir, "SHA256SUMS"), []byte(sums))
	}
}

func runInstallerScenario(t *testing.T, argv []string, mode installerMode, scenario installerScenario, archiveName string, template []byte) {
	root := t.TempDir()
	releaseDir := filepath.Join(root, "release")
	home := filepath.Join(root, "home")
	script := filepath.Join(root, "script", "install.sh")
	writeFixture(t, script, template)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := root
	if scenario.quoted {
		parent = filepath.Join(root, "dir with space [x]*")
		// Siblings that an unquoted glob or a split word would land on.
		for _, decoy := range []string{"dir", "dir with space x", "dir with space [x]"} {
			if err := os.MkdirAll(filepath.Join(root, decoy), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	tmpDir := filepath.Join(parent, "tmp")
	if scenario.quoted {
		tmpDir = filepath.Join(parent, "tmp dir with space [x]*")
	}
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(parent, "install")
	if scenario.quoted {
		installDir = filepath.Join(parent, "install dir with space [x]*")
	}
	payload := []byte("#!/bin/sh\necho installed tool\n# " + t.Name() + "\n\x00\x01\xfe\xff\n")
	writeInstallerRelease(t, releaseDir, archiveName, installerArchive(t, payload), scenario.release)
	previous := []byte("previous tool bytes\n")
	target := filepath.Join(installDir, "tool")
	if scenario.existing {
		if err := os.MkdirAll(installDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, previous, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	args := append([]string(nil), argv[1:]...)
	command := exec.CommandContext(ctx, argv[0])
	switch {
	case mode.scriptArg:
		args = append(args, script)
	case mode.stdinFile:
		file, err := os.Open(script)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		command.Stdin = file
	case mode.piped:
		args = append(args, "-s")
		// A non-file reader makes os/exec connect stdin through a pipe.
		command.Stdin = io.MultiReader(bytes.NewReader(template))
	}
	command.Args = append(command.Args, args...)
	command.Dir = root
	command.WaitDelay = 5 * time.Second
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + tmpDir,
		"TOOL_BASE_URL=file://" + filepath.ToSlash(releaseDir),
		"TOOL_INSTALL_DIR=" + installDir,
	}
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	runErr := command.Run()
	if ctx.Err() != nil {
		t.Fatalf("installer did not finish within 30s: %v\nstderr:\n%s", runErr, stderr.String())
	}

	if scenario.release == releaseGood {
		if runErr != nil {
			t.Fatalf("installer failed: %v\nstderr:\n%s", runErr, stderr.String())
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("installed tool: %v\nstderr:\n%s", err, stderr.String())
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("installed tool bytes differ from the archive entry: %q", got)
		}
		info, err := os.Stat(target)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("installed tool is not an owner-executable regular file: %v, %v", info.Mode(), err)
		}
	} else {
		var exitErr *exec.ExitError
		if !errors.As(runErr, &exitErr) || exitErr.ExitCode() == 0 {
			t.Fatalf("installer exit = %v, want a non-zero exit status", runErr)
		}
		if len(bytes.TrimSpace(stderr.Bytes())) == 0 {
			t.Fatal("installer failed without a diagnostic on stderr")
		}
		got, err := os.ReadFile(target)
		switch {
		case scenario.existing && (err != nil || !bytes.Equal(got, previous)):
			t.Fatalf("failed install changed the existing tool: %q, %v", got, err)
		case !scenario.existing && !errors.Is(err, os.ErrNotExist):
			t.Fatalf("failed install left a tool behind: %q, %v", got, err)
		}
	}
	// T9: nothing but the tool itself is left in the install directory.
	if entries, err := os.ReadDir(installDir); err == nil {
		for _, entry := range entries {
			if entry.Name() != "tool" {
				t.Errorf("TOOL_INSTALL_DIR holds a leftover entry: %q", entry.Name())
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("TOOL_INSTALL_DIR after run: %v", err)
	}
	assertEmptyTmpDir(t, tmpDir)
}

func assertEmptyTmpDir(t *testing.T, tmpDir string) {
	t.Helper()
	entries, err := os.ReadDir(tmpDir)
	if err != nil {
		t.Fatalf("TMPDIR after run: %v", err)
	}
	if len(entries) != 0 {
		var names []string
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("TMPDIR is not empty after the run: %v", names)
	}
}

// runInstallerSignal is T8: the archive download never completes, the shell's
// process group is signalled as a terminal would, and the template must exit
// non-zero, leave TMPDIR empty and install nothing.
func runInstallerSignal(t *testing.T, argv []string, mode installerMode, signal syscall.Signal, archiveName string, template []byte) {
	// A signal ignored on entry to a non-interactive shell cannot be trapped,
	// and the child inherits that from this process (as with `go test &`).
	if ossignal.Ignored(signal) {
		t.Skipf("%s is ignored by the test process and would be ignored by the shell; cannot check its cleanup", signalName(signal))
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	tmpDir := filepath.Join(root, "tmp")
	installDir := filepath.Join(root, "install")
	script := filepath.Join(root, "script", "install.sh")
	writeFixture(t, script, template)
	for _, dir := range []string{home, tmpDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	archive := installerArchive(t, []byte("#!/bin/sh\necho installed tool\n"))
	sums := sha256Hex(archive) + "  " + archiveName + "\n"

	requested := make(chan struct{})
	var requestOnce sync.Once
	unblock := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/SHA256SUMS":
			_, _ = io.WriteString(w, sums)
		case "/" + archiveName:
			requestOnce.Do(func() { close(requested) })
			select {
			case <-unblock:
			case <-r.Context().Done():
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(unblock) }) // runs before server.Close

	args := append([]string(nil), argv[1:]...)
	command := exec.Command(argv[0])
	switch {
	case mode.scriptArg:
		args = append(args, script)
	case mode.piped:
		args = append(args, "-s")
		command.Stdin = io.MultiReader(bytes.NewReader(template))
	}
	command.Args = append(command.Args, args...)
	command.Dir = root
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.WaitDelay = 5 * time.Second
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + tmpDir,
		"TOOL_BASE_URL=" + server.URL,
		"TOOL_INSTALL_DIR=" + installDir,
	}
	var stderr bytes.Buffer
	command.Stdout, command.Stderr = io.Discard, &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pgid := command.Process.Pid
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	kill := func() {
		_ = syscall.Kill(-pgid, syscall.SIGKILL)
		<-done //nolint:errcheck // only joins Wait after the kill
	}

	ready := func() bool {
		select {
		case <-requested:
		default:
			return false
		}
		entries, err := os.ReadDir(tmpDir)
		return err == nil && len(entries) > 0
	}
	deadline := time.Now().Add(10 * time.Second)
	for !ready() {
		select {
		case err := <-done:
			t.Fatalf("installer exited before its archive download was pending with an entry in TMPDIR: %v\nstderr:\n%s", err, stderr.String())
		case <-time.After(10 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			kill()
			select {
			case <-requested:
				t.Fatal("archive was requested but TMPDIR stayed empty: temporary files are not created under TMPDIR")
			default:
				t.Fatal("archive was not requested within 10s")
			}
		}
	}

	if err := syscall.Kill(-pgid, signal); err != nil {
		kill()
		t.Fatal(err)
	}
	select {
	case err := <-done:
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() == 0 {
			t.Errorf("installer after %s: %v; want a non-zero exit or death by signal", signalName(signal), err)
		}
	case <-time.After(10 * time.Second):
		kill()
		t.Fatalf("installer did not exit within 10s of %s\nstderr:\n%s", signalName(signal), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(installDir, "tool")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("interrupted install left a tool behind: %v", err)
	}
	assertEmptyTmpDir(t, tmpDir)
}
