package records_test

import (
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Satisfied check", scenario "A run on the same tree satisfies".
func TestRunOnTheSameTreeSatisfies(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)

	status := repo.CW("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "satisfied")
}

// Requirement "Satisfied check", scenario "A change outside `paths` keeps a
// check satisfied".
func TestChangeOutsidePathsKeepsACheckSatisfied(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	repo.Write("README.md", "# demo\n\nedited\n")

	status := repo.CW("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "not run")
}

// Requirement "Satisfied check", scenario "A change inside `paths` does not".
func TestChangeInsidePathsNeedsANewRun(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	repo.Write("src/app.txt", "ok \n")

	assertState(t, repo.CW("status"), "unit", "not run")
}

// Requirement "Satisfied check", scenario "A changed command needs a new run".
func TestChangedCommandNeedsANewRun(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	declaration := repo.Read("cw.toml")
	changed := strings.Replace(declaration, `run = ["sh", "unit.sh"]`, `run = ["sh", "unit.sh", "-v"]`, 1)
	if changed == declaration {
		t.Fatalf("the run of unit is not in cw.toml:\n%s", declaration)
	}
	repo.Write("cw.toml", changed)

	assertState(t, repo.CW("status"), "unit", "not run")
}

// Requirement "Satisfied check", scenario "A later pass replaces an earlier
// failure on the same content".
func TestLaterPassReplacesAnEarlierFailure(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	flag := envFlag(t)
	addEnvCheck(repo, "test", "-e", flag)
	mustRun(t, repo, 5, "run", "env")
	before := len(repo.RecordFiles("env"))

	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")

	if after := len(repo.RecordFiles("env")); after != before+1 {
		t.Errorf("the program was not executed again: %d records before the second run, %d after", before, after)
	}
	recordWithOutcome(t, repo, "env", "passed")
	assertState(t, repo.CW("status"), "env", "satisfied")
}

// Requirement "Satisfied check", scenario "A later failure replaces an earlier
// pass".
func TestLaterFailureReplacesAnEarlierPass(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	flag := envFlag(t)
	addEnvCheck(repo, "test", "-e", flag)
	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")

	removeFile(t, flag)
	mustRun(t, repo, 5, "run", "env", "--force")

	assertState(t, repo.CW("status"), "env", "failed")
}

// Requirement "Satisfied check", scenario "Two runs that overlap".
func TestOverlappingRunsAreOrderedByTheirEnd(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	flag := envFlag(t)
	// The program waits until build/go exists in its own worktree; build/ is
	// ignored, so the worktrees keep the same content.
	addEnvCheck(repo, "sh", "-c", "mkdir -p build && touch build/started && while [ ! -e build/go ]; do sleep 0.05; done; test -e '"+flag+"'")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "declare env")
	repo.Git("worktree", "add", "-q", "../first")
	repo.Git("worktree", "add", "-q", "../second")
	first, second := repo.In("../first"), repo.In("../second")

	firstRun := startCW(t, first, "run", "env")
	waitFor(t, first.Path("build/started"))

	// The second run starts after the first and ends passed before it.
	createFile(t, flag)
	second.Write("build/go", "")
	if code, output := startCW(t, second, "run", "env").wait(t); code != 0 {
		t.Fatalf("the second run of env: exit status %d, want 0 (passed)\n%s", code, output)
	}

	// The first run ends failed, last.
	removeFile(t, flag)
	first.Write("build/go", "")
	if code, output := firstRun.wait(t); code != 5 {
		t.Fatalf("the first run of env: exit status %d, want 5 (failed)\n%s", code, output)
	}

	assertState(t, first.CW("status"), "env", "failed")
}

// Requirement "Satisfied check", scenario "The clock does not decide the
// order".
func TestTheClockDoesNotDecideTheOrder(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	flag := envFlag(t)
	addEnvCheck(repo, "test", "-e", flag)
	createFile(t, flag)
	mustRun(t, repo, 0, "run", "env")
	removeFile(t, flag)
	mustRun(t, repo, 5, "run", "env", "--force")

	failed := recordWithOutcome(t, repo, "env", "failed")
	repo.EditRecord(failed, func(fields map[string]any) {
		ended, err := time.Parse(time.RFC3339Nano, fields["ended_at"].(string))
		if err != nil {
			t.Fatalf("ended_at: %v", err)
		}
		fields["ended_at"] = ended.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	})

	assertState(t, repo.CW("status"), "env", "failed")
}

// Requirement "Satisfied check", scenario "Content checked earlier is
// recognised again".
func TestContentCheckedEarlierIsRecognisedAgain(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	repo.Write("src/app.txt", "broken\n")
	repo.Write("src/app.txt", "ok\n")

	status := repo.CW("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "satisfied")
}
