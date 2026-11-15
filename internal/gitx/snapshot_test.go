package gitx

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type indexState struct {
	data  []byte
	mtime time.Time
}

func readIndex(t *testing.T, gitDir string) indexState {
	t.Helper()
	path := filepath.Join(gitDir, "index")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return indexState{data: data, mtime: info.ModTime()}
}

func (s indexState) same(other indexState) bool {
	return bytes.Equal(s.data, other.data) && s.mtime.Equal(other.mtime)
}

// tmpOf is the directory the callers pass for index copies.
func tmpOf(repo *Repo) string { return filepath.Join(repo.GitDir, "gatecub", "tmp") }

// checkNoTmp fails when the index copy or its empty directories are left.
func checkNoTmp(t *testing.T, repo *Repo) {
	t.Helper()
	if exists(tmpOf(repo)) {
		t.Errorf("%s is left behind", tmpOf(repo))
	}
	if exists(filepath.Join(repo.GitDir, "gatecub")) {
		t.Errorf("%s is left behind", filepath.Join(repo.GitDir, "gatecub"))
	}
}

func demo(t *testing.T) (string, *Repo) {
	t.Helper()
	return committed(t, map[string]string{
		"README.md":   "# demo\n",
		"src/app.txt": "ok\n",
		".gitignore":  "build/\n",
	})
}

// expectedSnapshot is the tree `git add -A && git write-tree` records in a
// scratch copy of the repository.
func expectedSnapshot(t *testing.T, dir string) string {
	t.Helper()
	scratch := filepath.Join(t.TempDir(), "scratch")
	copyTree(t, dir, scratch)
	git(t, scratch, "add", "-A")
	return git(t, scratch, "write-tree")
}

func snapshot(t *testing.T, repo *Repo) string {
	t.Helper()
	tree, err := repo.Snapshot(t.Context(), tmpOf(repo))
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return tree
}

func TestSnapshotOfCleanTreeIsHeadTree(t *testing.T) {
	dir, repo := demo(t)
	if got, want := snapshot(t, repo), git(t, dir, "rev-parse", "HEAD^{tree}"); got != want {
		t.Errorf("Snapshot = %s, want the tree of HEAD %s", got, want)
	}
	checkNoTmp(t, repo)
}

func TestSnapshotHoldsUntrackedAndEditedFiles(t *testing.T) {
	dir, repo := demo(t)
	head := git(t, dir, "rev-parse", "HEAD^{tree}")

	write(t, dir, "src/new.txt", "new\n")
	write(t, dir, "src/app.txt", "ok \n")
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	// git status may refresh the index, so the index is read after it.
	statusBefore := git(t, dir, "status", "--porcelain")
	before := readIndex(t, repo.GitDir)

	got := snapshot(t, repo)
	if got == head {
		t.Fatal("the snapshot ignores the changes")
	}
	if after := readIndex(t, repo.GitDir); !before.same(after) {
		t.Error("Snapshot changed the bytes or the modification time of the index")
	}
	if want := expectedSnapshot(t, dir); got != want {
		t.Errorf("Snapshot = %s, want %s as git add -A gives", got, want)
	}
	if after := git(t, dir, "status", "--porcelain"); after != statusBefore {
		t.Errorf("git status changed:\n%s\n---\n%s", statusBefore, after)
	}
	checkNoTmp(t, repo)
}

func TestSnapshotExcludesIgnoredFiles(t *testing.T) {
	dir, repo := demo(t)
	before := snapshot(t, repo)
	write(t, dir, "build/out.bin", "binary\n")
	if after := snapshot(t, repo); after != before {
		t.Errorf("an ignored file changed the snapshot: %s != %s", after, before)
	}
}

