package main_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// brokenAlpha makes alpha's test fail whenever its test binary runs.
const brokenAlpha = "package alpha\n\nfunc Name() string { return \"broken\" }\n"

// expectNoAlphaCredit fails unless the latest run of check alpha, made after
// brokenAlpha with exit code and standard error from cw task run, failed or
// earned no Go rule credit, and returns verify's limitations. The oracle is
// alpha's plain package problem: a credited run of alpha clears it (leaving
// at most its -race problem), and a failed or uncredited one does not.
func (e *goToolchainE2E) expectNoAlphaCredit(code int, stderr string) []string {
	e.t.Helper()
	run := e.latestCheckRun("alpha")
	verification := e.verify()
	if alpha := packageProblem(e.t, "alpha"); !slices.Contains(verification.Problems, alpha) {
		e.t.Fatalf("cw task run exited %d with run %s, and verify credits it for alpha although alpha's test fails whenever it runs: problems lack %q\nproblems %q\nstderr %s", code, run.Status, alpha, verification.Problems, stderr)
	}
	return verification.Limitations
}

// storedGoEnv returns the go_env member of the latest stored run of check, or
// nil when it has none. It reads the JSON, so the test builds against a cw
// without the member.
func (e *goToolchainE2E) storedGoEnv(check string) map[string]any {
	e.t.Helper()
	runs := e.storedRunMembers(check)
	if len(runs) == 0 {
		e.t.Fatalf("no stored run of check %s", check)
	}
	env, _ := runs[len(runs)-1]["go_env"].(map[string]any)
	return env
}

// A go env file sets GOFLAGS=-exec=/bin/true, and cw's environment sets an
// empty GOFLAGS, so cmd/go applies the file's: go test hands alpha's test
// binary to /bin/true, which exits 0 without running it. Threat: the Go rule
// read GOFLAGS only from the environment, so this run passed and earned
// credit. Oracle (F1): the rule credits only the GOFLAGS that the verified go
// command reports with go env under the check's environment, so the run earns
// no credit, and it records that GOFLAGS under go_env.
func TestTaskRunUnderAGoEnvFileThatSkipsTestsEarnsNoCredit(t *testing.T) {
	for _, tc := range []struct {
		name string
		// configure writes the go env file and returns the extra environment
		// of cw task run.
		configure func(e *goToolchainE2E) []string
	}{
		{"GOENV names the file", func(e *goToolchainE2E) []string {
			file := filepath.Join(hooksCanonicalTempDir(e.t), "env")
			if err := os.WriteFile(file, []byte("GOFLAGS=-exec=/bin/true\n"), 0o644); err != nil {
				e.t.Fatal(err)
			}
			return []string{"GOENV=" + file}
		}},
		{"go env -w writes the user's file", func(e *goToolchainE2E) []string {
			command := exec.CommandContext(e.t.Context(), goTool(e.t), "env", "-w", "GOFLAGS=-exec=/bin/true")
			command.Dir, command.Env = e.files, e.env
			if output, err := command.CombinedOutput(); err != nil {
				e.t.Fatalf("go env -w: %v\n%s", err, output)
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newGoToolchainE2E(t)
			e.write("alpha/alpha.go", brokenAlpha)
			code, stderr := e.runCode("alpha", tc.configure(e)...)
			e.expectNoAlphaCredit(code, stderr)
			if status := e.latestCheckRun("alpha").Status; status != "passed" {
				t.Fatalf("precondition: the run under the go env file is %s, want passed: /bin/true no longer stands in for alpha's test (cw task run exited %d: %s)", status, code, stderr)
			}
			if env := e.storedGoEnv("alpha"); env["goflags"] != "-exec=/bin/true" {
				t.Errorf("the passed run stores go_env %v, want goflags -exec=/bin/true", env)
			}
		})
	}
}

// GOARCH names an architecture other than the host's, and PATH holds a
// go_<goos>_<goarch>_exec program that exits 0: go test hands each
// cross-compiled test binary to it, so alpha's test never runs. Threat: the
// Go rule never looked at GOOS or GOARCH, so this run passed and earned
// credit. Oracle (F1): the rule credits only a run whose go env GOOS and
// GOARCH are its go command's host's, so the run earns no credit and verify
// says why; the run passed and the exec program ran, so the repro is real
// (review finding 5 on #139: a run that stops reproducing fails the test).
func TestTaskRunForAnotherPlatformEarnsNoCredit(t *testing.T) {
	e := newGoToolchainE2E(t)
	goarch := "arm64"
	if runtime.GOARCH == "arm64" {
		goarch = "amd64"
	}
	wrapper := e.fake("go_"+runtime.GOOS+"_"+goarch+"_exec", "exit 0\n")
	e.write("alpha/alpha.go", brokenAlpha)
	code, stderr := e.runCode("alpha", "PATH="+wrapper+":"+filepath.Dir(goTool(t))+":/usr/bin:/bin", "GOARCH="+goarch)
	limitations := e.expectNoAlphaCredit(code, stderr)
	if status := e.latestCheckRun("alpha").Status; status != "passed" {
		t.Fatalf("precondition: the cross-platform run is %s, want passed: the exec program no longer stands in for alpha's test (cw task run exited %d: %s)", status, code, stderr)
	}
	if len(e.fakeCalls()) == 0 {
		t.Fatal("the run passed, but go test never ran the exec program")
	}
	platform := runtime.GOOS + "/" + goarch
	if !slices.ContainsFunc(limitations, func(line string) bool {
		return strings.HasPrefix(line, "check alpha passed, but earns no Go evidence credit") && strings.Contains(line, platform)
	}) {
		t.Errorf("verify limitations %q do not say why check alpha, built for %s, earns no credit", limitations, platform)
	}
	if env := e.storedGoEnv("alpha"); env["goos"] != runtime.GOOS || env["goarch"] != goarch {
		t.Errorf("the passed run stores go_env %v, want goos %s and goarch %s", env, runtime.GOOS, goarch)
	}
}
