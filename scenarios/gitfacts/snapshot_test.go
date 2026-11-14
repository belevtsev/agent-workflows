package gitfacts_test

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"
)

// Sources, first script: "The tree written from a temporary index after
// `git add -A` equals the tree of a commit made after `git add -A`, and the
// real index is unchanged."
func TestSnapshotEqualsTheTreeOfACommitAfterAddAll(t *testing.T) {
	t.Parallel()
	repo := newRepo(t)
	repo.Write(".gitignore", "ignored.log\n")
	commitFile(repo, "a.txt", "a\n", "init")
	repo.Git("add", ".gitignore")
	repo.Git("commit", "-q", "--no-verify", "-m", "ignore")
	repo.Write("a.txt", "a\nb\n")
	repo.Write("new.txt", "new\n")
	repo.Write("ignored.log", "x\n")

	status := repo.Git("status", "--porcelain")
	index := gitPath(repo, "index")
	before, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	tree := mustSnapshot(t, repo)
	after, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("the real index changed while the snapshot was taken")
	}
	if got := repo.Git("status", "--porcelain"); got != status {
		t.Errorf("git status changed:\nbefore %q\nafter  %q", status, got)
	}

	repo.Git("add", "-A")
	repo.Git("commit", "-q", "--no-verify", "-m", "all")
	if got := treeOf(repo, "HEAD"); got != tree {
		t.Errorf("the commit recorded tree %s, the snapshot was %s", got, tree)
	}
	files := repo.Git("ls-tree", "-r", "--name-only", tree)
	if !strings.Contains(files, "new.txt") || strings.Contains(files, "ignored.log") {
		t.Errorf("the snapshot holds:\n%s\nwant new.txt and not ignored.log", files)
	}
}

// Sources, second script: "With a file marked skip-worktree or
// assume-unchanged and changed on disk, and with a sparse checkout, the tree
// after `git add -A` equals the tree of `HEAD`. `git ls-files -v` shows the
// marks."
func TestFilesGitIsToldToOverlookStayAsInHead(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	repo.Write("conf", "committed\n")
	repo.Write("b/y", "y\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "--no-verify", "-m", "more")
	head := treeOf(repo, "HEAD")

	for _, mark := range []struct{ flag, letter string }{
		{"skip-worktree", "S"},
		{"assume-unchanged", "h"},
	} {
		repo.Git("update-index", "--"+mark.flag, "conf")
		repo.Write("conf", "local override\n")
		if got := mustSnapshot(t, repo); got != head {
			t.Errorf("%s: the snapshot is %s, want the tree of HEAD %s", mark.flag, got, head)
		}
		if got, want := repo.Git("ls-files", "-v", "conf"), mark.letter+" conf"; got != want {
			t.Errorf("%s: git ls-files -v says %q, want %q", mark.flag, got, want)
		}
		repo.Git("update-index", "--no-"+mark.flag, "conf")
		repo.Git("checkout", "-q", "--", "conf")
	}

	repo.Git("sparse-checkout", "set", "--no-cone", "/*", "!/b/")
	if repo.Exists("b/y") {
		t.Errorf("b/y is on disk after the sparse checkout left it out")
	}
	if got := mustSnapshot(t, repo); got != head {
		t.Errorf("sparse checkout: the snapshot is %s, want the tree of HEAD %s", got, head)
	}
	if got, want := repo.Git("ls-files", "-v", "b/y"), "S b/y"; got != want {
		t.Errorf("sparse checkout: git ls-files -v says %q, want %q", got, want)
	}
}

// Sources, second script: "`git add -A` fails with status 128 when the
// working tree holds an untracked nested repository that has no commit."
func TestAddAllFailsOnANestedRepositoryWithoutCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	repo.Git("init", "-q", "sub")
	if tree, result := snapshot(t, repo); tree != "" || result.Code != 128 {
		t.Errorf("git add -A in a copy of the index: exit status %d, want 128\n%s", result.Code, result.Output())
	}
	if result := repo.TryGit("add", "-A"); result.Code != 128 {
		t.Errorf("git add -A: exit status %d, want 128\n%s", result.Code, result.Output())
	}
}

