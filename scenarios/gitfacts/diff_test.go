package gitfacts_test

import (
	"os"
	"strings"
	"testing"
)

// Sources, second script: "`git diff-tree -r --name-only` lists a moved
// file at both paths; `git diff --name-only` lists only the new one."
func TestAMovedFileIsListedAtBothPaths(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	if err := os.MkdirAll(repo.Path("docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo.Git("mv", "f.txt", "docs/f.txt")
	repo.Git("commit", "-q", "--no-verify", "-m", "mv")

	got := strings.Fields(repo.Git("diff-tree", "-r", "--name-only", treeOf(repo, "HEAD~1"), treeOf(repo, "HEAD")))
	if strings.Join(got, " ") != "docs/f.txt f.txt" {
		t.Errorf("git diff-tree -r --name-only lists %q, want both paths", got)
	}
	got = strings.Fields(repo.Git("diff", "--name-only", "HEAD~1", "HEAD"))
	if strings.Join(got, " ") != "docs/f.txt" {
		t.Errorf("git diff --name-only lists %q, want only the new path", got)
	}
}

// Sources, second script: "In a new repository with an untracked
// `kitsu.toml`, `git commit` records nothing."
func TestACommitInANewRepositoryWithAnUntrackedFileRecordsNothing(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	repo.Write("kitsu.toml", "version = 1\n")
	if result := repo.TryGit("commit", "-q", "-m", "first"); result.Code == 0 {
		t.Errorf("git commit exited 0 with nothing staged\n%s", result.Output())
	}
	if result := repo.TryGit("rev-parse", "--verify", "-q", "HEAD"); result.Code == 0 {
		t.Errorf("HEAD exists after the commit: %s", result.Stdout)
	}
}
