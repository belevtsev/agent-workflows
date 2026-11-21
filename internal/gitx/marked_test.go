package gitx

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSnapshotMarkedExecutableMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose the POSIX executable bit")
	}
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		for _, tc := range []struct {
			name     string
			before   os.FileMode
			after    os.FileMode
			filemode string
			refused  bool
		}{
			{"added", 0o644, 0o755, "true", true},
			{"removed", 0o755, 0o644, "true", true},
			{"ignored", 0o644, 0o755, "false", false},
		} {
			t.Run(mark+"/"+tc.name, func(t *testing.T) {
				dir := newRepo(t)
				write(t, dir, "check.sh", "#!/bin/sh\nexit 0\n")
				path := filepath.Join(dir, "check.sh")
				if err := os.Chmod(path, tc.before); err != nil {
					t.Fatal(err)
				}
				git(t, dir, "config", "core.filemode", "true")
				git(t, dir, "add", "-A")
				git(t, dir, "commit", "-qm", "script")
				git(t, dir, "config", "core.filemode", tc.filemode)
				git(t, dir, "update-index", "--"+mark, "check.sh")
				if err := os.Chmod(path, tc.after); err != nil {
					t.Fatal(err)
				}
				assertMarkedSnapshot(t, open(t, dir), "check.sh", mark, tc.refused)
			})
		}
	}
}

func TestSnapshotMarkedSymlinkIdentity(t *testing.T) {
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		for _, name := range []string{"unchanged", "target changed", "file becomes link", "link becomes file"} {
			t.Run(mark+"/"+name, func(t *testing.T) {
				dir := newRepo(t)
				git(t, dir, "config", "core.symlinks", "true")
				write(t, dir, "target", "content\n")
				write(t, dir, "other", "content\n")
				const rel = "link with [brackets]"
				path := filepath.Join(dir, rel)
				if name == "file becomes link" {
					write(t, dir, rel, "content\n")
				} else {
					markedSymlink(t, "target", path)
				}
				git(t, dir, "add", "-A")
				git(t, dir, "commit", "-qm", "link")
				git(t, dir, "update-index", "--"+mark, rel)
				if name != "unchanged" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					switch name {
					case "target changed":
						markedSymlink(t, "other", path)
					case "file becomes link":
						markedSymlink(t, "target", path)
					case "link becomes file":
						write(t, dir, rel, "target")
					}
				}
				assertMarkedSnapshot(t, open(t, dir), rel, mark, name != "unchanged")
			})
		}
	}
}

func markedSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks are not available: %v", err)
		}
		t.Fatal(err)
	}
}

func TestSnapshotMarkedSymlinkPlaceholder(t *testing.T) {
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			dir := newRepo(t)
			git(t, dir, "config", "core.symlinks", "false")
			write(t, dir, "link", "target")
			object := git(t, dir, "hash-object", "-w", "link")
			git(t, dir, "update-index", "--add", "--cacheinfo", "120000,"+object+",link")
			git(t, dir, "commit", "-qm", "link placeholder")
			git(t, dir, "update-index", "--"+mark, "link")
			repo := open(t, dir)
			assertMarkedSnapshot(t, repo, "link", mark, false)
			write(t, dir, "link", "other")
			assertMarkedSnapshot(t, repo, "link", mark, true)
		})
	}
}

func TestSnapshotMarkedCleanFilter(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the clean filter uses the POSIX tr program")
	}
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			dir := newRepo(t)
			write(t, dir, ".gitattributes", "filtered filter=lowercase\n")
			write(t, dir, "filtered", "CONTENT\n")
			git(t, dir, "config", "filter.lowercase.clean", "tr A-Z a-z")
			git(t, dir, "config", "filter.lowercase.required", "true")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-qm", "filter")
			git(t, dir, "update-index", "--"+mark, "filtered")
			repo := open(t, dir)
			assertMarkedSnapshot(t, repo, "filtered", mark, false)
			write(t, dir, "filtered", "OTHER\n")
			assertMarkedSnapshot(t, repo, "filtered", mark, true)
		})
	}
}

// With text=auto, git keeps the line endings of a file that was committed
// with CRLF. An untouched marked file must not read as changed for it.
func TestSnapshotMarkedFileCommittedWithCRLF(t *testing.T) {
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			dir := newRepo(t)
			git(t, dir, "config", "core.autocrlf", "false")
			write(t, dir, "crlf.txt", "a\r\nb\r\n")
			git(t, dir, "add", "-A")
			git(t, dir, "commit", "-qm", "content with CRLF")
			write(t, dir, ".gitattributes", "* text=auto\n")
			git(t, dir, "add", ".gitattributes")
			git(t, dir, "commit", "-qm", "attributes")
			git(t, dir, "update-index", "--"+mark, "crlf.txt")
			repo := open(t, dir)
			assertMarkedSnapshot(t, repo, "crlf.txt", mark, false)
			write(t, dir, "crlf.txt", "a\r\nchanged\r\n")
			assertMarkedSnapshot(t, repo, "crlf.txt", mark, true)
		})
	}
}

func TestSnapshotMarkedPresentSparseFile(t *testing.T) {
	for _, mark := range []string{"assume-unchanged", "skip-worktree"} {
		t.Run(mark, func(t *testing.T) {
			dir, repo := committed(t, map[string]string{"kept/file": "kept\n", "outside/file": "outside\n"})
			git(t, dir, "config", "core.sparseCheckout", "true")
			write(t, repo.GitDir, "info/sparse-checkout", "/kept/\n")
			git(t, dir, "update-index", "--"+mark, "outside/file")
			assertMarkedSnapshot(t, repo, "outside/file", mark, false)
			if got := git(t, dir, "config", "--get", "core.sparseCheckout"); got != "true" {
				t.Errorf("Snapshot changed core.sparseCheckout to %q", got)
			}
		})
	}
}

func TestSnapshotBothIndexMarks(t *testing.T) {
	dir, repo := committed(t, map[string]string{"marked": "content\n"})
	git(t, dir, "update-index", "--skip-worktree", "marked")
	git(t, dir, "update-index", "--assume-unchanged", "marked")
	if got := git(t, dir, "ls-files", "-v", "--", "marked"); got != "s marked" {
		t.Fatalf("the fixture does not hold both index marks: %q", got)
	}
	assertMarkedSnapshot(t, repo, "marked", "skip-worktree", false)
	write(t, dir, "marked", "changed\n")
	assertMarkedSnapshot(t, repo, "marked", "skip-worktree", true)
}

func assertMarkedSnapshot(t *testing.T, repo *Repo, path, mark string, refused bool) {
	t.Helper()
	before := readIndex(t, repo.GitDir)
	tree, err := repo.Snapshot(t.Context(), tmpOf(repo))
	if refused {
		var wtErr *WorktreeError
		if !errors.As(err, &wtErr) || wtErr.Path != path || !strings.Contains(wtErr.Cause, mark) || !strings.Contains(wtErr.Cause, "differs") {
			t.Errorf("Snapshot = %q, %v; want a marked-path difference for %q", tree, err, path)
		}
	} else if err != nil {
		t.Errorf("Snapshot of unchanged marked content: %v", err)
	} else if want := git(t, repo.Root, "rev-parse", "HEAD^{tree}"); tree != want {
		t.Errorf("Snapshot = %q, want unchanged tree %q", tree, want)
	}
	if !before.same(readIndex(t, repo.GitDir)) {
		t.Error("Snapshot changed the real index or its timestamp")
	}
	checkNoTmp(t, repo)
}
