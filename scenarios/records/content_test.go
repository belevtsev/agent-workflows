package records_test

import (
	"os"
	"testing"
)

// Requirement "Same checked content", scenario "A file moved out of `paths`".
func TestMovingAFileOutOfPathsNeedsANewRun(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	if err := os.MkdirAll(repo.Path("docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo.Git("mv", "src/app.txt", "docs/app.txt")

	assertState(t, repo.CW("status"), "unit", "not run")
}

// Requirement "Same checked content", scenario "A file made executable".
func TestMakingAFileExecutableNeedsANewRun(t *testing.T) {
	t.Parallel()
	repo := checkedState(t)
	if repo.Git("config", "--get", "core.filemode") != "true" {
		t.Skip("git does not record file modes in this repository")
	}
	if err := os.Chmod(repo.Path("unit.sh"), 0o755); err != nil {
		t.Fatal(err)
	}

	assertState(t, repo.CW("status"), "unit", "not run")
}
