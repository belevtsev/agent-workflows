package evidence_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// requireRefused checks that kitsu exited 1, that its output holds every text,
// and that no check was executed.
func requireRefused(t *testing.T, result kitsutest.Result, trace string, texts ...string) {
	t.Helper()
	if result.Code != 1 {
		t.Errorf("exit status %d, want 1", result.Code)
	}
	for _, text := range texts {
		if !strings.Contains(result.Output(), text) {
			t.Errorf("the output does not name %q", text)
		}
	}
	if trace != "" {
		if ran := executed(t, trace); len(ran) != 0 {
			t.Errorf("checks executed: %v, want none", ran)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}

// Requirement "Working tree that cannot be recorded", scenario "A local
// override that git is told to overlook".
func TestSkipWorktreeFileThatDiffersIsRefused(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)
	repo.Git("update-index", "--skip-worktree", "README.md")
	repo.Write("README.md", "# demo, edited locally\n")

	result := repo.Kitsu("run")

	requireRefused(t, result, trace, "README.md", "skip-worktree")
}

// Requirement "Working tree that cannot be recorded", scenario "A sparse
// checkout".
func TestSparseCheckoutIsRefused(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Git("sparse-checkout", "init", "--cone")
	if repo.Exists("src/app.txt") {
		t.Fatal("the sparse checkout left src/app.txt on disk")
	}

	result := repo.Kitsu("status")

	if result.Code != 1 {
		t.Errorf("exit status %d, want 1", result.Code)
	}
	if !strings.Contains(strings.ToLower(result.Output()), "sparse") {
		t.Errorf("the output does not say that the sparse checkout keeps tracked files off the disk")
	}
	if t.Failed() {
		t.Logf("output:\n%s", result.Output())
	}
}

// Requirement "Working tree that cannot be recorded", scenario "A nested
// repository without a commit".
func TestNestedRepositoryWithoutACommitIsRefused(t *testing.T) {
	t.Parallel()
	repo, trace := tracedDemo(t)
	repo.Git("init", "-q", "sub")

	result := repo.Kitsu("run")

	// git add -A says: error: 'sub/' does not have a commit checked out
	requireRefused(t, result, trace, "sub/", "does not have a commit checked out")
}

// Requirement "Working tree that cannot be recorded", scenario "A submodule
// with uncommitted changes".
func TestSubmoduleWithUncommittedChangesIsRefused(t *testing.T) {
	t.Parallel()
	lib := kitsutest.NewRepo(t)
	lib.Write("lib.txt", "lib\n")
	lib.Git("add", "-A")
	lib.Git("commit", "-q", "-m", "lib")
	repo, trace := tracedDemo(t)
	repo.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "vendor/lib")
	repo.Git("commit", "-q", "-m", "add vendor/lib")
	repo.Write("vendor/lib/lib.txt", "lib, edited\n")

	result := repo.Kitsu("run")

	requireRefused(t, result, trace, "vendor/lib")
	if !strings.Contains(strings.ToLower(result.Output()), "uncommitted") {
		t.Errorf("the output does not say that vendor/lib holds uncommitted changes:\n%s", result.Output())
	}
}
