//go:build unix

package langpack_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"go/version"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
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

// LoadGoFacts reads the -race probe and the package graph only from a go
// command that ResolveGo verified. Each shim first on PATH answers `go list
// -race runtime` with "-race is not supported" and passes every other command
// to the real go, except that the second reports its own directory as GOROOT.
// Oracle: G6b: the first shim is followed to the real go, so the facts equal
// the real go's, NoRace included; the second is refused, so the graph facts
// are unavailable with an error that names it, and NoRace stays "", which
// keeps -race required. Neither shim runs as anything but `go env GOROOT`.
func TestLoadGoFactsReadsTheRaceProbeOnlyFromAVerifiedGo(t *testing.T) {
	// /usr/bin and /bin hold the C compiler that -race needs on Linux.
	real, rest := gtRealGo(t), string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin"
	files := map[string]string{"go.mod": "module example.com/s\n\ngo 1.21\n", "a/a.go": "package a\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n"}
	dir := gfModule(t, files)
	src := langpack.Source{Current: gfKeys(files), Changed: []string{"a/a.go"}}
	want := langpack.LoadGoFacts(gfCtx(t), dir, gtEnv(real+rest), src)
	if want.GraphError != "" {
		t.Fatalf("precondition: the real go gives no package graph: %s", want.GraphError)
	}
	for _, ownRoot := range []bool{false, true} {
		root, calls := t.TempDir(), filepath.Join(t.TempDir(), "calls")
		body := "printf '%s\\n' \"$*\" >> '" + calls + "'\n" +
			"case \" $* \" in *\" -race \"*) echo '-race is not supported on plan9/amd64' >&2; exit 2;; esac\n"
		if ownRoot {
			body += "if [ \"$*\" = 'env GOROOT' ]; then echo '" + root + "'; exit 0; fi\n"
		}
		shim := gtScript(t, filepath.Join(root, "bin"), "go", body+"exec '"+filepath.Join(real, "go")+"' \"$@\"\n")
		got := langpack.LoadGoFacts(gfCtx(t), dir, gtEnv(filepath.Dir(shim)+rest), src)
		if ran := gfLines(calls); !slices.Equal(ran, []string{"env", "GOROOT"}) {
			t.Errorf("shim reporting its own GOROOT %v ran as %q, want only env GOROOT", ownRoot, ran)
		}
		if !ownRoot {
			if !reflect.DeepEqual(got, want) {
				t.Errorf("facts through a shim of the real go = %+v, want the real go's %+v", got, want)
			}
			continue
		}
		if !strings.Contains(got.GraphError, "cannot verify the go command "+shim) || !strings.Contains(got.GraphError, "put the real go first on PATH") ||
			got.NoRace != "" || got.Tested != nil || got.Embedded != nil || got.Excluded != nil {
			t.Errorf("facts through a shim reporting its own GOROOT = %+v; want a GraphError refusing %s, NoRace \"\" and no graph facts", got, shim)
		}
	}
}

