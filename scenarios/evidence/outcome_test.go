package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Outcome of a run", scenario "A failing check".
func TestFailingCheckEndsFailedAndTheNextStillRuns(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)
	repo.Write("src/app.txt", "broken\n")

	result := repo.Kitsu("run")

	unit := status(t, repo).check(t, "unit")
	if unit.Outcome != "failed" || unit.ExitStatus == nil || *unit.ExitStatus != 3 {
		t.Errorf("unit ended %q with exit status %s, want failed with 3", unit.Outcome, exitStatus(unit))
	}
	ran := executed(t, trace)
	if len(ran) == 0 || ran[len(ran)-1] != "lint" {
		t.Errorf("scripts executed: %v, want lint after unit", ran)
	}
	if result.Code != 5 {
		t.Errorf("exit status %d, want 5", result.Code)
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}

// Requirement "Outcome of a run", scenario "A check that writes into the
// source".
func TestCheckThatWritesIntoTheSourceEndsSourceChanged(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	declare(t, repo, "[[check]]\nid = \"cover\"\nrun = [\"sh\", \"-c\", \"echo covered > coverage.out\"]\n")

	result := repo.Kitsu("run", "cover")

	if outcome := lastRecord(t, repo, "cover")["outcome"]; outcome != "source_changed" {
		t.Errorf("cover ended %v, want source_changed", outcome)
	}
	if !strings.Contains(result.Output(), "coverage.out") {
		t.Errorf("the output does not name coverage.out")
	}
	if state := status(t, repo).check(t, "cover").State; state == "satisfied" {
		t.Errorf("cover is satisfied with coverage.out on disk")
	}
	// The run was recorded for the tree before the program wrote the file.
	repo.Remove("coverage.out")
	if state := status(t, repo).check(t, "cover").State; state == "satisfied" {
		t.Errorf("cover is satisfied on the tree it ran on")
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", result.Code, result.Output())
	}
}

// Requirement "Outcome of a run", scenario "A source edit during a run".
func TestSourceEditDuringARunEndsSourceChanged(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	signals := t.TempDir()
	started := filepath.Join(signals, "started")
	proceed := filepath.Join(signals, "proceed")
	repo.Write("lint.sh", "touch '"+started+"'\n"+
		"while [ ! -e '"+proceed+"' ]; do sleep 0.05; done\n"+
		"test ! -e BAD\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "lint.sh waits")

	run := startKitsu(t, repo, nil, "run", "lint")
	if !waitFor(30*time.Second, func() bool { _, err := os.Stat(started); return err == nil }) {
		t.Fatalf("lint.sh did not start\n%s", run.out.String())
	}
	repo.Write("README.md", "# demo, edited during the run\n")
	if err := os.WriteFile(proceed, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !run.wait(30 * time.Second) {
		t.Fatalf("kitsu run lint did not end\n%s", run.out.String())
	}

	if outcome := lastRecord(t, repo, "lint")["outcome"]; outcome != "source_changed" {
		t.Errorf("lint ended %v, want source_changed", outcome)
	}
	if !strings.Contains(run.out.String(), "README.md") {
		t.Errorf("the output does not name README.md")
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", run.code, run.out.String())
	}
}
