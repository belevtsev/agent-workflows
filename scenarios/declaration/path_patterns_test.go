package declaration_test

import (
	"fmt"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// No command prints whether a pattern matches a path. These tests declare a
// check `p` whose paths is the pattern, let it pass, change one path, and
// read from kitsu status whether `p` is still satisfied: a change to a matched
// path makes it unsatisfied, a change to any other path does not.

const satisfied = "satisfied"

// patternRepo is a repository with the given files and one check `p` whose
// paths is pattern, after a run in which `p` passed.
func patternRepo(t *testing.T, pattern string, files ...string) *kitsutest.Repo {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	repo.Write("kitsu.toml", fmt.Sprintf("version = 1\n\n[[check]]\nid = \"p\"\nrun = [\"git\", \"--version\"]\npaths = [%q]\n", pattern))
	for _, file := range files {
		repo.Write(file, "first\n")
	}
	commitAll(repo, "first")
	if result := repo.Kitsu("run"); result.Code != 0 {
		t.Fatalf("kitsu run exited %d:\n%s", result.Code, result.Output())
	}
	if state := stateOf(t, repo, "p"); state != satisfied {
		t.Fatalf("after kitsu run, p is %q, want satisfied", state)
	}
	return repo
}

// matches changes one path, says whether that left `p` unsatisfied, and then
// puts the path back as it was.
func matches(t *testing.T, repo *kitsutest.Repo, path string) bool {
	t.Helper()
	repo.Write(path, "second\n")
	matched := stateOf(t, repo, "p") != satisfied
	repo.Write(path, "first\n")
	if state := stateOf(t, repo, "p"); state != satisfied {
		t.Fatalf("after %s is put back, p is %q, want satisfied", path, state)
	}
	return matched
}

func expectMatches(t *testing.T, pattern string, files, matching, notMatching []string) {
	t.Helper()
	repo := patternRepo(t, pattern, files...)
	for _, path := range matching {
		if !matches(t, repo, path) {
			t.Errorf("%q does not match %s: p is still satisfied after it changed", pattern, path)
		}
	}
	for _, path := range notMatching {
		if matches(t, repo, path) {
			t.Errorf("%q matches %s: p is no longer satisfied after it changed", pattern, path)
		}
	}
}

// Requirement "Path patterns", scenario "Double star crosses directories".
func TestDoubleStarCrossesDirectories(t *testing.T) {
	t.Parallel()
	expectMatches(t, "**/*.go",
		[]string{"main.go", "a/b/c.go", "main.go.bak"},
		[]string{"main.go", "a/b/c.go"},
		[]string{"main.go.bak"})
}

// Requirement "Path patterns", scenario "A directory prefix matches
// everything below it".
func TestDirectoryPrefixMatchesEverythingBelowIt(t *testing.T) {
	t.Parallel()
	expectMatches(t, "docs/**",
		[]string{"docs/a.md", "docs/x/y.md", "docs.md"},
		[]string{"docs/a.md", "docs/x/y.md"},
		[]string{"docs.md"})
}

// Requirement "Path patterns", scenario "A plain name matches at the root
// only".
func TestPlainNameMatchesAtTheRootOnly(t *testing.T) {
	t.Parallel()
	expectMatches(t, "go.mod",
		[]string{"go.mod", "tools/go.mod"},
		[]string{"go.mod"},
		[]string{"tools/go.mod"})
}

// Requirement "Path patterns", scenario "A submodule is one entry".
func TestNewSubmoduleCommitIsAChangeInsideItsPattern(t *testing.T) {
	t.Parallel()
	lib := kitsutest.NewRepo(t)
	lib.Write("lib.txt", "first\n")
	commitAll(lib, "first")

	repo := kitsutest.NewRepo(t)
	repo.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "vendor/lib")
	repo.Write("kitsu.toml", "version = 1\n\n[[check]]\nid = \"p\"\nrun = [\"git\", \"--version\"]\npaths = [\"vendor/lib/**\"]\n")
	commitAll(repo, "first")
	if result := repo.Kitsu("run"); result.Code != 0 {
		t.Fatalf("kitsu run exited %d:\n%s", result.Code, result.Output())
	}
	if state := stateOf(t, repo, "p"); state != satisfied {
		t.Fatalf("after kitsu run, p is %q, want satisfied", state)
	}

	// Move the submodule to a new commit; commit nothing else.
	sub := repo.In("vendor/lib")
	sub.Write("lib.txt", "second\n")
	sub.Git("commit", "-q", "-a", "-m", "second")
	repo.Git("add", "vendor/lib")
	repo.Git("commit", "-q", "-m", "move vendor/lib")

	if state := stateOf(t, repo, "p"); state == satisfied {
		t.Errorf("after the submodule moved to a new commit, p is still satisfied: vendor/lib/** does not match the entry vendor/lib")
	}
}
