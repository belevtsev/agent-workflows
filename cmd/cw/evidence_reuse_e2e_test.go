package main_test

import (
	json "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Contract tests for the reuse of a passed check run, driven through the
// real cw binary: a pass stays creditable while the source digest, go
// command, GOFLAGS and environment key are unchanged, and cw task run then
// runs nothing.

// newReuseE2E starts the evidence plan, changes package alpha and passes its
// check, and returns the fixture, the alpha package problem and the problems
// verify reports right after the pass.
func newReuseE2E(t *testing.T) (*evidenceE2E, string, []string) {
	e := newEvidenceE2E(t)
	e.setUpModule()
	task, code, output := e.start(e.store, "plan.json", evidencePlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	alpha := packageProblem(t, "alpha")
	e.write("alpha/alpha.go", alphaSource+"\nfunc Other() string { return \"other\" }\n")
	e.expectProblems("alpha changed, not run", []string{alpha}, nil)
	e.runExpect(true, "alpha")
	passed := e.verifyEnv().Problems
	if slices.Contains(passed, alpha) {
		t.Fatalf("alpha passed: problems %q include %q", passed, alpha)
	}
	return e, alpha, passed
}

// executedRuns counts the runs the task recorded.
func (e *evidenceE2E) executedRuns() int {
	e.t.Helper()
	record, err := taskflow.Load(e.task)
	if err != nil {
		e.t.Fatalf("load task: %v", err)
	}
	return len(record.Checks)
}

// verifyEnv is verify with extra environment entries.
func (e *evidenceE2E) verifyEnv(extra ...string) taskflow.Verification {
	e.t.Helper()
	_, stdout, stderr := e.cwEnv(extra, "task", "verify", "--full", "--task", e.task)
	var verification taskflow.Verification
	if err := json.Unmarshal([]byte(stdout), &verification); err != nil {
		e.t.Fatalf("verify output is not a verification: %v\nstdout %s\nstderr %s", err, stdout, stderr)
	}
	return verification
}

// runStatus runs check with flags and extra environment entries and returns
// its exit code and printed status.
func (e *evidenceE2E) runStatus(check string, flags []string, extra ...string) (int, string) {
	e.t.Helper()
	code, stdout, stderr := e.cwEnv(extra, append([]string{"task", "run", "--task", e.task, "--check", check}, flags...)...)
	var summary struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal([]byte(stdout), &summary); err != nil {
		e.t.Fatalf("run %s output is not a summary: %v\nstdout %s\nstderr %s", check, err, stdout, stderr)
	}
	return code, summary.Status
}

func reusedLimitation(verification taskflow.Verification, check string) bool {
	return slices.ContainsFunc(verification.Limitations, func(line string) bool { return strings.HasPrefix(line, "check "+check+" reuses run ") })
}

// Oracle: after a commit of the passed bytes, verify reports the same problems
// as at the pass, with no new run, and lists the pass as reused; cw task run prints status reused, exits 0
// and records nothing, unless --force; CW_NO_REUSE=1 withdraws the credit.
func TestReuseE2EPassCommitVerifyNeedsNoRerun(t *testing.T) {
	t.Parallel()
	e, alpha, passed := newReuseE2E(t)
	e.git("add", "-A")
	e.git("commit", "-qm", "alpha")
	runs := e.executedRuns()
	verification := e.verifyEnv()
	if !slices.Equal(verification.Problems, passed) || !reusedLimitation(verification, "alpha") {
		t.Errorf("after commit: problems %q, limitations %q; want the problems at the pass, %q, and alpha reused", verification.Problems, verification.Limitations, passed)
	}
	if code, status := e.runStatus("alpha", nil); code != 0 || status != "reused" {
		t.Errorf("cw task run after commit: exit %d, status %q; want 0, reused", code, status)
	}
	if got := e.executedRuns(); got != runs {
		t.Errorf("executed runs went from %d to %d; want no new run", runs, got)
	}
	if verification := e.verifyEnv("CW_NO_REUSE=1"); !slices.Contains(verification.Problems, alpha) {
		t.Errorf("CW_NO_REUSE=1: problems %q lack %q", verification.Problems, alpha)
	}
	if code, status := e.runStatus("alpha", []string{"--force"}); code != 0 || status != "passed" || e.executedRuns() != runs+1 {
		t.Errorf("cw task run --force: exit %d, status %q, %d runs; want 0, passed, %d", code, status, e.executedRuns(), runs+1)
	}
}

// Oracle: one changed byte after the pass makes verify demand a run, and cw
// task run executes the check.
func TestReuseE2EOneByteEditDemandsARun(t *testing.T) {
	t.Parallel()
	e, alpha, _ := newReuseE2E(t)
	e.write("alpha/alpha.go", alphaSource+"\nfunc Other() string { return \"otheR\" }\n")
	runs := e.executedRuns()
	if verification := e.verifyEnv(); !slices.Contains(verification.Problems, alpha) || reusedLimitation(verification, "alpha") {
		t.Errorf("after a one-byte edit: problems %q, limitations %q; want %q and no reuse", verification.Problems, verification.Limitations, alpha)
	}
	if code, status := e.runStatus("alpha", nil); code != 0 || status != "passed" || e.executedRuns() != runs+1 {
		t.Errorf("cw task run after the edit: exit %d, status %q, %d runs; want 0, passed, %d", code, status, e.executedRuns(), runs+1)
	}
}

const reuseGitPlan = `{"title":"Whitespace","kind":"small",
 "requirements":[{"id":"clean","description":"No whitespace errors since base","check_ids":["whitespace"],"allow_non_execution":false}],
 "checks":[{"id":"whitespace","argv":["git","diff","--check","base","HEAD"],"timeout_seconds":60}]}`

// Oracle: a git check reads HEAD, which the source digest does not cover.
// git diff --check base HEAD passes while trailing whitespace is uncommitted;
// after git commit -am of the same bytes, cw task run does not reuse the pass
// but runs the check, which now fails (exit 5), and verify lists no reuse.
func TestReuseE2EGitCheckIsNeverReused(t *testing.T) {
	t.Parallel()
	e := newEvidenceE2E(t)
	e.setUpModule()
	e.git("tag", "base")
	task, code, output := e.start(e.store, "plan.json", reuseGitPlan)
	if code != 0 {
		t.Fatalf("start: exit %d: %s", code, output)
	}
	e.task = task
	e.write("alpha/alpha.go", alphaSource+"func Spaced() {}   \n")
	if code, status := e.runStatus("whitespace", nil); code != 0 || status != "passed" {
		t.Fatalf("whitespace before the commit: exit %d, status %q; want 0, passed", code, status)
	}
	e.git("commit", "-qam", "trailing whitespace")
	runs := e.executedRuns()
	if code, status := e.runStatus("whitespace", nil); code != 5 || status != "failed" || e.executedRuns() != runs+1 {
		t.Errorf("whitespace after the commit: exit %d, status %q, %d runs; want 5, failed, %d", code, status, e.executedRuns(), runs+1)
	}
	if verification := e.verifyEnv(); reusedLimitation(verification, "whitespace") {
		t.Errorf("verify lists the git check as reused: %q", verification.Limitations)
	}
}

// Oracle: under another GOFLAGS the committed pass earns no credit, and cw
// task run executes the check.
func TestReuseE2EOtherGOFLAGSGetNoCredit(t *testing.T) {
	t.Parallel()
	e, alpha, _ := newReuseE2E(t)
	e.git("add", "-A")
	e.git("commit", "-qm", "alpha")
	runs := e.executedRuns()
	if verification := e.verifyEnv("GOFLAGS=-count=1"); !slices.Contains(verification.Problems, alpha) || reusedLimitation(verification, "alpha") {
		t.Errorf("under GOFLAGS=-count=1: problems %q, limitations %q; want %q and no reuse", verification.Problems, verification.Limitations, alpha)
	}
	if code, status := e.runStatus("alpha", nil, "GOFLAGS=-count=1"); code != 0 || status != "passed" || e.executedRuns() != runs+1 {
		t.Errorf("cw task run under GOFLAGS=-count=1: exit %d, status %q, %d runs; want 0, passed, %d", code, status, e.executedRuns(), runs+1)
	}
}
