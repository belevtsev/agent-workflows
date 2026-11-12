//go:build unix

package langpack_test

import (
	"crypto/sha256"
	"encoding/hex"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// gtEnv is the process environment with PATH replaced by path and the go
// command pinned to its own toolchain, followed by overrides.
func gtEnv(path string, overrides ...string) []string {
	env := slices.DeleteFunc(os.Environ(), func(entry string) bool { return strings.HasPrefix(entry, "PATH=") })
	return append(append(env, "PATH="+path, "GOTOOLCHAIN=local", "GOWORK=off", "GOFLAGS="), overrides...)
}

// gtRealGo returns the directory of the go command that runs this test: go
// test puts it first on the test's PATH.
func gtRealGo(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("go is not on PATH: %v", err)
	}
	return filepath.Dir(path)
}

// gtScript writes an executable shell script at dir/name.
func gtScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// gtRefused fails unless err refuses the candidate: it names the candidate,
// contains want and tells the user to put the real go first on PATH.
func gtRefused(t *testing.T, tool langpack.GoTool, err error, candidate, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("ResolveGo accepted %+v, want a refusal of %s", tool, candidate)
	}
	for _, fragment := range []string{candidate, want, "put the real go first on PATH"} {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("ResolveGo error %q does not contain %q", err, fragment)
		}
	}
}

// The go command that runs this test verifies. Oracle: G6's acceptance: Path
// ends in /bin/go, GOROOT is its own tree (review finding 1 on #119), the
// version is in the runtime.Version() family, SHA256 is the hash of Path's
// bytes, and Env pins GOROOT and GOTOOLCHAIN=local.
func TestResolveGoAcceptsTheRealGoCommand(t *testing.T) {
	tool, err := langpack.ResolveGo(t.Context(), "go", t.TempDir(), gtEnv(gtRealGo(t)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tool.Path, "/bin/go") || version.Lang(tool.Version) != version.Lang(runtime.Version()) {
		t.Errorf("ResolveGo = %+v, want a Path ending in /bin/go and a %s version", tool, version.Lang(runtime.Version()))
	}
	if tool.GOROOT != filepath.Dir(filepath.Dir(tool.Path)) {
		t.Errorf("GOROOT %s, want the tree of %s", tool.GOROOT, tool.Path)
	}
	data, err := os.ReadFile(tool.Path)
	if err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256(data); tool.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("SHA256 %s, want %x", tool.SHA256, sum)
	}
	if want := []string{"GOROOT=" + tool.GOROOT, "GOTOOLCHAIN=local"}; !slices.Equal(tool.Env(), want) {
		t.Errorf("Env = %q, want %q", tool.Env(), want)
	}
}

// A shim, such as a version manager's, is followed to the go command of the
// GOROOT it reports. Oracle: the plan's shim rule: the result is the real go
// command, and a program given as a path relative to dir is that candidate.
func TestResolveGoFollowsAShimToTheGoCommandItReports(t *testing.T) {
	dir := t.TempDir()
	want, err := langpack.ResolveGo(t.Context(), "go", dir, gtEnv(gtRealGo(t)))
	if err != nil {
		t.Fatal(err)
	}
	shims := filepath.Join(dir, "shims")
	gtScript(t, shims, "go", "exec '"+filepath.Join(gtRealGo(t), "go")+"' \"$@\"\n")
	for _, program := range []string{"go", "shims/go", filepath.Join(shims, "go")} {
		got, err := langpack.ResolveGo(t.Context(), program, dir, gtEnv(shims))
		if err != nil || got != want {
			t.Errorf("ResolveGo(%q) = %+v, %v; want %+v", program, got, err, want)
		}
	}
}

