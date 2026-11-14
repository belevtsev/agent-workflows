package declaration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Check definition", scenario "Arguments are not interpreted by
// a shell".
func TestRunArgumentsReachTheProgramWithoutAShell(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Write("cw.toml", cwtest.DemoDeclaration+`
[[check]]
id = "shell"
run = ["echo", "$HOME", "&&", "false"]
`)
	commitAll(repo, "add shell")

	result := repo.CW("run")
	recs := records(t, repo, "shell")
	if len(recs) != 1 {
		t.Fatalf("cw run (exit %d) left %d records of shell, want 1:\n%s", result.Code, len(recs), result.Output())
	}
	if recs[0].Outcome != "passed" {
		t.Errorf("the outcome of shell is %q, want passed:\n%s", recs[0].Outcome, result.Output())
	}
	data, err := os.ReadFile(filepath.Join(repo.StoreDir(), filepath.FromSlash(recs[0].Log)))
	if err != nil {
		t.Fatalf("the log of shell: %v", err)
	}
	found := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimRight(line, "\r") == "$HOME && false" {
			found = true
		}
	}
	if !found {
		t.Errorf("the log of shell does not hold the line `$HOME && false`:\n%s", data)
	}
}

// Requirement "Check definition", scenario "The timeout defaults to ten
// minutes".
func TestTheTimeoutDefaultsToTenMinutes(t *testing.T) {
	if os.Getenv("CW_SLOW_TESTS") != "1" {
		t.Skip("waits ten minutes for the default timeout; set CW_SLOW_TESTS=1 and go test -timeout 15m to run it")
	}
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < 11*time.Minute {
		t.Fatal("the test needs more than ten minutes; run go test with -timeout 15m or more")
	}
	t.Parallel()
	repo := cwtest.Demo(t)
	// lint has no timeout; its program now does not end.
	repo.Write("lint.sh", "while :; do sleep 1; done\n")
	commitAll(repo, "lint never ends")

	result := repo.CW("run", "lint")
	recs := records(t, repo, "lint")
	if len(recs) != 1 {
		t.Fatalf("cw run lint (exit %d) left %d records of lint, want 1:\n%s", result.Code, len(recs), result.Output())
	}
	if recs[0].Outcome != "timed_out" {
		t.Errorf("the outcome of lint is %q, want timed_out", recs[0].Outcome)
	}
	started, err := time.Parse(time.RFC3339Nano, recs[0].StartedAt)
	if err != nil {
		t.Fatalf("started_at: %v", err)
	}
	ended, err := time.Parse(time.RFC3339Nano, recs[0].EndedAt)
	if err != nil {
		t.Fatalf("ended_at: %v", err)
	}
	if took := ended.Sub(started); took < 10*time.Minute || took > 10*time.Minute+30*time.Second {
		t.Errorf("lint ended %v after it started, want ten minutes", took)
	}
}
