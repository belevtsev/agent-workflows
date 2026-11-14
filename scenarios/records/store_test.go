package records_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Record store", scenario "The working tree and the home
// directory stay clean".
func TestRunLeavesTheWorkingTreeAndHomeClean(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	home := filepath.Join(t.TempDir(), "home")
	if err := os.Mkdir(home, 0o755); err != nil {
		t.Fatal(err)
	}
	repo.Setenv("HOME", home)
	repo.Setenv("USERPROFILE", home)

	mustRun(t, repo, 0, "run")

	if porcelain := repo.Git("status", "--porcelain"); porcelain != "" {
		t.Errorf("git status --porcelain prints:\n%s", porcelain)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("HOME is not empty: it holds %s", entry.Name())
	}
}

// Requirement "Record store", scenario "A linked worktree sees the same
// records".
func TestLinkedWorktreeSeesTheSameRecords(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	repo.Git("worktree", "add", "-q", "../wt")

	status := repo.In("../wt").CW("status")
	assertState(t, status, "unit", "satisfied")
	assertState(t, status, "lint", "satisfied")
}
