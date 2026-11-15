package evidence_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// A runnable local override must not produce evidence for a non-executable
// committed script, even when git is told to overlook that path.
func TestMarkedExecutableCannotProducePassingEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose the POSIX executable bit")
	}
	t.Parallel()
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			t.Parallel()
			repo := gatecubtest.NewRepo(t)
			repo.Git("config", "core.filemode", "true")
			repo.Write("check.sh", "#!/bin/sh\nexit 0\n")
			repo.Write("gatecub.toml", "version = 1\n[[check]]\nid = \"script\"\nrun = [\"./check.sh\"]\n")
			repo.Git("add", "-A")
			repo.Git("commit", "-qm", "non-executable script")
			if result := repo.Gatecub("init"); result.Code != 0 {
				t.Fatalf("gatecub init: %s", result.Output())
			}
			repo.Git("update-index", "--"+mark, "check.sh")
			if err := os.Chmod(filepath.Join(repo.Dir, "check.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			result := repo.Gatecub("run")
			requireRefused(t, result, "", "check.sh", mark, "differs")
			if records := repo.RecordFiles("script"); len(records) != 0 {
				t.Errorf("recorded runs on a mismatched tree: %v", records)
			}
			head := repo.Git("rev-parse", "HEAD")
			commit := repo.TryGit("commit", "--allow-empty", "-qm", "unchecked content")
			if commit.Code == 0 || !strings.Contains(commit.Output(), "denied") {
				t.Errorf("the commit was not denied: exit %d\n%s", commit.Code, commit.Output())
			}
			if got := repo.Git("rev-parse", "HEAD"); got != head {
				t.Errorf("the denied commit moved HEAD from %s to %s", head, got)
			}
		})
	}
}
