package gitx

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A submodule or nested repository is recorded as its commit. A file in it
// that git is told to overlook, and files in the folder of a submodule that
// is not checked out, would be content that no tree records; the snapshot
// refuses both (second challenge, finding G2).
func TestSnapshotRefusesAMarkedPathInANestedRepository(t *testing.T) {
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			dir, repo := demo(t)
			sub := filepath.Join(dir, "vendor", "lib")
			if err := os.MkdirAll(sub, 0o755); err != nil {
				t.Fatal(err)
			}
			git(t, sub, "init", "-q", "-b", "main", ".")
			write(t, sub, "lib.txt", "1\n")
			git(t, sub, "add", "-A")
			git(t, sub, "commit", "-q", "-m", "lib")
			snapshot(t, repo) // clean: recorded as its commit

			git(t, sub, "update-index", "--"+mark, "lib.txt")
			_, err := repo.Snapshot(t.Context(), tmpOf(repo))
			var wtErr *WorktreeError
			if !errors.As(err, &wtErr) || wtErr.Path != "vendor/lib/lib.txt" {
				t.Fatalf("with lib.txt marked %s in vendor/lib: error = %v, want a WorktreeError for vendor/lib/lib.txt", mark, err)
			}
			for _, want := range []string{mark, "clear the mark there first"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the error %q does not hold %q", err.Error(), want)
				}
			}
			checkNoTmp(t, repo)
		})
	}
}

func TestSnapshotRefusesASubmoduleThatIsNotCheckedOut(t *testing.T) {
	dir, repo := demo(t)
	head := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-index", "--add", "--cacheinfo", "160000,"+head+",vendor/lib")
	git(t, dir, "commit", "-q", "-m", "add vendor/lib")

	// When the entire path is gone, git add -A records a deletion. Its
	// result cannot identify a candidate that still contains the gitlink.
	if got := git(t, dir, "ls-tree", snapshot(t, repo), "--", "vendor/lib"); got != "" {
		t.Fatalf("snapshot recorded a gitlink without its checkout: %s", got)
	}
	for _, content := range []string{"empty", "unrelated files"} {
		switch content {
		case "empty":
			if err := os.MkdirAll(filepath.Join(dir, "vendor", "lib"), 0o755); err != nil {
				t.Fatal(err)
			}
		case "unrelated files":
			write(t, dir, "vendor/lib/plain.txt", "a file of no tree\n")
		}
		_, err := repo.Snapshot(t.Context(), tmpOf(repo))
		var wtErr *WorktreeError
		if !errors.As(err, &wtErr) || wtErr.Path != "vendor/lib" || !strings.Contains(err.Error(), "is a submodule that is not checked out") {
			t.Errorf("%s checkout: error = %v, want a WorktreeError for vendor/lib", content, err)
		}
		checkNoTmp(t, repo)
	}
}
