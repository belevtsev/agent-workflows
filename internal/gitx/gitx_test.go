package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func open(t *testing.T, dir string) *Repo {
	t.Helper()
	repo, err := Open(t.Context(), dir)
	if err != nil {
		t.Fatalf("Open(%s): %v", dir, err)
	}
	return repo
}

// committed creates a repository with one commit holding the given files.
func committed(t *testing.T, files map[string]string) (string, *Repo) {
	t.Helper()
	dir := newRepo(t)
	for rel, content := range files {
		write(t, dir, rel, content)
	}
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "first")
	return dir, open(t, dir)
}

func TestOpen(t *testing.T) {
	dir := newRepo(t)
	repo := open(t, dir)
	if repo.Root != dir {
		t.Errorf("Root = %q, want %q", repo.Root, dir)
	}
	if repo.GitDir != filepath.Join(dir, ".git") || repo.CommonDir != filepath.Join(dir, ".git") {
		t.Errorf("GitDir = %q, CommonDir = %q", repo.GitDir, repo.CommonDir)
	}
	write(t, dir, "sub/a.txt", "a")
	inner := open(t, filepath.Join(dir, "sub"))
	if inner.Root != dir || inner.CommonDir != repo.CommonDir {
		t.Errorf("from a subdirectory: Root = %q, CommonDir = %q", inner.Root, inner.CommonDir)
	}
}

func TestOpenNotARepository(t *testing.T) {
	base := isolate(t)
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	repo, err := Open(t.Context(), plain)
	if !errors.Is(err, ErrNotRepo) {
		t.Errorf("Open of a plain directory = %+v, %v; want ErrNotRepo", repo, err)
	}
}

func TestOpenLinkedWorktreeSharesCommonDir(t *testing.T) {
	dir, main := committed(t, map[string]string{"a.txt": "a\n"})
	wt := filepath.Join(filepath.Dir(dir), "wt")
	git(t, dir, "worktree", "add", "-q", wt)
	linked := open(t, wt)
	if linked.CommonDir != main.CommonDir {
		t.Errorf("CommonDir of the linked worktree = %q, want %q", linked.CommonDir, main.CommonDir)
	}
	if linked.Root != wt {
		t.Errorf("Root = %q, want %q", linked.Root, wt)
	}
	if linked.GitDir == main.GitDir || !strings.HasPrefix(linked.GitDir, filepath.Join(main.CommonDir, "worktrees")) {
		t.Errorf("GitDir of the linked worktree = %q", linked.GitDir)
	}
}

func TestOpenBare(t *testing.T) {
	base := isolate(t)
	bare := filepath.Join(base, "bare.git")
	git(t, base, "init", "-q", "--bare", bare)
	repo := open(t, bare)
	if repo.Root != "" || repo.GitDir != bare {
		t.Errorf("bare: Root = %q, GitDir = %q", repo.Root, repo.GitDir)
	}
	if _, err := repo.Snapshot(t.Context(), filepath.Join(bare, "kitsu", "tmp")); err == nil {
		t.Error("Snapshot of a bare repository gave a tree")
	}
}

func TestTreeOfAndCommitOf(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	head := git(t, dir, "rev-parse", "HEAD")
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")
	git(t, dir, "tag", "-a", "-m", "v1", "v1")
	tagObject := git(t, dir, "rev-parse", "v1")
	if tagObject == head {
		t.Fatal("the tag is not annotated")
	}

	cases := []struct {
		rev        string
		wantTree   string
		wantCommit string
	}{
		{"HEAD", tree, head},
		{"main", tree, head},
		{"v1", tree, head},
		{tagObject, tree, head},
		{tree, tree, ""},
		{"nosuch", "", ""},
		{"0123456789012345678901234567890123456789", "", ""},
		{"--help", "", ""},
	}
	for _, tc := range cases {
		gotTree, found, err := repo.TreeOf(t.Context(), tc.rev)
		if err != nil || found != (tc.wantTree != "") || gotTree != tc.wantTree {
			t.Errorf("TreeOf(%q) = %q, %v, %v; want %q", tc.rev, gotTree, found, err, tc.wantTree)
		}
		gotCommit, found, err := repo.CommitOf(t.Context(), tc.rev)
		if err != nil || found != (tc.wantCommit != "") || gotCommit != tc.wantCommit {
			t.Errorf("CommitOf(%q) = %q, %v, %v; want %q", tc.rev, gotCommit, found, err, tc.wantCommit)
		}
	}
}

