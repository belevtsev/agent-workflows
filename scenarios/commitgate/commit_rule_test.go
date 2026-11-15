package commitgate_test

import (
	"os"
	"strings"
	"testing"
)

// Requirement "Commit rule", scenario "Checked content is committed".
func TestCheckedContentIsCommitted(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "ok") // edited, and it still holds ok
	g.mustRun(0)
	g.Git("add", "-A")
	g.commitRecorded("commit", "-m", "change")
}

// Requirement "Commit rule", scenario "A failed check denies".
func TestFailedCheckDenies(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "broken\n")
	g.mustRun(5)
	result := g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	assertLineWith(t, result.Stderr, "unit", "failed")
}

// Requirement "Commit rule", scenario "Unchecked content denies".
func TestUncheckedContentDenies(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.checked()
	g.Write("src/app.txt", "ok")
	result := g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	assertLineWith(t, result.Stderr, "unit", notRun)
	assertLineWith(t, result.Stderr, "lint", notRun)
}

// Requirement "Commit rule", scenario "A new file left out of the commit
// denies".
func TestNewFileLeftOutOfCommitDenies(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "ok")
	g.Write("src/new.txt", "new\n")
	g.mustRun(0)
	g.Git("add", "src/app.txt")
	g.commitDenied("pre-commit", "commit", "-m", "change")
}

// Requirement "Commit rule", scenario "A commit of one path is judged by its
// own tree".
func TestCommitOfOnePathIsJudgedByItsOwnTree(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.checked()
	g.Write("README.md", "# demo\n\nmore\n")
	g.Write("src/app.txt", "ok")
	g.mustRun(0)
	result := g.commitDenied("pre-commit", "commit", "-m", "docs", "README.md")
	assertLineWith(t, result.Stderr, "lint", notRun)
	if namesWord(result.Stderr, "unit") {
		t.Errorf("the reason names unit:\n%s", result.Stderr)
	}
}

// Requirement "Commit rule", scenario "Content too large for the old
// fingerprint is still judged".
func TestContentTooLargeForOldFingerprintIsStillJudged(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("writes a 68 MiB file")
	}
	g := newGate(t)
	g.checked()
	g.Write("src/app.txt", "ok")
	g.Git("add", "src/app.txt")
	big, err := os.Create(g.Path("big.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := big.Truncate(68 << 20); err != nil {
		t.Fatal(err)
	}
	if err := big.Close(); err != nil {
		t.Fatal(err)
	}
	g.commitDenied("pre-commit", "commit", "-m", "change")
}

// Requirement "Commit rule", scenario "The next change needs only a new run".
func TestNextChangeNeedsOnlyANewRun(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "ok")
	g.mustRun(0)
	g.commitRecorded("commit", "-a", "-m", "change")

	g.Write("src/app.txt", "ok\n\n")
	g.mustRun(0)
	g.commitRecorded("commit", "-a", "-m", "next")
}

// Requirement "Commit rule", scenario "A documentation change reruns only
// what depends on it".
func TestDocumentationChangeRerunsOnlyWhatDependsOnIt(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.checked()
	g.Write("README.md", "# demo\n\nmore\n")
	result := g.Gatecub("run")
	if !strings.Contains(result.Output(), "lint.sh runs") {
		t.Errorf("gatecub run did not execute lint.sh:\n%s", result.Output())
	}
	if strings.Contains(result.Output(), "unit.sh runs") {
		t.Errorf("gatecub run executed unit.sh:\n%s", result.Output())
	}
	g.commitRecorded("commit", "-a", "-m", "docs")
}

// Requirement "Commit rule", scenario "A continued cherry-pick is judged".
func TestContinuedCherryPickIsJudged(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Setenv("GIT_EDITOR", "true")
	g.Git("checkout", "-q", "-b", "feature")
	g.Write("src/app.txt", "feature\n")
	g.Write("docs/f.md", "feature\n")
	g.commitNoVerify("feature")
	g.Git("checkout", "-q", "main")
	g.Write("src/app.txt", "main\n")
	g.commitNoVerify("main")

	if result := g.TryGit("cherry-pick", "feature"); result.Code == 0 {
		t.Fatalf("git cherry-pick feature did not stop on a conflict:\n%s", result.Output())
	}
	g.Write("src/app.txt", "ok\n")
	g.Git("add", "src/app.txt")

	g.commitDenied("pre-commit", "cherry-pick", "--continue")
	if g.TryGit("rev-parse", "-q", "--verify", "CHERRY_PICK_HEAD").Code != 0 {
		t.Errorf("the cherry-pick is no longer in progress after the denial")
	}

	g.mustRun(0)
	g.commitRecorded("cherry-pick", "--continue")
	if g.TryGit("rev-parse", "-q", "--verify", "CHERRY_PICK_HEAD").Code == 0 {
		t.Errorf("the cherry-pick is still in progress after it was concluded")
	}
}

// Requirement "Paths that differ", scenario "The left-out file is named".
func TestLeftOutFileIsNamed(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	g.Write("src/app.txt", "ok")
	g.Write("src/new.txt", "new\n")
	g.mustRun(0)
	g.Git("add", "src/app.txt")
	result := g.commitDenied("pre-commit", "commit", "-m", "change")
	if !strings.Contains(result.Stderr, "src/new.txt") {
		t.Errorf("the reason does not list src/new.txt:\n%s", result.Stderr)
	}
	assertLineWith(t, result.Stderr, "working tree as a whole is verified")
}
