package declaration_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Declaration file", scenario "The tree's own file governs".
func TestAUserFileDoesNotChangeTheChecks(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	// $HOME/.config/cw/cw.toml, declaring no checks.
	repo.In(repo.Home).Write(".config/cw/cw.toml", "version = 1\n")

	result := repo.CW("status")
	for _, id := range []string{"unit", "lint"} {
		if !strings.Contains(result.Output(), id) {
			t.Errorf("cw status does not list %q:\n%s", id, result.Output())
		}
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint", "unit"}) {
		t.Errorf("cw status --json lists the checks %q, want exactly unit and lint", ids)
	}
}

// Requirement "Declaration file", scenario "Another branch has its own
// declaration".
func TestAnotherBranchUsesItsOwnDeclaration(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Git("checkout", "-q", "-b", "old")
	repo.Write("cw.toml", "version = 1\n\n[[check]]\nid = \"lint\"\nrun = [\"sh\", \"lint.sh\"]\n")
	commitAll(repo, "only lint")
	repo.Git("checkout", "-q", "main")
	repo.Git("checkout", "-q", "old")

	result := repo.CW("status")
	if !strings.Contains(result.Output(), "lint") {
		t.Errorf("cw status does not list lint:\n%s", result.Output())
	}
	if ids := checkIDs(t, repo); !slices.Equal(ids, []string{"lint"}) {
		t.Errorf("cw status --json lists the checks %q, want exactly lint", ids)
	}
}