func TestContains(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	first := git(t, dir, "rev-parse", "HEAD")
	write(t, dir, "a.txt", "b\n")
	git(t, dir, "commit", "-q", "-am", "second")
	second := git(t, dir, "rev-parse", "HEAD")
	cases := []struct {
		ancestor, descendant string
		want                 bool
	}{
		{first, second, true},
		{second, second, true},
		{second, first, false},
		{"0123456789012345678901234567890123456789", second, false},
	}
	for _, tc := range cases {
		got, err := repo.Contains(t.Context(), tc.ancestor, tc.descendant)
		if err != nil || got != tc.want {
			t.Errorf("Contains(%.7s, %.7s) = %v, %v; want %v", tc.ancestor, tc.descendant, got, err, tc.want)
		}
	}
}

func TestRemoteHolds(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	first := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-ref", "refs/remotes/origin/main", first)
	git(t, dir, "update-ref", "refs/remotes/other/main", first)
	write(t, dir, "a.txt", "b\n")
	git(t, dir, "commit", "-q", "-am", "second")
	second := git(t, dir, "rev-parse", "HEAD")
	git(t, dir, "update-ref", "refs/remotes/other/main", second)

	cases := []struct {
		remote, commit string
		want           bool
	}{
		{"origin", first, true},
		{"origin", second, false},
		{"other", second, true},
		{"orig", first, false},
		{"", first, false},
		{"o*", first, false},
		{"nosuch", first, false},
		{"origin", "0123456789012345678901234567890123456789", false},
	}
	for _, tc := range cases {
		got, err := repo.RemoteHolds(t.Context(), tc.remote, tc.commit)
		if err != nil || got != tc.want {
			t.Errorf("RemoteHolds(%q, %.7s) = %v, %v; want %v", tc.remote, tc.commit, got, err, tc.want)
		}
	}
}

func TestDiff(t *testing.T) {
	dir, repo := committed(t, map[string]string{"keep.txt": "k\n", "change.txt": "1\n", "remove.txt": "r\n", "move/a.txt": "m\n"})
	from := git(t, dir, "rev-parse", "HEAD^{tree}")
	write(t, dir, "change.txt", "2\n")
	write(t, dir, "add dir/new file.txt", "n\n")
	git(t, dir, "rm", "-q", "remove.txt")
	git(t, dir, "mv", "move/a.txt", "moved.txt")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-q", "-m", "second")
	to := git(t, dir, "rev-parse", "HEAD^{tree}")

	changes, err := repo.Diff(t.Context(), from, to)
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Path: "add dir/new file.txt", Kind: "added"},
		{Path: "change.txt", Kind: "changed"},
		{Path: "move/a.txt", Kind: "removed"},
		{Path: "moved.txt", Kind: "added"},
		{Path: "remove.txt", Kind: "removed"},
	}
	if !slices.Equal(changes, want) {
		t.Errorf("Diff = %+v, want %+v", changes, want)
	}
	same, err := repo.Diff(t.Context(), from, from)
	if err != nil || len(same) != 0 {
		t.Errorf("Diff of a tree with itself = %v, %v", same, err)
	}
	if _, err := repo.Diff(t.Context(), from, "nosuch"); err == nil {
		t.Error("Diff with an unknown tree gave no error")
	}
}

func TestDiffModeChange(t *testing.T) {
	dir, repo := committed(t, map[string]string{"run.sh": "echo\n"})
	from := git(t, dir, "rev-parse", "HEAD^{tree}")
	blob := git(t, dir, "rev-parse", "HEAD:run.sh")
	to := gitIn(t, dir, "100755 blob "+blob+"\trun.sh\n", "mktree")
	changes, err := repo.Diff(t.Context(), from, to)
	if err != nil || !slices.Equal(changes, []Change{{Path: "run.sh", Kind: "changed"}}) {
		t.Errorf("Diff of a mode change = %+v, %v", changes, err)
	}
}