// Go commands that are not a build of cmd/go are refused, each with an error
// that names the candidate. Oracle: G6's design: only a Go program whose build
// information names cmd/go runs, whatever the candidate prints.
func TestResolveGoRefusesAGoCommandThatIsNotCmdGo(t *testing.T) {
	base := t.TempDir()
	// The review's fake: a script that answers every command with ok.
	fake := gtScript(t, filepath.Join(base, "fake"), "go", "echo ok\nexit 0\n")
	// A script that reports a GOROOT whose bin/go is the script itself.
	selfRoot := filepath.Join(base, "selfroot")
	self := gtScript(t, filepath.Join(selfRoot, "bin"), "go", "echo '"+selfRoot+"'\n")
	// A Go program that is not cmd/go, built here, reporting its own GOROOT.
	builtRoot := filepath.Join(base, "builtroot")
	source := filepath.Join(base, "notgo")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("module example.com/notgo\n\ngo 1.21\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "main.go"), []byte("package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n)\n\nfunc main() {\n\texe, _ := os.Executable()\n\tfmt.Println(filepath.Dir(filepath.Dir(exe)))\n}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	built := filepath.Join(builtRoot, "bin", "go")
	build := exec.CommandContext(t.Context(), filepath.Join(gtRealGo(t), "go"), "build", "-o", built, ".")
	build.Dir, build.Env = source, gtEnv(gtRealGo(t))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build the non-go program: %v\n%s", err, output)
	}
	// A script that reports a GOROOT whose bin/go is a FIFO, which an open
	// for reading would wait on for ever.
	fifoRoot := filepath.Join(base, "fiforoot")
	if err := os.MkdirAll(filepath.Join(fifoRoot, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(fifoRoot, "bin", "go"), 0o600); err != nil {
		t.Fatal(err)
	}
	fifo := gtScript(t, filepath.Join(base, "fifo"), "go", "echo '"+fifoRoot+"'\n")
	for _, tc := range []struct {
		name, path, candidate, want string
	}{
		{"script that prints ok", filepath.Dir(fake), fake, "`go env GOROOT` printed \"ok\\n\", not an absolute directory"},
		{"script that reports its own GOROOT", filepath.Dir(self), self, self + " is not a Go program"},
		{"compiled program that is not cmd/go", filepath.Dir(built), built, built + " is a build of example.com/notgo, not of cmd/go"},
		{"script that fails", filepath.Dir(gtScript(t, filepath.Join(base, "failing"), "go", "echo 'go: broken' >&2\nexit 3\n")), filepath.Join(base, "failing", "go"), "`go env GOROOT` failed: \"go: broken\""},
		{"GOROOT whose go is a FIFO", filepath.Dir(fifo), fifo, filepath.Join(fifoRoot, "bin", "go") + " is not a regular file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tool, err := langpack.ResolveGo(t.Context(), "go", base, gtEnv(tc.path))
			gtRefused(t, tool, err, tc.candidate, tc.want)
		})
	}
	if tool, err := langpack.ResolveGo(t.Context(), "go", base, gtEnv(filepath.Join(base, "empty"))); err == nil || !strings.Contains(err.Error(), "go was not found on PATH") {
		t.Errorf("ResolveGo with no go on PATH = %+v, %v; want \"go was not found on PATH\"", tool, err)
	}
}

// The real go command, asked for its GOROOT in a module whose toolchain line
// it cannot satisfy, runs a go1.99.0 found on PATH before downloading one.
// Oracle: cmd/go's toolchain switch (toolchain.Exec looks in PATH first):
// ResolveGo follows the switch and refuses the script it reaches, naming the
// candidate it started from.
func TestResolveGoRefusesAToolchainSwitchToAScriptOnPath(t *testing.T) {
	module := t.TempDir()
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.com/switch\n\ngo 1.21\n\ntoolchain go1.99.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Dir(gtScript(t, t.TempDir(), "go1.99.0", "echo ok\nexit 0\n"))
	env := gtEnv(fake+string(os.PathListSeparator)+gtRealGo(t), "GOTOOLCHAIN=auto", "GOPROXY=off")
	tool, err := langpack.ResolveGo(t.Context(), "go", module, env)
	gtRefused(t, tool, err, filepath.Join(gtRealGo(t), "go"), "`go env GOROOT` printed \"ok\\n\"")
}
