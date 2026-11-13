package main_test

import (
	"go/version"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// goToolchainE2E is an evidence fixture whose go checks run with a fake go
// command first on cw's PATH. Each fake records its arguments, one call per
// line, in calls.
type goToolchainE2E struct {
	*evidenceE2E
	calls string
}

func newGoToolchainE2E(t *testing.T) *goToolchainE2E {
	t.Helper()
	e := &goToolchainE2E{evidenceE2E: newEvidenceE2E(t), calls: filepath.Join(hooksCanonicalTempDir(t), "calls")}
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", evidencePlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	return e
}

// fake writes an executable named name that records its arguments and then
// runs body, and returns the PATH entry holding it.
func (e *goToolchainE2E) fake(name, body string) string {
	e.t.Helper()
	dir := hooksCanonicalTempDir(e.t)
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> '" + e.calls + "'\n" + body
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		e.t.Fatal(err)
	}
	return dir
}

// runCode runs check with extra environment entries and returns cw's exit
// code and standard error.
func (e *goToolchainE2E) runCode(check string, extra ...string) (int, string) {
	e.t.Helper()
	code, _, stderr := e.cwEnv(extra, "task", "run", "--task", e.task, "--check", check, "--summary")
	return code, stderr
}

// fakeCalls returns the argument lines the fakes recorded.
func (e *goToolchainE2E) fakeCalls() []string {
	data, _ := os.ReadFile(e.calls)
	return strings.Fields(strings.ReplaceAll(strings.TrimSpace(string(data)), " ", "_"))
}

// storedTool returns the tool member of the latest stored run of check, or
// nil when the run has none. It reads the JSON so that the test also builds
// against a cw without the member.
func (e *goToolchainE2E) storedTool(check string) map[string]any {
	e.t.Helper()
	runs := e.storedRunMembers(check)
	if len(runs) == 0 {
		e.t.Fatalf("no stored run of check %s", check)
	}
	tool, _ := runs[len(runs)-1]["tool"].(map[string]any)
	return tool
}

func (e *goToolchainE2E) log(check string) string {
	e.t.Helper()
	data, err := os.ReadFile(filepath.Join(e.task, e.latestCheckRun(check).Log))
	if err != nil {
		e.t.Fatal(err)
	}
	return string(data)
}

// The review's repro: PATH=/fake:$PATH cw task run, where /fake/go answers
// every command with ok. Oracle: G6's acceptance: the run is recorded as
// failed with failure untrusted_toolchain and no exit code, cw exits 5 and
// names the fake, and the fake runs once, as `go env GOROOT`, never as the
// check; the check's log is empty.
func TestTaskRunRefusesAFakeGoFirstOnPath(t *testing.T) {
	t.Parallel()
	e := newGoToolchainE2E(t)
	fake := e.fake("go", "echo ok\nexit 0\n")
	code, stderr := e.runCode("alpha", "PATH="+fake+":"+filepath.Dir(goTool(t))+":/usr/bin:/bin")
	run, tool := e.latestCheckRun("alpha"), e.storedTool("alpha")
	if code != 5 || run.Status != "failed" || run.Failure != "untrusted_toolchain" || run.ExitCode != nil || tool != nil {
		exit := "none"
		if run.ExitCode != nil {
			exit = strconv.Itoa(*run.ExitCode)
		}
		t.Fatalf("exit %d, run %s/%s exit code %s tool %v; want exit 5 and a failed untrusted_toolchain run that never started\nstderr %s", code, run.Status, run.Failure, exit, tool, stderr)
	}
	if !strings.Contains(stderr, filepath.Join(fake, "go")) || !strings.Contains(stderr, "put the real go first on PATH") {
		t.Errorf("stderr %q does not name the fake and say what to do", stderr)
	}
	if calls := e.fakeCalls(); !slices.Equal(calls, []string{"env_GOROOT"}) {
		t.Errorf("fake go ran as %q, want only env GOROOT", calls)
	}
	if log := e.log("alpha"); log != "" {
		t.Errorf("refused run's log holds %q, want it empty", log)
	}
}

// A shim that reports the real GOROOT, without running go, under a
// GOTOOLCHAIN that names a fake go1.99.0 on PATH. Oracle: G6's acceptance:
// cw runs the real go from that GOROOT with GOTOOLCHAIN=local, so the run
// passes with the real go test's output, and neither the shim nor the
// go1.99.0 that the real go would otherwise switch to runs the check.
func TestTaskRunRunsTheRealGoThatAShimReports(t *testing.T) {
	t.Parallel()
	e := newGoToolchainE2E(t)
	root := evidenceGoEnv(t, "GOROOT")
	shim := e.fake("go", "if [ \"$*\" = 'env GOROOT' ]; then echo '"+root+"'; else echo 'ok  fake pass'; fi\nexit 0\n")
	switched := e.fake("go1.99.0", "echo 'ok  fake pass'\nexit 0\n")
	if code, stderr := e.runCode("alpha", "PATH="+shim+":"+switched+":/usr/bin:/bin", "GOTOOLCHAIN=go1.99.0"); code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %s", code, stderr)
	}
	want, err := filepath.EvalSymlinks(filepath.Join(root, "bin", "go"))
	if err != nil {
		t.Fatal(err)
	}
	if run, tool := e.latestCheckRun("alpha"), e.storedTool("alpha"); run.Status != "passed" || tool["path"] != want || tool["goroot"] != filepath.Dir(filepath.Dir(want)) {
		t.Fatalf("run %s with tool %v, want passed with %s from its own tree", run.Status, tool, want)
	}
	if log := e.log("alpha"); !strings.Contains(log, "example.com/evidence/alpha") || strings.Contains(log, "fake pass") {
		t.Errorf("log %q, want the real go test output for alpha", log)
	}
	if calls := e.fakeCalls(); !slices.Equal(calls, []string{"env_GOROOT"}) {
		t.Errorf("fakes ran as %q, want only the shim, as env GOROOT", calls)
	}
}

