package roles_test

import (
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// assertLists checks that the output names each of the paths and none of
// the others.
func assertLists(t *testing.T, result kitsutest.Result, listed, notListed []string) {
	t.Helper()
	out := result.Output()
	for _, path := range listed {
		if !namesWord(out, path) {
			t.Errorf("the output does not list %s:\n%s", path, out)
		}
	}
	for _, path := range notListed {
		if namesWord(out, path) {
			t.Errorf("the output lists %s, which is within the limit:\n%s", path, out)
		}
	}
}

// Requirement "Write limits", scenario "The developer role changes a test".
func TestTheDeveloperRoleChangesATest(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Write("src/app.txt", "ok\nmore\n")
	repo.Write("tests/app_test.txt", "app test\nchanged\n")
	result := repo.Kitsu("roles", "check", "developer", "--from", t0)
	if result.Code != 5 {
		t.Errorf("kitsu roles check developer exited %d, want 5:\n%s", result.Code, result.Output())
	}
	assertLists(t, result, []string{"tests/app_test.txt"}, []string{"src/app.txt"})
}

// Requirement "Write limits", scenario "The developer role adds a test".
func TestTheDeveloperRoleAddsATest(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Write("src/app.txt", "ok\nmore\n")
	repo.Write("tests/extra_test.txt", "extra test\n")
	result := repo.Kitsu("roles", "check", "developer", "--from", t0)
	if result.Code != 0 {
		t.Errorf("kitsu roles check developer exited %d, want 0:\n%s", result.Code, result.Output())
	}
}

func TestDeveloperCannotChangeAdviserPolicy(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	before := tree(t, repo)
	repo.Write("kitsu-advisers.toml", "version = 1\n[access]\ndefault = []\n")
	result := repo.Kitsu("roles", "check", "developer", "--from", before)
	if result.Code != 5 || !namesWord(result.Output(), "kitsu-advisers.toml") {
		t.Fatalf("changing adviser policy must exceed developer scope:\n%s", result.Output())
	}
}

// Requirement "Write limits", scenario "The quality engineer changes
// production code".
func TestTheQualityEngineerChangesProductionCode(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Write("tests/app_test.txt", "app test\nchanged\n")
	repo.Write("src/app.txt", "ok\nmore\n")
	result := repo.Kitsu("roles", "check", "quality-engineer", "--from", t0, "--task", "add-greeting")
	if result.Code != 5 {
		t.Errorf("kitsu roles check quality-engineer exited %d, want 5:\n%s", result.Code, result.Output())
	}
	assertLists(t, result, []string{"src/app.txt"}, []string{"tests/app_test.txt"})
}

// Requirement "Write limits", scenario "The quality engineer removes an
// existing test".
func TestTheQualityEngineerRemovesAnExistingTest(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Remove("tests/app_test.txt")
	result := repo.Kitsu("roles", "check", "quality-engineer", "--from", t0, "--task", "add-greeting")
	if result.Code != 0 {
		t.Errorf("kitsu roles check quality-engineer exited %d, want 0:\n%s", result.Code, result.Output())
	}
}

// Requirement "Write limits", scenario "A role that writes nothing wrote a
// file".
func TestARoleThatWritesNothingWroteAFile(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Write("notes.txt", "a note\n")
	result := repo.Kitsu("roles", "check", "reviewer", "--from", t0)
	if result.Code != 5 {
		t.Errorf("kitsu roles check reviewer exited %d, want 5:\n%s", result.Code, result.Output())
	}
	assertLists(t, result, []string{"notes.txt"}, nil)
}

// Requirement "Write limits", scenario "The spec writer stays in its folder".
func TestTheSpecWriterStaysInItsFolder(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Write("openspec/changes/add-greeting/specs/greeting/spec.md", "# greeting\n")
	result := repo.Kitsu("roles", "check", "spec-writer", "--from", t0, "--task", "add-greeting")
	if result.Code != 0 {
		t.Errorf("kitsu roles check spec-writer exited %d, want 0:\n%s", result.Code, result.Output())
	}
	// One path is "1 path differs", not "1 paths differ".
	if want := "`spec-writer` stayed within its limit: 1 path differs from " + t0 + "\n"; result.Stdout != want {
		t.Errorf("kitsu roles check spec-writer printed %q, want %q", result.Stdout, want)
	}
}

// Requirement "Write limits", scenario "Test paths are not declared".
func TestTestPathsAreNotDeclared(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	t0 := tree(t, repo)
	repo.Remove("kitsu-roles.toml")
	repo.Write("src/app.txt", "ok\nmore\n")
	result := repo.Kitsu("roles", "check", "developer", "--from", t0)
	if result.Code != 1 {
		t.Errorf("kitsu roles check developer exited %d, want 1:\n%s", result.Code, result.Output())
	}
	out := result.Output()
	if !lineWith(out, "kitsu-roles.toml", "[paths]") {
		t.Errorf("the output does not say that kitsu-roles.toml must name the test paths under [paths]:\n%s", out)
	}
}

// Requirement "Write limits", scenario "A clone in which nothing is gated".
func TestACloneInWhichNothingIsGated(t *testing.T) {
	t.Parallel()
	repo := demo(t) // kitsu init has not run in it
	t0 := tree(t, repo)
	result := repo.Kitsu("roles", "check", "reviewer", "--from", t0)
	if result.Code != 0 {
		t.Errorf("kitsu roles check reviewer exited %d, want 0:\n%s", result.Code, result.Output())
	}
	out := result.Output()
	// The wording is not fixed; the warning is one line about the hooks
	// that names kitsu init.
	if !lineWith(out, "hook", "kitsu init") {
		t.Errorf("the output does not warn that the hooks are not installed, naming kitsu init:\n%s", out)
	}
}
