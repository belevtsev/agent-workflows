package records_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Damaged and concurrent records", scenario "A truncated record".
func TestTruncatedRecordCountsAsNoPass(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	passed := recordWithOutcome(t, repo, "lint", "passed")
	truncateToHalf(t, passed)

	status := repo.CW("status")
	if !strings.Contains(status.Output(), filepath.Base(passed)) {
		t.Errorf("cw status does not warn with the path of %s:\n%s", passed, status.Output())
	}
	assertState(t, status, "lint", "not run")
	assertState(t, status, "unit", "satisfied")
}

// Requirement "Damaged and concurrent records", scenario "A damaged record
// hides no failure".
func TestDamagedRecordHidesNoFailure(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	flag := envFlag(t)
	addEnvCheck(repo, "test", "-e", flag)
	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")
	removeFile(t, flag)
	mustRun(t, repo, 5, "run", "env", "--force")
	truncateToHalf(t, recordWithOutcome(t, repo, "env", "failed"))

	assertState(t, repo.CW("status"), "env", "not run")

	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")
	assertState(t, repo.CW("status"), "env", "satisfied")
}

// Requirement "Damaged and concurrent records", scenario "Two runs at once".
func TestTwoRunsAtOnceLoseNoRecord(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Git("worktree", "add", "-q", "../first")
	repo.Git("worktree", "add", "-q", "../second")

	unit := startCW(t, repo.In("../first"), "run", "unit", "--force")
	lint := startCW(t, repo.In("../second"), "run", "lint", "--force")
	if code, output := unit.wait(t); code != 0 {
		t.Errorf("cw run unit --force: exit status %d, want 0 (passed)\n%s", code, output)
	}
	if code, output := lint.wait(t); code != 0 {
		t.Errorf("cw run lint --force: exit status %d, want 0 (passed)\n%s", code, output)
	}

	status := repo.CW("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "satisfied")
}
