package pushgate_test

import (
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// linkedWorktree adds the linked worktree ../wt on a branch of its own.
func linkedWorktree(repo *cwtest.Repo) *cwtest.Repo {
	repo.Git("worktree", "add", "-q", "-b", "wt", "../wt")
	return repo.In("../wt")
}

// Requirement "Linked worktrees", scenario "A failing main checkout does not
// block a worktree".
func TestAFailingMainCheckoutDoesNotBlockAWorktree(t *testing.T) {
	t.Parallel()
	repo, _ := gated(t)
	wt := linkedWorktree(repo)
	repo.Write("src/app.txt", "broken\n")
	mustCW(t, repo, 5, "run")

	wt.Write("README.md", "# demo\nmore\n")
	mustCW(t, wt, 0, "run")
	before := refOn(wt, "HEAD")
	commit := wt.TryGit("commit", "-a", "-m", "docs")
	if commit.Code != 0 {
		t.Errorf("git commit in ../wt exited %d, want 0\n%s", commit.Code, commit.Output())
	}
	if after := refOn(wt, "HEAD"); after == before {
		t.Errorf("HEAD of ../wt did not move: the commit was not recorded")
	}
}

// Requirement "Linked worktrees", scenario "A worktree is gated too".
func TestAWorktreeIsGatedToo(t *testing.T) {
	t.Parallel()
	repo, _ := gated(t)
	wt := linkedWorktree(repo)
	wt.Write("src/app.txt", "ok\nmore\n")
	before := refOn(wt, "HEAD")

	commit := wt.TryGit("commit", "-a", "-m", "change")
	if commit.Code == 0 {
		t.Errorf("git commit in ../wt exited 0, want a denial\n%s", commit.Output())
	}
	if after := refOn(wt, "HEAD"); after != before {
		t.Errorf("HEAD of ../wt moved from %s to %s", before, after)
	}
	if commit.Stderr == "" {
		t.Errorf("the denial printed nothing on standard error")
	}
}
