package main_test

import (
	"crypto/rand"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// cgoAlpha makes package alpha a cgo package, so go test compiles C code for
// it with the C compiler and links its test binary with it. The nonce keeps
// go's build cache from reusing an object compiled by another run.
func cgoAlpha(t *testing.T) string {
	t.Helper()
	return "package alpha\n\n// /* nonce " + rand.Text() + " */\nimport \"C\"\n"
}

// systemCC returns the absolute path of the C compiler that go env reports
// for the fixture's PATH: gcc on Linux, clang on macOS.
func (e *goToolchainE2E) systemCC() string {
	e.t.Helper()
	command := exec.CommandContext(e.t.Context(), goTool(e.t), "env", "CC")
	command.Dir, command.Env = e.files, append(e.env, "GOENV=off")
	output, err := command.Output()
	if err != nil {
		e.t.Fatalf("go env CC: %v", err)
	}
	name := strings.TrimSpace(string(output))
	for _, entry := range e.env {
		if list, ok := strings.CutPrefix(entry, "PATH="); ok {
			for _, dir := range filepath.SplitList(list) {
				if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
					return path
				}
			}
		}
	}
	e.t.Fatalf("precondition: the C compiler %q that go env reports is not on the fixture's PATH", name)
	return ""
}

// A wrapper C compiler that the user wrote compiles every C file with a
// constructor that prints PASS and exits 0 before any Go code runs, so
// alpha's test binary reports a pass although alpha's test fails whenever it
// runs. Threat (#139 review finding 2): cw verified the go command but not
// the C compiler that go test runs for cgo, so this run passed and earned
// full credit. Oracle (F18): with cgo on, the Go rule credits a run only when
// cw verified its C toolchain, which a compiler in a directory the user owns
// fails; the run still passes (the repro is real), verify says why it earns
// no credit, and go_env records the wrapper by its path.
func TestTaskRunWithAWrapperCCompilerThatFakesAPassEarnsNoCredit(t *testing.T) {
	e := newGoToolchainE2E(t)
	header := e.file("pass.h", "long write(int, const void *, unsigned long);\nvoid _exit(int);\n__attribute__((constructor)) static void cw_fake_pass(void) { if (write(1, \"PASS\\n\", 5) < 0) {} _exit(0); }\n")
	// The wrapper adds the header to C sources only: runtime/cgo also has
	// assembly files.
	cc := e.systemCC()
	wrapper := filepath.Join(e.fake("cc-wrapper", "for arg in \"$@\"; do case \"$arg\" in *.c) exec '"+cc+"' -include '"+header+"' \"$@\";; esac; done\nexec '"+cc+"' \"$@\"\n"), "cc-wrapper")
	e.write("alpha/alpha.go", brokenAlpha)
	e.write("alpha/cgo.go", cgoAlpha(t))
	code, stderr := e.runCode("alpha", "CGO_ENABLED=1", "CC="+wrapper)
	limitations := e.expectNoAlphaCredit(code, stderr)
	if status := e.latestCheckRun("alpha").Status; status != "passed" || !strings.Contains(e.log("alpha"), "ok") {
		t.Fatalf("precondition: the run under the wrapper is %s with log %q, want passed: the wrapper no longer fakes alpha's pass (cw task run exited %d: %s)", status, e.log("alpha"), code, stderr)
	}
	if !slices.ContainsFunc(limitations, func(line string) bool {
		return strings.HasPrefix(line, "check alpha passed, but earns no Go evidence credit: cgo was on") && strings.Contains(line, wrapper)
	}) {
		t.Errorf("verify limitations %q do not say that check alpha's C compiler %s did not verify", limitations, wrapper)
	}
	judged, _ := e.storedGoEnv("alpha")["c"].(map[string]any)
	if cc, _ := judged["cc"].(map[string]any); judged["cgo"] != true || cc["path"] != wrapper {
		t.Errorf("the passed run stores go_env c %v, want cgo on and cc %s", judged, wrapper)
	}
}

// The ordinary setup: no CC set, so go test runs the system compiler on
// PATH, gcc in /usr/bin on Linux and the xcrun shim /usr/bin/clang on macOS.
// Oracle (F18 constraint): cw verifies a compiler that only root can change,
// so a cgo package's passing run keeps its credit, and go_env records cgo on,
// that compiler and nothing unverified.
func TestTaskRunWithTheSystemCCompilerEarnsCredit(t *testing.T) {
	e := newGoToolchainE2E(t)
	e.write("alpha/cgo.go", cgoAlpha(t))
	e.runExpect(true, "alpha", "CGO_ENABLED=1")
	if alpha := packageProblem(t, "alpha"); slices.Contains(e.verify().Problems, alpha) {
		t.Fatalf("verify gives a passed cgo run with the system C compiler no credit: problems hold %q; go_env %v", alpha, e.storedGoEnv("alpha"))
	}
	judged, _ := e.storedGoEnv("alpha")["c"].(map[string]any)
	if cc, _ := judged["cc"].(map[string]any); judged["cgo"] != true || cc["path"] != e.systemCC() || judged["unverified"] != nil {
		t.Errorf("the passed run stores go_env c %v, want cgo on, cc %s and nothing unverified", judged, e.systemCC())
	}
}
