package pushgate_test

import (
	"testing"
)

// Requirement "Updates that add nothing", scenario "A branch is deleted on
// the remote".
func TestABranchIsDeletedOnTheRemote(t *testing.T) {
	t.Parallel()
	repo, origin := withOrigin(t)
	repo.Git("push", "-q", "origin", "main:refs/heads/old-branch")
	mustGatecub(t, repo, 0, "init")
	repo.Write("src/app.txt", "broken\n")

	push := repo.TryGit("push", "origin", "--delete", "old-branch")
	assertSent(t, push, origin, map[string]string{"refs/heads/old-branch": ""})
}

// Requirement "Updates that add nothing", scenario "A tag on a commit the
// remote already has".
func TestATagOnACommitTheRemoteAlreadyHas(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	first := refOn(repo, "HEAD")
	repo.Git("tag", "v1", first)
	if len(repo.RecordFiles("unit")) > 0 || len(repo.RecordFiles("lint")) > 0 {
		t.Fatal("the fixture already holds records")
	}

	push := repo.TryGit("push", "origin", "v1")
	assertSent(t, push, origin, map[string]string{"refs/tags/v1": first})
}

// Requirement "Updates that add nothing", scenario "A branch is moved back".
func TestABranchIsMovedBack(t *testing.T) {
	t.Parallel()
	repo, origin := withOrigin(t)
	first := refOn(repo, "HEAD")
	repo.Write("docs/a.md", "a\n")
	commitAround(repo, "a")
	repo.Write("docs/b.md", "b\n")
	commitAround(repo, "b")
	repo.Git("push", "-q", "origin", "main")
	mustGatecub(t, repo, 0, "init")
	if len(repo.RecordFiles("unit")) > 0 || len(repo.RecordFiles("lint")) > 0 {
		t.Fatal("the fixture already holds records")
	}

	push := repo.TryGit("push", "--force", "origin", "HEAD~2:main")
	assertSent(t, push, origin, map[string]string{"refs/heads/main": first})
}

// Requirement "Updates that add nothing", scenario "A new branch at a commit
// another branch has".
func TestANewBranchAtACommitAnotherBranchHas(t *testing.T) {
	t.Parallel()
	repo, origin := gated(t)
	if len(repo.RecordFiles("unit")) > 0 || len(repo.RecordFiles("lint")) > 0 {
		t.Fatal("the fixture already holds records")
	}

	push := repo.TryGit("push", "origin", "origin/main:refs/heads/release")
	assertDenied(t, push, origin, map[string]string{"refs/heads/release": ""})
}
