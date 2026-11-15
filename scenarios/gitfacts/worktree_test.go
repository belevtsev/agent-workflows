package gitfacts_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// addWorktree adds a linked worktree next to the repository, on a branch
// of its own.
func addWorktree(repo *gatecubtest.Repo, branch string) *gatecubtest.Repo {
	dir := filepath.Join(filepath.Dir(repo.Dir), branch)
	repo.Git("worktree", "add", "-q", "-b", branch, dir)
	return repo.In(dir)
}

// Sources, first script: "Hooks in the common git directory run in a
// linked worktree."
func TestHooksOfTheCommonDirectoryRunInALinkedWorktree(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	log := newHookLog(t)
	logTreeHook(t, repo, "pre-commit", log)
	wt := addWorktree(repo, "wt")

	// SameFile, not a string compare: on Windows the two paths may differ
	// in case or in short names.
	got, want := gitPath(wt, "hooks"), gitPath(repo, "hooks")
	gotInfo, err := os.Stat(got)
	if err != nil {
		t.Fatal(err)
	}
	wantInfo, err := os.Stat(want)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(gotInfo, wantInfo) {
		t.Errorf("the worktree's hooks are in %s, want the common %s", got, want)
	}
	wt.Write("w.txt", "w\n")
	wt.Git("add", "w.txt")
	wt.Git("commit", "-q", "-m", "w")
	if got := log.runs(); got != 1 {
		t.Errorf("pre-commit ran %d time(s) for a commit in the linked worktree, want 1", got)
	}
}

// Sources, second script: "With a relative `core.hooksPath` and untracked
// hook files, a commit in a linked worktree runs no hook." Known limits
// says the same.
func TestARelativeHooksPathLeavesALinkedWorktreeWithoutHooks(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	writeHook(t, repo.Path(".githooks/pre-commit"), "echo 'denied (test)' >&2\nexit 1\n")
	repo.Git("config", "core.hooksPath", ".githooks")

	repo.Write("f.txt", "b\n")
	if result := repo.TryGit("commit", "-q", "-a", "-m", "x"); result.Code == 0 {
		t.Fatalf("the commit in the main checkout passed a hook that exits 1\n%s", result.Output())
	}
	repo.Git("checkout", "-q", "--", "f.txt")

	wt := addWorktree(repo, "wt")
	if wt.Exists(".githooks") {
		t.Fatalf(".githooks exists in the linked worktree although it is untracked")
	}
	wt.Write("f.txt", "c\n")
	if result := wt.TryGit("commit", "-q", "-a", "-m", "x"); result.Code != 0 {
		t.Errorf("the commit in the linked worktree exited %d, want 0: no hook should run\n%s",
			result.Code, result.Output())
	}
}
