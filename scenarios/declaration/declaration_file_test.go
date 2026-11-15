package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Declaration file", scenario "The tree's own file governs".
func TestAUserFileDoesNotChangeTheChecks(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	// $HOME/.config/kitsu/kitsu.toml, declaring no checks.
	repo.In(repo.Home).Write(".config/kitsu/kitsu.toml", "version = 1\n")

	result := repo.Kitsu("status")
	for _, id := range []string{"unit", "lint"} {
		if !strings.Contains(result.Output(), id) {
			t.Errorf("kitsu status does not list %q:\n%s", id, result.Output())
		}
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint", "unit"}) {
		t.Errorf("kitsu status --json lists the checks %q, want exactly unit and lint", ids)
	}
}

// Requirement "Declaration file", scenario "Another branch has its own
// declaration".
func TestAnotherBranchUsesItsOwnDeclaration(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	repo.Git("checkout", "-q", "-b", "old")
	repo.Write("kitsu.toml", "version = 1\n\n[[check]]\nid = \"lint\"\nrun = [\"sh\", \"lint.sh\"]\n")
	commitAll(repo, "only lint")
	repo.Git("checkout", "-q", "main")
	repo.Git("checkout", "-q", "old")

	result := repo.Kitsu("status")
	if !strings.Contains(result.Output(), "lint") {
		t.Errorf("kitsu status does not list lint:\n%s", result.Output())
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint"}) {
		t.Errorf("kitsu status --json lists the checks %q, want exactly lint", ids)
	}
}