// Known limits: "A submodule is recorded by its checked-out commit." Git
// records a nested repository by the commit it has checked out, and
// uncommitted changes inside it are not in the tree.
func TestANestedRepositoryIsRecordedByItsCommit(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	sub := repo.In("sub")
	repo.Git("init", "-q", "-b", "main", "sub")
	commitFile(sub, "s.txt", "s\n", "sub")
	commit := sub.Git("rev-parse", "HEAD")
	sub.Write("s.txt", "changed, not committed\n")

	tree := mustSnapshot(t, repo)
	entry := repo.Git("ls-tree", tree, "sub")
	if want := "160000 commit " + commit + "\tsub"; entry != want {
		t.Errorf("the snapshot records sub as %q, want %q", entry, want)
	}
}

// Known limits: "A run is bound to the tree git records, after git's own
// conversions: line endings, clean filters and `core.fileMode`." This test
// holds line endings and a clean filter; the next one holds the mode.
func TestTheTreeHoldsContentAfterGitsConversions(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	// The filter uses only sh builtins, so it runs where sh has no tr or sed.
	repo.Git("config", "filter.mark.clean", `while IFS= read -r l; do echo "clean:$l"; done`)
	repo.Write(".gitattributes", "*.txt text\n*.mark filter=mark\n")
	repo.Write("crlf.txt", "a\r\nb\r\n")
	repo.Write("x.mark", "x\n")
	tree := mustSnapshot(t, repo)

	if got := repo.Git("cat-file", "-p", tree+":crlf.txt"); got != "a\nb" {
		t.Errorf("crlf.txt is recorded as %q, want %q", got, "a\nb")
	}
	if got := repo.Git("cat-file", "-p", tree+":x.mark"); got != "clean:x" {
		t.Errorf("x.mark is recorded as %q, want %q", got, "clean:x")
	}
	if got := repo.Read("crlf.txt"); got != "a\r\nb\r\n" {
		t.Errorf("crlf.txt on disk changed to %q", got)
	}
}

// Known limits: "On Windows git does not record a change of the executable
// mark." git init sets core.fileMode to false there, and a file is recorded
// as not executable. Elsewhere git records the mark it reads on disk.
func TestTheExecutableMarkFollowsCoreFileMode(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	fileMode := repo.Git("config", "--bool", "core.fileMode")
	if err := os.Chmod(repo.Path("f.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	entry := repo.Git("ls-tree", mustSnapshot(t, repo), "f.txt")
	mode, _, _ := strings.Cut(entry, " ")

	want, wantMode := "true", "100755"
	if runtime.GOOS == "windows" {
		want, wantMode = "false", "100644"
	}
	if fileMode != want {
		t.Errorf("core.fileMode is %s on %s, want %s", fileMode, runtime.GOOS, want)
	}
	if mode != wantMode {
		t.Errorf("f.txt is recorded with mode %s after chmod 755, want %s", mode, wantMode)
	}
}

// Known limits: "file names that differ only in case are one file on disk"
// on Windows. Git records what the disk holds: one path where the file
// system ignores case, two where it does not. macOS ignores case by default
// too; the test logs what it found.
func TestNamesThatDifferInCaseFollowTheDisk(t *testing.T) {
	t.Parallel()
	repo := withFile(t)
	repo.Write("dir/readme.txt", "lower\n")
	repo.Write("dir/README.txt", "upper\n")
	entries, err := os.ReadDir(repo.Path("dir"))
	if err != nil {
		t.Fatal(err)
	}
	ignoresCase := len(entries) == 1
	t.Logf("%s: the file system ignores case: %v", runtime.GOOS, ignoresCase)
	if runtime.GOOS == "windows" && !ignoresCase {
		t.Errorf("two names that differ only in case are two files on Windows")
	}
	// git init sets core.ignoreCase only where the file system ignores case.
	setting := strings.TrimSpace(repo.TryGit("config", "--bool", "core.ignoreCase").Stdout)
	if (setting == "true") != ignoresCase {
		t.Errorf("core.ignoreCase is %q, the file system ignores case: %v", setting, ignoresCase)
	}
	paths := strings.Fields(repo.Git("ls-tree", "-r", "--name-only", mustSnapshot(t, repo), "dir"))
	if len(paths) != len(entries) {
		t.Errorf("the snapshot holds %v under dir, the disk holds %d file(s)", paths, len(entries))
	}
}
