package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Declaration file", scenario "The tree's own file governs".
func TestAUserFileDoesNotChangeTheChecks(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	// $HOME/.config/gatecub/gatecub.toml, declaring no checks.
	repo.In(repo.Home).Write(".config/gatecub/gatecub.toml", "version = 1\n")

	result := repo.Gatecub("status")
	for _, id := range []string{"unit", "lint"} {
		if !strings.Contains(result.Output(), id) {
			t.Errorf("gatecub status does not list %q:\n%s", id, result.Output())
		}
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint", "unit"}) {
		t.Errorf("gatecub status --json lists the checks %q, want exactly unit and lint", ids)
	}
}

// Requirement "Declaration file", scenario "Another branch has its own
// declaration".
func TestAnotherBranchUsesItsOwnDeclaration(t *testing.T) {
	t.Parallel()
	repo := gatecubtest.Demo(t)
	repo.Git("checkout", "-q", "-b", "old")
	repo.Write("gatecub.toml", "version = 1\n\n[[check]]\nid = \"lint\"\nrun = [\"sh\", \"lint.sh\"]\n")
	commitAll(repo, "only lint")
	repo.Git("checkout", "-q", "main")
	repo.Git("checkout", "-q", "old")

	result := repo.Gatecub("status")
	if !strings.Contains(result.Output(), "lint") {
		t.Errorf("gatecub status does not list lint:\n%s", result.Output())
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint"}) {
		t.Errorf("gatecub status --json lists the checks %q, want exactly lint", ids)
	}
}
