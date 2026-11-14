package commitgate_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// deniedMerge builds the first merge scenario: in the checked state branch
// feature adds docs/a.md, main has a later commit of its own, and git merge
// feature runs on main and is denied. It returns how git merge ended.
func deniedMerge(t *testing.T) (*gate, cwtest.Result) {
	t.Helper()
	g := newGate(t)
	g.Setenv("GIT_EDITOR", "true")
	g.checked()
	g.Git("checkout", "-q", "-b", "feature")
	g.Write("docs/a.md", "a\n")
	g.commitNoVerify("add docs/a.md")
	g.Git("checkout", "-q", "main")
	g.Write("README.md", "# demo\n\nmain\n")
	g.commitNoVerify("main's own")
	return g, g.commitDenied("pre-merge-commit", "merge", "feature")
}

// Requirement "Merge rule", scenario "A merge is checked before it is
// recorded".
func TestMergeIsCheckedBeforeItIsRecorded(t *testing.T) {
	t.Parallel()
	g, result := deniedMerge(t)
	assertLineWith(t, result.Stderr, "lint", notRun)
	status := g.Git("status")
	if !strings.Contains(status, "merging") {
		t.Errorf("git status does not report a merge in progress:\n%s", status)
	}
}

// Requirement "Merge rule", scenario "The merge is concluded after a run".
func TestMergeIsConcludedAfterARun(t *testing.T) {
	t.Parallel()
	g, _ := deniedMerge(t)
	g.mustRun(0)
	g.commitRecorded("commit", "--no-edit")
	parents := strings.Fields(g.Git("rev-list", "--parents", "-n", "1", "HEAD"))
	if len(parents) != 3 {
		t.Errorf("HEAD has %d parents, want 2: %v", len(parents)-1, parents)
	}
}