func TestSnapshotKeepsStagedContentUnchanged(t *testing.T) {
	dir, repo := demo(t)
	write(t, dir, "src/app.txt", "staged\n")
	git(t, dir, "add", "src/app.txt")
	write(t, dir, "src/app.txt", "on disk\n")
	before := readIndex(t, repo.GitDir)
	staged := git(t, dir, "ls-files", "-s", "src/app.txt")

	got := snapshot(t, repo)
	if want := expectedSnapshot(t, dir); got != want {
		t.Errorf("Snapshot = %s, want %s", got, want)
	}
	if !before.same(readIndex(t, repo.GitDir)) {
		t.Error("Snapshot changed the index")
	}
	if after := git(t, dir, "ls-files", "-s", "src/app.txt"); after != staged {
		t.Errorf("the staged entry changed: %s -> %s", staged, after)
	}
}

func TestSnapshotWithoutIndex(t *testing.T) {
	dir := newRepo(t)
	write(t, dir, "a.txt", "a\n")
	repo := open(t, dir)
	got := snapshot(t, repo)
	if want := expectedSnapshot(t, dir); got != want {
		t.Errorf("Snapshot of a repository without an index = %s, want %s", got, want)
	}
	if exists(filepath.Join(repo.GitDir, "index")) {
		t.Error("Snapshot created the index")
	}
	checkNoTmp(t, repo)
}

func TestSnapshotLeavesNonEmptyStore(t *testing.T) {
	_, repo := demo(t)
	mode := filepath.Join(repo.GitDir, "gatecub", "mode")
	write(t, filepath.Dir(mode), "mode", "enforce\n")
	snapshot(t, repo)
	if exists(tmpOf(repo)) {
		t.Error("the tmp directory is left")
	}
	if !exists(mode) {
		t.Error("the cleanup removed the store's own files")
	}
}

func TestSnapshotUsesGitIndexFile(t *testing.T) {
	dir, repo := demo(t)
	alt := filepath.Join(repo.GitDir, "alt-index")
	data, err := os.ReadFile(filepath.Join(repo.GitDir, "index"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(alt, data, 0o644); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "src/app.txt", "edited\n")
	t.Setenv("GIT_INDEX_FILE", alt)
	before := readIndex(t, repo.GitDir)
	altInfo, _ := os.Stat(alt)

	tree, err := repo.IndexTree(t.Context(), tmpOf(repo))
	if err != nil {
		t.Fatal(err)
	}
	if want := git(t, dir, "rev-parse", "HEAD^{tree}"); tree != want {
		t.Errorf("IndexTree with GIT_INDEX_FILE = %s, want %s", tree, want)
	}
	if got := snapshot(t, repo); got == tree {
		t.Error("Snapshot with GIT_INDEX_FILE ignores the edit")
	}
	if !before.same(readIndex(t, repo.GitDir)) {
		t.Error("the default index changed")
	}
	if info, _ := os.Stat(alt); !info.ModTime().Equal(altInfo.ModTime()) {
		t.Error("the index GIT_INDEX_FILE names changed")
	}
}

func TestSnapshotRefusesMarkedFileThatDiffers(t *testing.T) {
	for _, mark := range []string{"skip-worktree", "assume-unchanged"} {
		t.Run(mark, func(t *testing.T) {
			dir, repo := demo(t)
			git(t, dir, "update-index", "--"+mark, "README.md")
			// An unchanged marked file is fine.
			snapshot(t, repo)

			write(t, dir, "README.md", "# local override\n")
			tree, err := repo.Snapshot(t.Context(), tmpOf(repo))
			var wtErr *WorktreeError
			if !errors.As(err, &wtErr) {
				t.Fatalf("Snapshot = %s, %v; want a *WorktreeError", tree, err)
			}
			if wtErr.Path != "README.md" || !strings.Contains(err.Error(), mark) || !strings.Contains(err.Error(), "differs") {
				t.Errorf("error = %q", err)
			}
			checkNoTmp(t, repo)
		})
	}
}

func TestSnapshotRefusesMarkedFileThatIsMissing(t *testing.T) {
	dir, repo := demo(t)
	git(t, dir, "update-index", "--skip-worktree", "README.md")
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	_, err := repo.Snapshot(t.Context(), tmpOf(repo))
	var wtErr *WorktreeError
	if !errors.As(err, &wtErr) || wtErr.Path != "README.md" || !strings.Contains(err.Error(), "missing") {
		t.Errorf("error = %v", err)
	}

	git(t, dir, "config", "core.sparseCheckout", "true")
	_, err = repo.Snapshot(t.Context(), tmpOf(repo))
	if !errors.As(err, &wtErr) || !strings.Contains(err.Error(), "sparse checkout") {
		t.Errorf("with a sparse checkout, error = %v", err)
	}
}

func TestSnapshotRefusesNestedRepositoryWithoutCommit(t *testing.T) {
	dir, repo := demo(t)
	git(t, dir, "init", "-q", "sub")
	_, err := repo.Snapshot(t.Context(), tmpOf(repo))
	var wtErr *WorktreeError
	if !errors.As(err, &wtErr) || !strings.Contains(err.Error(), "sub") {
		t.Errorf("error = %v", err)
	}
	checkNoTmp(t, repo)
}

func TestSnapshotRefusesNestedRepositoryWithChanges(t *testing.T) {
	dir, repo := demo(t)
	sub := filepath.Join(dir, "vendor", "lib")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, sub, "init", "-q", "-b", "main", ".")
	write(t, sub, "lib.txt", "1\n")
	git(t, sub, "add", "-A")
	git(t, sub, "commit", "-q", "-m", "lib")
	// A clean nested repository is recorded as its commit.
	snapshot(t, repo)

	write(t, sub, "lib.txt", "2\n")
	_, err := repo.Snapshot(t.Context(), tmpOf(repo))
	var wtErr *WorktreeError
	if !errors.As(err, &wtErr) || wtErr.Path != "vendor/lib" || !strings.Contains(err.Error(), "uncommitted") {
		t.Errorf("error = %v", err)
	}
}

