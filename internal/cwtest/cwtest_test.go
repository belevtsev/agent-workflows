package cwtest_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

func TestDemoRepositoryRunsItsChecks(t *testing.T) {
	t.Parallel()
	repo := cwtest.Demo(t)
	if got := repo.CW("version"); got.Code != 0 || !strings.HasPrefix(got.Stdout, "cw ") {
		t.Fatalf("cw version: exit %d, output %q", got.Code, got.Output())
	}
	if got := repo.CW("run"); got.Code != 0 {
		t.Fatalf("cw run in the demo repository: exit %d\n%s", got.Code, got.Output())
	}
	if files := repo.RecordFiles("unit"); len(files) != 1 {
		t.Fatalf("records of unit: %v, want one", files)
	}
	if got := repo.CW("status"); got.Code != 0 {
		t.Fatalf("cw status after a passing run: exit %d\n%s", got.Code, got.Output())
	}
}

// A commit in a test repository starts no maintenance in the background.
// Git detaches that process, and its lock in .git/objects would come and go
// while a test compares the files before and after a command.
func TestCommitStartsNoBackgroundMaintenance(t *testing.T) {
	t.Parallel()
	repo := cwtest.NewRepo(t)
	repo.Setenv("GIT_TRACE", "1")
	got := repo.TryGit("commit", "-q", "--allow-empty", "-m", "first")
	if got.Code != 0 {
		t.Fatalf("git commit: exit %d\n%s", got.Code, got.Output())
	}
	if strings.Contains(got.Output(), "maintenance run") {
		t.Errorf("the commit started git maintenance:\n%s", got.Output())
	}
}
