package evidence_test

import (
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// The scenarios in this file come from the second challenge of 14 November
// 2026 (openspec/production-plan.md, Independent challenge).

// withSubmodule is the demo repository with a submodule vendor/lib at its
// first commit, committed. The checks have not run.
func withSubmodule(t *testing.T) *cwtest.Repo {
	t.Helper()
	lib := cwtest.NewRepo(t)
	lib.Write("lib.txt", "lib\n")
	lib.Git("add", "-A")
	lib.Git("commit", "-q", "-m", "lib")
	repo := cwtest.Demo(t)
	repo.Git("-c", "protocol.file.allow=always", "submodule", "add", "-q", lib.Dir, "vendor/lib")
	repo.Git("commit", "-q", "-m", "add vendor/lib")
	return repo
}

// Requirement "Same checked content", scenario "A submodule counts for every
// check with `paths`".
func TestASubmoduleCountsForEveryCheckWithPaths(t *testing.T) {
	t.Parallel()
	repo := withSubmodule(t)
	checkedState(t, repo)

	// The submodule moves to another commit; the working tree is clean in
	// it, so the snapshot records the new commit.
	sub := repo.In("vendor/lib")
	sub.Write("lib.txt", "lib, second\n")
	sub.Git("commit", "-q", "-a", "-m", "second")

	unit := status(t, repo).check(t, "unit")
	if unit.State != "not_run" {
		t.Errorf("after vendor/lib moved, unit, whose paths are src/** and unit.sh, is %q, want not_run", unit.State)
	}
	result := repo.CW("status")
	if !lineWith(result.Stdout, "unit", "not run on this content") {
		t.Errorf("cw status does not say that unit was not run on this content:\n%s", result.Output())
	}
	if result.Code != 5 {
		t.Errorf("cw status exited %d, want 5", result.Code)
	}
}

// Requirement "Working tree that cannot be recorded", scenario "A submodule
// that hides a file".
func TestASubmoduleThatHidesAFileIsRefused(t *testing.T) {
	t.Parallel()
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			t.Parallel()
			repo := withSubmodule(t)
			sub := repo.In("vendor/lib")
			sub.Git("update-index", "--"+mark, "lib.txt")
			sub.Write("lib.txt", "lib, edited where git does not look\n")

			for _, command := range []string{"run", "status"} {
				result := repo.CW(command)
				requireRefused(t, result, "", "vendor/lib/lib.txt", mark, "clear the mark there first")
				if len(repo.RecordFiles("unit"))+len(repo.RecordFiles("lint")) > 0 {
					t.Errorf("cw %s recorded a run", command)
				}
			}
		})
	}
}

// Requirement "Working tree that cannot be recorded", scenario "A submodule
// that is not checked out but holds files".
func TestASubmoduleThatIsNotCheckedOutButHoldsFilesIsRefused(t *testing.T) {
	t.Parallel()
	repo := withSubmodule(t)
	repo.Git("submodule", "deinit", "-q", "-f", "vendor/lib")
	if entries, err := os.ReadDir(repo.Path("vendor/lib")); err != nil || len(entries) != 0 {
		t.Fatalf("after git submodule deinit, vendor/lib holds %v, %v; want an empty folder", entries, err)
	}
	// Not checked out and empty: nothing on disk differs.
	if result := repo.CW("status"); result.Code != 5 {
		t.Fatalf("cw status with vendor/lib not checked out exited %d, want 5:\n%s", result.Code, result.Output())
	}

	repo.Write("vendor/lib/plain.txt", "a file of no tree\n")
	for _, command := range []string{"run", "status"} {
		result := repo.CW(command)
		requireRefused(t, result, "", "vendor/lib", "is a submodule that is not checked out, yet its folder holds files")
	}
	if len(repo.RecordFiles("unit"))+len(repo.RecordFiles("lint")) > 0 {
		t.Errorf("a run was recorded")
	}
}

// Requirement "Outcome of a run", scenario "A run in which a check did not
// pass ends with one line".
func TestARunInWhichACheckDidNotPassEndsWithOneLine(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	repo.Write("src/app.txt", "broken\n")

	result := repo.CW("run")
	if result.Code != 5 {
		t.Errorf("cw run exited %d, want 5", result.Code)
	}
	lines := strings.Split(strings.TrimRight(result.Stdout, "\n"), "\n")
	want := "not verified: 1 of 2 checks did not pass; fix what each log shows, then run `cw run` again"
	if last := lines[len(lines)-1]; last != want {
		t.Errorf("the last line of cw run is %q, want %q:\n%s", last, want, result.Output())
	}
}