func TestIndexTree(t *testing.T) {
	dir, repo := demo(t)
	write(t, dir, "src/app.txt", "staged\n")
	git(t, dir, "add", "src/app.txt")
	write(t, dir, "src/untracked.txt", "u\n")
	write(t, dir, "README.md", "edited, not staged\n")
	before := readIndex(t, repo.GitDir)

	tree, err := repo.IndexTree(t.Context(), tmpOf(repo))
	if err != nil {
		t.Fatal(err)
	}
	if !before.same(readIndex(t, repo.GitDir)) {
		t.Error("IndexTree rewrote the index file")
	}
	checkNoTmp(t, repo)
	if want := git(t, dir, "write-tree"); tree != want {
		t.Errorf("IndexTree = %s, want git write-tree %s", tree, want)
	}
}

func TestIndexTreeWithoutIndex(t *testing.T) {
	dir := newRepo(t)
	repo := open(t, dir)
	tree, err := repo.IndexTree(t.Context(), tmpOf(repo))
	if err != nil {
		t.Fatal(err)
	}
	if want := gitIn(t, dir, "", "mktree"); tree != want {
		t.Errorf("IndexTree without an index = %s, want the empty tree %s", tree, want)
	}
	checkNoTmp(t, repo)
}

func TestIndexTreeInLinkedWorktree(t *testing.T) {
	dir, _ := demo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt")
	git(t, dir, "worktree", "add", "-q", wt)
	write(t, wt, "README.md", "in wt\n")
	git(t, wt, "add", "README.md")
	linked := open(t, wt)
	tmp := filepath.Join(linked.CommonDir, "gatecub", "tmp")
	tree, err := linked.IndexTree(t.Context(), tmp)
	if err != nil {
		t.Fatal(err)
	}
	if want := git(t, wt, "write-tree"); tree != want {
		t.Errorf("IndexTree in a linked worktree = %s, want %s", tree, want)
	}
	if exists(tmp) || exists(filepath.Dir(tmp)) {
		t.Error("the tmp directory is left")
	}
}

func TestWorktreeErrorText(t *testing.T) {
	if got := (&WorktreeError{Cause: "x"}).Error(); got != "the working tree cannot be recorded: x" {
		t.Errorf("Error() = %q", got)
	}
	if got := (&WorktreeError{Path: "a", Cause: "x"}).Error(); got != "the working tree cannot be recorded: a x" {
		t.Errorf("Error() = %q", got)
	}
}