func TestDeclaration(t *testing.T) {
	dir, repo := committed(t, map[string]string{"kitsu.toml": "version = 1\n", "sub/kitsu.toml": "other\n"})
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")
	data, found, err := repo.Declaration(t.Context(), tree, "kitsu.toml")
	if err != nil || !found || string(data) != "version = 1\n" {
		t.Errorf("Declaration = %q, %v, %v", data, found, err)
	}

	blob := git(t, dir, "rev-parse", "HEAD:kitsu.toml")
	subtree := git(t, dir, "rev-parse", "HEAD:sub")
	head := git(t, dir, "rev-parse", "HEAD")
	cases := []struct {
		name    string
		entries string
		found   bool
		wantErr bool
	}{
		{"no entry", "100644 blob " + blob + "\tother.toml\n", false, false},
		{"executable file", "100755 blob " + blob + "\tkitsu.toml\n", true, false},
		{"directory", "040000 tree " + subtree + "\tkitsu.toml\n", true, true},
		{"symbolic link", "120000 blob " + blob + "\tkitsu.toml\n", true, true},
		{"submodule", "160000 commit " + head + "\tkitsu.toml\n", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tree := gitIn(t, dir, tc.entries, "mktree")
			data, found, err := repo.Declaration(t.Context(), tree, "kitsu.toml")
			if found != tc.found || (err != nil) != tc.wantErr {
				t.Errorf("Declaration = %q, found %v, err %v", data, found, err)
			}
		})
	}
	empty := gitIn(t, dir, "", "mktree")
	if _, found, err := repo.Declaration(t.Context(), empty, "kitsu.toml"); found || err != nil {
		t.Errorf("Declaration of an empty tree: found %v, err %v", found, err)
	}
	if _, _, err := repo.Declaration(t.Context(), "nosuch", "kitsu.toml"); err == nil {
		t.Error("Declaration of an unknown tree gave no error")
	}
}

