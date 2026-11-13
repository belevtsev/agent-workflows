package taskflow_test

import (
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestTraceJudgesAReusedRun maps a passing test, runs its check, then
// commits the same bytes. Oracle: the criterion is proven by that run before
// and after the commit, because the trace judges a reused run as current.
func TestTraceJudgesAReusedRun(t *testing.T) {
	f := l4New(t, l4Opts{reqs: []string{"C1"}, checks: []taskflow.Check{l3Check("ok", l3JSONP...)}})
	f.forgeRound(true, l3Map("R1", l3Mapped("p/te_test.go", "TestPass")))
	if run := f.run("ok"); run.Status != "passed" {
		t.Fatalf("fixture run %s", run.Status)
	}
	if got := l3Criterion(t, f.trace(), "C1").Status; got != "proven" {
		t.Fatalf("before the commit: criterion %s, want proven", got)
	}
	l3Git(t, f.repo, "add", "-A")
	l3Git(t, f.repo, "-c", "user.name=fixture", "-c", "user.email=fixture@example.com", "commit", "-qm", "same bytes")
	if got := l3Criterion(t, f.trace(), "C1").Status; got != "proven" {
		t.Fatalf("after a commit of the same bytes: criterion %s, want proven", got)
	}
}
