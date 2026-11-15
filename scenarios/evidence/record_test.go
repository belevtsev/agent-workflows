package evidence_test

import (
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "What a run records", scenario "The record of a failed run".
func TestRecordOfAFailedRun(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	repo.Write("src/app.txt", "broken\n")
	script := repo.Run("", "sh", "unit.sh").Stdout
	if strings.TrimSpace(script) == "" {
		t.Fatal("unit.sh prints nothing, so its output cannot be found in the log")
	}

	run := repo.Gatecub("run", "unit")
	out := status(t, repo)

	unit := out.check(t, "unit")
	if unit.Outcome != "failed" {
		t.Errorf("outcome %q, want failed", unit.Outcome)
	}
	if unit.ExitStatus == nil || *unit.ExitStatus != 3 {
		t.Errorf("exit_status %s, want 3", exitStatus(unit))
	}
	if unit.Tree == "" || unit.Tree != out.Tree {
		t.Errorf("tree of the run %q, top-level tree %q; want them equal", unit.Tree, out.Tree)
	}
	if unit.Log == "" {
		t.Errorf("the entry names no log")
	} else if data, err := os.ReadFile(logPath(repo, unit.Log)); err != nil {
		t.Errorf("the log cannot be read: %v", err)
	} else if !strings.Contains(string(data), script) {
		t.Errorf("the log does not hold the output of unit.sh %q:\n%s", script, data)
	}
	if t.Failed() {
		t.Logf("entry: %+v\noutput of gatecub run unit (exit status %d):\n%s", unit, run.Code, run.Output())
	}
}
