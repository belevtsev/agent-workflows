package reporting_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Requirement "Gate state", scenario "Status says first that nothing judges
// yet". From the second challenge of 14 November 2026
// (openspec/production-plan.md, Independent challenge), finding U6.
func TestStatusSaysFirstThatNothingJudgesYet(t *testing.T) {
	t.Parallel()
	repo := gated(t)
	clone := repo.In(filepath.Join(t.TempDir(), "clone"))
	repo.Git("clone", "-q", repo.Dir, clone.Dir)

	result := clone.Gatecub("status")
	if result.Code != 5 {
		t.Errorf("gatecub status in the clone exited %d, want 5", result.Code)
	}
	first := strings.SplitN(result.Stdout, "\n", 2)[0]
	if want := "not verified, but no hook of gatecub judges a commit in this clone yet"; first != want {
		t.Errorf("the first line of gatecub status is %q, want %q:\n%s", first, want, result.Output())
	}
	if strings.Contains(result.Output(), "a commit of this content would be denied") {
		t.Errorf("gatecub status says that a commit would be denied, although no hook judges it:\n%s", result.Output())
	}

	// Where the hooks are installed, the same content would be denied.
	result = repo.Gatecub("status")
	if first := strings.SplitN(result.Stdout, "\n", 2)[0]; first != "not verified: a commit of this content would be denied" {
		t.Errorf("with the hooks installed, the first line of gatecub status is %q:\n%s", first, result.Output())
	}
}
