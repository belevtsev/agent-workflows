package platform_test

import (
	"testing"
)

// Requirement "Supported systems", scenario "A gated commit on each system".
func TestGatedCommitOnEachSystem(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	before := head(repo)
	repo.Write("README.md", editedReadme)

	run := repo.Gatecub("run")
	repo.Git("add", "-A")
	commit := repo.TryGit("commit", "-m", "docs")

	if run.Code != 0 {
		t.Errorf("gatecub run exit status %d, want 0\n%s", run.Code, run.Output())
	}
	if commit.Code != 0 {
		t.Errorf("git commit exit status %d, want 0\n%s", commit.Code, commit.Output())
	}
	if after := head(repo); after == before {
		t.Fatalf("HEAD did not move: the commit was not recorded")
	}
	if subject := repo.Git("log", "-1", "--format=%s"); subject != "docs" {
		t.Errorf("the last commit is %q, want docs", subject)
	}
	if parent := repo.Git("rev-parse", "HEAD~1"); parent != before {
		t.Errorf("the new commit's parent is %s, want %s", parent, before)
	}
	if readme := repo.Git("show", "HEAD:README.md"); readme+"\n" != editedReadme {
		t.Errorf("the commit holds README.md %q, want the edit", readme)
	}
}

// Requirement "Supported systems", scenario "A denied commit on each system".
func TestDeniedCommitOnEachSystem(t *testing.T) {
	t.Parallel()
	repo := portable(t, "")
	before := head(repo)
	repo.Write("README.md", editedReadme)
	trace := traced(t, repo)

	commit := repo.TryGit("commit", "-a", "-m", "docs")

	assertDenied(t, repo, before, commit, trace)
	if !lineWith(commit.Stderr, "git", "not run on this content") {
		t.Errorf("standard error has no line that names git as not run on this content")
	}
	if t.Failed() {
		t.Logf("git commit (exit status %d):\n%s", commit.Code, commit.Output())
	}
}
