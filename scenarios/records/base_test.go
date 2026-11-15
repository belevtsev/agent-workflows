package records_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// freshClone clones the demo repository, so that origin/main exists in a
// repository that holds no records.
func freshClone(t *testing.T) *gatecubtest.Repo {
	t.Helper()
	repo := gatecubtest.Demo(t)
	dir := filepath.Join(t.TempDir(), "clone")
	repo.Git("clone", "-q", repo.Dir, dir)
	clone := repo.In(dir)
	if files := append(clone.RecordFiles("unit"), clone.RecordFiles("lint")...); len(files) != 0 {
		t.Fatalf("the fresh clone holds records: %v", files)
	}
	return clone
}

// Requirement "Base revision for continuous integration", scenario "A
// documentation change in a pull request".
func TestDocumentationChangeRunsOnlyTheChecksItTouches(t *testing.T) {
	t.Parallel()
	clone := freshClone(t)
	clone.Write("README.md", "# demo\n\ndocumented\n")
	clone.Git("commit", "-q", "-am", "document")

	result := mustRun(t, clone, 0, "run", "--base", "origin/main")

	// unit.sh and lint.sh of the demo repository print these lines.
	if n := strings.Count(result.Output(), "unit.sh runs"); n != 0 {
		t.Errorf("unit.sh was executed %d times, want 0:\n%s", n, result.Output())
	}
	if n := strings.Count(result.Output(), "lint.sh runs"); n != 1 {
		t.Errorf("lint.sh was executed %d times, want 1:\n%s", n, result.Output())
	}
}

// Requirement "Base revision for continuous integration", scenario "A check
// new in the change always runs".
func TestCheckNewInTheChangeAlwaysRuns(t *testing.T) {
	t.Parallel()
	clone := freshClone(t)
	executed := filepath.Join(t.TempDir(), "build-executed")
	clone.Write("gatecub.toml", gatecubtest.DemoDeclaration+"\n[[check]]\nid = \"build\"\nrun = [\"sh\", \"-c\", ': >\"$0\"', '"+filepath.ToSlash(executed)+"']\npaths = [\"src/**\"]\n")
	clone.Git("commit", "-q", "-am", "declare build")

	result := clone.Gatecub("run", "--base", "origin/main")

	if _, err := os.Stat(executed); err != nil {
		t.Errorf("the program of build was not executed (gatecub run exit status %d):\n%s", result.Code, result.Output())
	}
}