func TestEntries(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n", "dir/b.txt": "b\n", "dir/sub/c.txt": "c\n", "with space.txt": "s\n"})
	head := git(t, dir, "rev-parse", "HEAD")
	tree := git(t, dir, "rev-parse", "HEAD^{tree}")
	// A submodule is one entry.
	withSub := gitIn(t, dir, git(t, dir, "ls-tree", tree)+"\n160000 commit "+head+"\tvendor\n", "mktree")

	var paths []string
	var raws []string
	err := repo.Entries(t.Context(), withSub, func(raw []byte, path string) {
		paths = append(paths, path)
		raws = append(raws, string(raw))
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.txt", "dir/b.txt", "dir/sub/c.txt", "vendor", "with space.txt"}
	if !slices.Equal(paths, want) {
		t.Errorf("paths = %q, want %q", paths, want)
	}
	listing := gitIn(t, dir, "", "ls-tree", "-r", "-z", withSub)
	if strings.Join(raws, "") != listing {
		t.Errorf("the raw entries are not git's own listing:\n%q\n%q", strings.Join(raws, ""), listing)
	}
	for _, raw := range raws {
		if !strings.HasSuffix(raw, "\x00") {
			t.Errorf("raw entry %q does not end with NUL", raw)
		}
	}
	if err := repo.Entries(t.Context(), "nosuch", func([]byte, string) {}); err == nil {
		t.Error("Entries of an unknown tree gave no error")
	}
}

func TestNulSplitterAcrossWrites(t *testing.T) {
	var got []string
	s := &nulSplitter{emit: func(record []byte) { got = append(got, string(record)) }}
	for _, part := range []string{"ab", "c\x00d", "\x00", "e\x00f\x00g"} {
		if n, err := s.Write([]byte(part)); err != nil || n != len(part) {
			t.Fatalf("Write = %d, %v", n, err)
		}
	}
	want := []string{"abc\x00", "d\x00", "e\x00", "f\x00"}
	if !slices.Equal(got, want) {
		t.Errorf("records = %q, want %q", got, want)
	}
}

func TestHooksDir(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	hooks, configured, err := repo.HooksDir(t.Context())
	if err != nil || configured || hooks != filepath.Join(dir, ".git", "hooks") {
		t.Errorf("HooksDir = %q, %v, %v", hooks, configured, err)
	}

	wt := filepath.Join(filepath.Dir(dir), "wt")
	git(t, dir, "worktree", "add", "-q", wt)
	hooks, configured, err = open(t, wt).HooksDir(t.Context())
	if err != nil || configured || hooks != filepath.Join(dir, ".git", "hooks") {
		t.Errorf("HooksDir in a linked worktree = %q, %v, %v; want the common directory's", hooks, configured, err)
	}

	git(t, dir, "config", "core.hooksPath", ".githooks")
	hooks, configured, err = repo.HooksDir(t.Context())
	if err != nil || !configured || hooks != filepath.Join(dir, ".githooks") {
		t.Errorf("HooksDir with a relative core.hooksPath = %q, %v, %v", hooks, configured, err)
	}
	abs := filepath.Join(filepath.Dir(dir), "shared-hooks")
	git(t, dir, "config", "core.hooksPath", abs)
	hooks, configured, err = repo.HooksDir(t.Context())
	if err != nil || !configured || hooks != abs {
		t.Errorf("HooksDir with an absolute core.hooksPath = %q, %v, %v", hooks, configured, err)
	}
}

func TestSparseCheckout(t *testing.T) {
	dir, repo := committed(t, map[string]string{"a.txt": "a\n"})
	if repo.SparseCheckout(t.Context()) {
		t.Error("a plain checkout reads as sparse")
	}
	git(t, dir, "config", "core.sparseCheckout", "true")
	if !repo.SparseCheckout(t.Context()) {
		t.Error("a sparse checkout reads as not sparse")
	}
}

func TestErrorText(t *testing.T) {
	err := &Error{Args: []string{"status"}, Code: 2, Stderr: " fatal: x \n"}
	if err.Error() != "git status: fatal: x" {
		t.Errorf("Error() = %q", err.Error())
	}
	err.Stderr = ""
	if err.Error() != "git status: exit status 2" {
		t.Errorf("Error() = %q", err.Error())
	}
}

func TestOpenCheckoutWithNewlineInPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows paths cannot contain a newline")
	}
	parent := isolate(t)
	for _, bare := range []bool{false, true} {
		dir := filepath.Join(parent, "source\ncheckout")
		args := []string{"init", "-q"}
		if bare {
			dir += ".git"
			args = append(args, "--bare")
		}
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		git(t, parent, append(args, dir)...)
		wantRoot, wantGit := dir, filepath.Join(dir, ".git")
		if bare {
			wantRoot, wantGit = "", dir
		}
		repo := open(t, dir)
		if repo.Root != wantRoot || repo.GitDir != wantGit || repo.CommonDir != wantGit {
			t.Fatalf("worktree %q, git dir %q, common dir %q; expected %q and %q", repo.Root, repo.GitDir, repo.CommonDir, wantRoot, wantGit)
		}
	}
}

func TestOpenReportsBrokenRepository(t *testing.T) {
	dir := isolate(t)
	git(t, dir, "init", "-q")
	write(t, dir, ".git/config", "[invalid\n")
	repo, err := Open(t.Context(), dir)
	var gitErr *Error
	if repo != nil || errors.Is(err, ErrNotRepo) || !errors.As(err, &gitErr) {
		t.Fatalf("Open returned repo=%v, error=%v; want the Git configuration error", repo, err)
	}
}

func TestOpenRecoversFromInterruptedGitOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX Git shim")
	}
	dir := newRepo(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	shim := `#!/bin/sh
if [ ! -e "$KITSU_TEST_DRAIN_MARKER" ]; then
  : > "$KITSU_TEST_DRAIN_MARKER"
  printf '%s\n' "$PWD/.git"
  sleep 5 &
  exit 0
fi
exec "$KITSU_TEST_REAL_GIT" "$@"
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(shim), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KITSU_TEST_DRAIN_MARKER", filepath.Join(bin, "started"))
	t.Setenv("KITSU_TEST_REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	// The first invocation exits cleanly without closing its output pipe.
	repo := open(t, dir)
	if repo.Root != dir || repo.GitDir != filepath.Join(dir, ".git") || repo.CommonDir != repo.GitDir {
		t.Fatalf("incomplete discovery recovered the wrong repository: %+v", repo)
	}
}