// GOTOOLCHAIN names a version the real go is not, so it switches, and finds
// a fake go1.99.0 on PATH before it would download one. Oracle: cmd/go's
// toolchain switch looks in PATH first; G6 verifies the go command the switch
// selects, so the run is refused and the fake never runs the check.
func TestTaskRunRefusesAToolchainSwitchToAFakeOnPath(t *testing.T) {
	t.Parallel()
	e := newGoToolchainE2E(t)
	fake := e.fake("go1.99.0", "echo ok\nexit 0\n")
	code, stderr := e.runCode("alpha", "PATH="+fake+":"+filepath.Dir(goTool(t))+":/usr/bin:/bin", "GOTOOLCHAIN=go1.99.0")
	if run := e.latestCheckRun("alpha"); code != 5 || run.Status != "failed" || run.Failure != "untrusted_toolchain" {
		t.Fatalf("exit %d, run %s/%s; want exit 5 and a failed untrusted_toolchain run\nstderr %s", code, run.Status, run.Failure, stderr)
	}
	if calls := e.fakeCalls(); !slices.Equal(calls, []string{"env_GOROOT"}) {
		t.Errorf("fake go1.99.0 ran as %q, want only env GOROOT", calls)
	}
}

// The reviewer's attack on #119: a GOROOT made of links to the real tree plus
// a go.env of its own that sets GOFLAGS=-exec=/bin/true, so no test binary
// runs, named by the GOROOT variable or printed by a shim, while alpha's test
// fails. Oracle: review finding 1: cw accepts only the go command's own tree,
// so the run is refused as untrusted_toolchain, never recorded as passed.
func TestTaskRunRefusesAGOROOTOutsideTheGoCommandsTree(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		env  func(e *goToolchainE2E, root string) []string
	}{
		{"GOROOT variable", func(_ *goToolchainE2E, root string) []string { return []string{"GOROOT=" + root} }},
		{"shim", func(e *goToolchainE2E, root string) []string {
			return []string{"PATH=" + e.fake("go", "echo '"+root+"'\n") + ":/usr/bin:/bin"}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newGoToolchainE2E(t)
			tree, links := evidenceGoEnv(t, "GOROOT"), hooksCanonicalTempDir(t)
			entries, err := os.ReadDir(tree)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if entry.Name() != "go.env" {
					if err := os.Symlink(filepath.Join(tree, entry.Name()), filepath.Join(links, entry.Name())); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := os.WriteFile(filepath.Join(links, "go.env"), []byte("GOTOOLCHAIN=local\nGOFLAGS=-exec=/bin/true\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			e.write("alpha/alpha.go", "package alpha\n\nfunc Name() string { return \"broken\" }\n")
			code, stderr := e.runCode("alpha", tc.env(e, links)...)
			if run := e.latestCheckRun("alpha"); code != 5 || run.Status != "failed" || run.Failure != "untrusted_toolchain" {
				t.Fatalf("exit %d, run %s/%s; want exit 5 and a failed untrusted_toolchain run\nstderr %s", code, run.Status, run.Failure, stderr)
			}
		})
	}
}

// A normal run. Oracle: G6's acceptance: the record stores tool.path and
// tool.version (the go command that ran, in the runtime.Version() family) and
// verify lists that go command and its GOROOT as a limitation.
func TestTaskRunRecordsTheGoCommandThatRanIt(t *testing.T) {
	t.Parallel()
	e := newGoToolchainE2E(t)
	e.runExpect(true, "alpha")
	want, err := filepath.EvalSymlinks(goTool(t))
	if err != nil {
		t.Fatal(err)
	}
	tool := e.storedTool("alpha")
	toolVersion, _ := tool["version"].(string)
	if sum, _ := tool["sha256"].(string); tool["path"] != want || version.Lang(toolVersion) != version.Lang(runtime.Version()) || len(sum) != 64 {
		t.Fatalf("stored tool %v, want path %s, a %s version and a SHA-256", tool, want, version.Lang(runtime.Version()))
	}
	limitation := "check alpha ran " + toolVersion + " at " + want + " with GOROOT " + filepath.Dir(filepath.Dir(want)) + "; cw verified that it is a build of cmd/go, not that it or its GOROOT is unmodified"
	if limitations := e.verify().Limitations; !slices.Contains(limitations, limitation) {
		t.Errorf("verify limitations %q lack %q", limitations, limitation)
	}
}