// ReadGoEnv runs the verified go command under the environment it is given
// and reports the configuration a check run with that environment gets.
// Oracle: cmd/go's cfg.Getenv: a GOFLAGS variable that is not empty wins, and
// otherwise the go env file GOENV names supplies it; GOOS and GOARCH follow
// the environment, and GOHOSTOS and GOHOSTARCH are the go command's own
// platform, the one this test runs on; GOFLAGS is stored redacted
// (RedactGOFLAGS). Review finding 1 on #139: pin holds the GOFLAGS read,
// unredacted, or a space for none, and GOOS and GOARCH; appended to the
// environment, they win over a go env file that says otherwise, so the go
// command reports the same configuration again.
func TestReadGoEnvReportsTheConfigurationTheGoCommandApplies(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, gtEnv(gtRealGo(t)))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	file := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(file, []byte("GOFLAGS=-exec=/bin/true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	rewritten := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(rewritten, []byte("GOFLAGS=-exec=/bin/false\nGOOS=plan9\nGOARCH=386\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	host := func(goflags, goarch string) langpack.GoEnv {
		return langpack.GoEnv{GOFLAGS: goflags, GOOS: runtime.GOOS, GOARCH: goarch, GOHOSTOS: runtime.GOOS, GOHOSTARCH: runtime.GOARCH}
	}
	pinned := func(goflags, goarch string) []string {
		return []string{"GOFLAGS=" + goflags, "GOOS=" + runtime.GOOS, "GOARCH=" + goarch}
	}
	for _, c := range []struct {
		name string
		env  []string
		want langpack.GoEnv
		pin  []string
	}{
		{"GOFLAGS from the go env file", []string{"GOENV=" + file}, host("-exec=/bin/true", runtime.GOARCH), pinned("-exec=/bin/true", runtime.GOARCH)},
		{"GOFLAGS from the environment over the file", []string{"GOENV=" + file, "GOFLAGS=-count=1 -v"}, host("-count=1 -v", runtime.GOARCH), pinned("-count=1 -v", runtime.GOARCH)},
		{"another architecture", []string{"GOENV=off", "GOARCH=" + other}, host("", other), pinned(" ", other)},
		{"linker values redacted", []string{"GOENV=off", "GOFLAGS=-ldflags=-X=main.k=secret -count=1"}, host("-ldflags=<redacted> -count=1", runtime.GOARCH), pinned("-ldflags=-X=main.k=secret -count=1", runtime.GOARCH)},
	} {
		t.Run(c.name, func(t *testing.T) {
			env := gtEnv(gtRealGo(t), c.env...)
			got, pin, _, err := langpack.ReadGoEnv(t.Context(), tool, dir, env)
			if err != nil || got != c.want || !slices.Equal(pin, c.pin) {
				t.Fatalf("ReadGoEnv = %+v, pin %q, %v; want %+v, pin %q", got, pin, err, c.want, c.pin)
			}
			again, _, _, err := langpack.ReadGoEnv(t.Context(), tool, dir, append(append(env, pin...), "GOENV="+rewritten))
			if err != nil || again != c.want {
				t.Errorf("ReadGoEnv with the pin and a go env file that says otherwise = %+v, %v; want %+v", again, err, c.want)
			}
		})
	}
}

// GoEnvKey of a ReadGoEnv read follows the configuration and holds none of
// it. Oracle (X1): cmd/go reports CGO_ENABLED and CC from the environment,
// so reads under the same environment give the same key, a different
// CGO_ENABLED or CC a different one; the key is a hex SHA-256 and contains
// no value read, such as a CC path; and it is an HMAC (review finding 1 on
// #146): another secret gives another key for the same read.
func TestReadGoEnvKeyFollowsTheConfigurationWithoutItsValues(t *testing.T) {
	dir := t.TempDir()
	tool, err := langpack.ResolveGo(t.Context(), "go", dir, gtEnv(gtRealGo(t)))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	secret := []byte("0123456789abcdef0123456789abcdef")
	const compiler = "/opt/cw-x1-compiler-marker/bin/cc"
	read := func(overrides ...string) string {
		_, _, config, err := langpack.ReadGoEnv(t.Context(), tool, dir, gtEnv(gtRealGo(t), append([]string{"GOENV=off"}, overrides...)...))
		if err != nil {
			t.Fatal(err)
		}
		key := langpack.GoEnvKey(secret, config)
		if sum, err := hex.DecodeString(key); err != nil || len(sum) != sha256.Size {
			t.Fatalf("key %q is not a hex SHA-256", key)
		}
		if other := langpack.GoEnvKey([]byte("another secret, also 32 bytes!!!"), config); other == key {
			t.Fatalf("two secrets gave the same key %s", key)
		}
		return key
	}
	off, on := read("CGO_ENABLED=0", "CC="+compiler), read("CGO_ENABLED=1", "CC="+compiler)
	if again := read("CGO_ENABLED=0", "CC="+compiler); again != off {
		t.Errorf("the same configuration gave keys %s and %s", off, again)
	}
	if off == on {
		t.Errorf("CGO_ENABLED 0 and 1 gave the same key %s", off)
	}
	if other := read("CGO_ENABLED=0", "CC=/usr/bin/other-cc"); other == off {
		t.Errorf("two CC values gave the same key %s", off)
	}
	if strings.Contains(off, "cw-x1-compiler-marker") || strings.Contains(off, hex.EncodeToString([]byte(compiler))) {
		t.Errorf("key %s holds the CC value", off)
	}
}

// ReadGoEnv fails closed. Oracle (F1): a go env that cannot be read gives no
// configuration, so the check does not run and earns no credit: a failing or
// stopped command, and output that is not exactly the values asked for with
// the four platform values set, are errors naming the go command, with no
// configuration (X1).
func TestReadGoEnvRefusesAConfigurationItCannotRead(t *testing.T) {
	stopped, cancel := context.WithCancel(t.Context())
	cancel()
	const shape = "not exactly GOFLAGS, GOOS, GOARCH, GOHOSTOS, GOHOSTARCH, CGO_ENABLED, GOEXPERIMENT, GOWORK, GOAMD64, GOARM64, CC, CXX with the four platform values set"
	complete := `"GOFLAGS":"","GOOS":"linux","GOARCH":"amd64","GOHOSTOS":"linux","CGO_ENABLED":"1","GOEXPERIMENT":"","GOWORK":"","GOAMD64":"v1","GOARM64":"v8.0","CC":"gcc","CXX":"g++"`
	for _, c := range []struct {
		name, body, want string
		ctx              context.Context
	}{
		{"failed", "echo 'go: cannot read env' >&2\nexit 1\n", "`go env` failed: \"go: cannot read env\"", t.Context()},
		{"stopped", "echo '{}'\n", "`go env` did not finish", stopped},
		{"not JSON", "echo ok\n", "not a JSON object of strings", t.Context()},
		{"a value missing", "echo '{" + complete + "}'\n", shape, t.Context()},
		{"an empty platform", "echo '{" + complete + `,"GOHOSTARCH":""}'` + "\n", shape, t.Context()},
		{"another value", "echo '{" + complete + `,"GOHOSTARCH":"amd64","GOROOT":"/x"}'` + "\n", shape, t.Context()},
	} {
		t.Run(c.name, func(t *testing.T) {
			tool := langpack.GoTool{Path: gtScript(t, t.TempDir(), "go", c.body), GOROOT: t.TempDir()}
			got, pin, config, err := langpack.ReadGoEnv(c.ctx, tool, t.TempDir(), gtEnv(gtRealGo(t)))
			if err == nil || pin != nil || config != nil || !strings.Contains(err.Error(), "cannot read the configuration of the go command "+tool.Path) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("ReadGoEnv = %+v, pin %q, %v; config %q; want no pin, no config and an error naming %s and containing %q", got, pin, err, config, tool.Path, c.want)
			}
		})
	}
}
