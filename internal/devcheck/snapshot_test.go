package devcheck

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
)

// Tests for contract wp10-snapshot revision 1 (cwdev check --snapshot).

const snapPrefix = "cwdev-snapshot-"

const snapIdentity = "cwdev snapshot <cwdev-snapshot@invalid>"

var snapFullHash = regexp.MustCompile(`^[0-9a-f]{40}$`)

var snapTreeLine = regexp.MustCompile(`(?m)^Snapshot tree: ([0-9a-f]{40}) \((\d+) files from (.+)\)$`)

// snapInheritedGitVariables are cleared for every snapshot test so that the
// environment the tests run in cannot redirect or reconfigure Git.
var snapInheritedGitVariables = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG",
	"GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_AUTHOR_DATE",
	"GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_COMMITTER_DATE", "GIT_TEMPLATE_DIR",
}

// snapIsolate gives the test a fresh TMPDIR and HOME and clears inherited Git
// variables. It returns the TMPDIR and HOME values.
func snapIsolate(t *testing.T) (string, string) {
	t.Helper()
	tmp := t.TempDir()
	home := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range snapInheritedGitVariables {
		snapUnsetenv(t, name)
	}
	if got := os.TempDir(); got != tmp {
		t.Fatalf("test setup: os.TempDir() = %q, want %q", got, tmp)
	}
	return tmp, home
}

func snapUnsetenv(t *testing.T, name string) {
	t.Helper()
	t.Setenv(name, "")
	if err := os.Unsetenv(name); err != nil {
		t.Fatal(err)
	}
}

// snapGitEnvironment is the environment for Git commands the tests themselves
// run: inherited GIT_* variables (including ones a test sets for the code under
// test) are removed.
func snapGitEnvironment() []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "GIT_") {
			continue
		}
		environment = append(environment, entry)
	}
	return append(environment, "GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0")
}

func snapRunGit(dir string, args ...string) (string, string, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = snapGitEnvironment()
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}

func snapGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	stdout, stderr, err := snapRunGit(dir, args...)
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s%s", dir, strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout
}

func snapInitRepository(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	snapGit(t, dir, "init", "-q")
	snapGit(t, dir, "config", "user.name", "Snapshot Fixture")
	snapGit(t, dir, "config", "user.email", "snapshot-fixture@example.invalid")
	snapGit(t, dir, "config", "commit.gpgsign", "false")
}

// snapNewRepository creates an empty fixture repository and returns its
// physical root.
func snapNewRepository(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	snapInitRepository(t, root)
	return root
}

func snapCommitAll(t *testing.T, root, message string) {
	t.Helper()
	snapGit(t, root, "add", "-A")
	snapGit(t, root, "commit", "-q", "--no-verify", "--allow-empty", "-m", message)
}

// snapRealCommitTree commits the working tree with git add -A and returns the
// committed tree hash.
func snapRealCommitTree(t *testing.T, root string) string {
	t.Helper()
	snapCommitAll(t, root, "real commit of the working tree")
	return strings.TrimSpace(snapGit(t, root, "rev-parse", "HEAD^{tree}"))
}

type snapState struct {
	Head   string
	Index  string
	Status string
	Files  map[string]string
}

func snapCaptureState(t *testing.T, root string) snapState {
	t.Helper()
	return snapState{
		Head:   snapGit(t, root, "rev-parse", "HEAD"),
		Index:  snapGit(t, root, "ls-files", "--stage"),
		Status: snapGit(t, root, "status", "--porcelain=v1", "--untracked-files=all", "--ignored"),
		Files:  snapReadWorkingTree(t, root),
	}
}

func snapAssertUnchanged(t *testing.T, requirement string, before, after snapState) {
	t.Helper()
	if before.Head != after.Head {
		t.Errorf("%s: source HEAD changed from %q to %q", requirement, before.Head, after.Head)
	}
	if before.Index != after.Index {
		t.Errorf("%s: source index changed\nbefore:\n%s\nafter:\n%s", requirement, before.Index, after.Index)
	}
	if before.Status != after.Status {
		t.Errorf("%s: source status changed\nbefore:\n%s\nafter:\n%s", requirement, before.Status, after.Status)
	}
	if !reflect.DeepEqual(before.Files, after.Files) {
		t.Errorf("%s: source working tree changed\nbefore: %v\nafter:  %v", requirement, before.Files, after.Files)
	}
}

// snapReadWorkingTree describes every entry below root except the top-level
// .git directory.
func snapReadWorkingTree(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == root {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if relative == ".git" {
			return filepath.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			files[relative] = "symlink:" + target
		case info.IsDir():
			files[relative] = "dir"
		case info.Mode().IsRegular():
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[relative] = fmt.Sprintf("file:%04o:%s", info.Mode().Perm(), data)
		default:
			files[relative] = "special:" + info.Mode().String()
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

type snapTreeEntry struct {
	Mode string
	Type string
	Hash string
}

// snapReadTree lists the recursive tree of HEAD in the repository at dir.
func snapReadTree(t *testing.T, dir string) map[string]snapTreeEntry {
	t.Helper()
	entries := map[string]snapTreeEntry{}
	for _, record := range strings.Split(snapGit(t, dir, "ls-tree", "-r", "-z", "HEAD"), "\x00") {
		if record == "" {
			continue
		}
		tab := strings.IndexByte(record, '\t')
		if tab < 0 {
			t.Fatalf("unexpected ls-tree record %q", record)
		}
		fields := strings.Fields(record[:tab])
		if len(fields) != 3 {
			t.Fatalf("unexpected ls-tree record %q", record)
		}
		entries[record[tab+1:]] = snapTreeEntry{Mode: fields[0], Type: fields[1], Hash: fields[2]}
	}
	return entries
}

func snapBlob(t *testing.T, dir, hash string) string {
	t.Helper()
	return snapGit(t, dir, "cat-file", "blob", hash)
}

func snapSortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// snapAssertRepository checks the R1 repository invariants of a snapshot.
func snapAssertRepository(t *testing.T, snapshot *Snapshot) {
	t.Helper()
	if !snapFullHash.MatchString(snapshot.Tree) {
		t.Errorf("R1: Snapshot.Tree = %q, want 40 lowercase hex characters", snapshot.Tree)
	}
	if !snapFullHash.MatchString(snapshot.Commit) {
		t.Errorf("R1: Snapshot.Commit = %q, want 40 lowercase hex characters", snapshot.Commit)
	}
	physical, err := filepath.EvalSymlinks(snapshot.Dir)
	if err != nil {
		t.Fatalf("R1: Snapshot.Dir %q: %v", snapshot.Dir, err)
	}
	if physical != snapshot.Dir {
		t.Errorf("R1: Snapshot.Dir = %q is not a physical path (resolves to %q)", snapshot.Dir, physical)
	}
	if top := strings.TrimSpace(snapGit(t, snapshot.Dir, "rev-parse", "--show-toplevel")); top != snapshot.Dir {
		t.Errorf("R1: repository top level at Snapshot.Dir = %q, want %q", top, snapshot.Dir)
	}
	if head := strings.TrimSpace(snapGit(t, snapshot.Dir, "rev-parse", "HEAD")); head != snapshot.Commit {
		t.Errorf("R1: HEAD in Snapshot.Dir = %q, want Snapshot.Commit %q", head, snapshot.Commit)
	}
	if tree := strings.TrimSpace(snapGit(t, snapshot.Dir, "rev-parse", "HEAD^{tree}")); tree != snapshot.Tree {
		t.Errorf("R1: HEAD^{tree} in Snapshot.Dir = %q, want Snapshot.Tree %q", tree, snapshot.Tree)
	}
	if status := snapGit(t, snapshot.Dir, "status", "--porcelain=v1", "--untracked-files=all"); status != "" {
		t.Errorf("R1: git status --porcelain in Snapshot.Dir is not empty:\n%s", status)
	}
}

// snapEntries lists entries named cwdev-snapshot-* in os.TempDir().
func snapEntries(t *testing.T, directoriesOnly bool) []string {
	t.Helper()
	entries, err := os.ReadDir(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), snapPrefix) {
			continue
		}
		if directoriesOnly && !entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

func snapAssertNoDirectories(t *testing.T, requirement string) {
	t.Helper()
	if names := snapEntries(t, true); len(names) != 0 {
		t.Errorf("%s: cwdev-snapshot-* directories remain in os.TempDir(): %v", requirement, names)
	}
}

type snapLockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (buffer *snapLockedBuffer) Write(data []byte) (int, error) {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.Write(data)
}

func (buffer *snapLockedBuffer) String() string {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return buffer.buffer.String()
}

// R1

func TestNewSnapshotCopiesWhatGitAddAllWouldCommit(t *testing.T) {
	_, home := snapIsolate(t)
	globalIgnore := filepath.Join(home, "global-ignore")
	writeFixture(t, globalIgnore, []byte("*.tmp\n"))
	writeFixture(t, filepath.Join(home, ".gitconfig"), []byte("[core]\n\texcludesFile = "+globalIgnore+"\n"))

	root := snapNewRepository(t)
	for path, content := range map[string]string{
		".gitignore":   "*.log\nbuild/\n",
		"tracked.txt":  "tracked\n",
		"modified.txt": "committed content\n",
		"deleted.txt":  "deleted later\n",
		"sub/keep.txt": "keep\n",
	} {
		writeFixture(t, filepath.Join(root, path), []byte(content))
	}
	snapGit(t, root, "add", "-A")
	writeFixture(t, filepath.Join(root, "forced.log"), []byte("forced\n"))
	writeFixture(t, filepath.Join(root, "build", "keep.txt"), []byte("forced build file\n"))
	snapGit(t, root, "add", "-f", "forced.log", "build/keep.txt")
	snapGit(t, root, "commit", "-q", "--no-verify", "-m", "base")

	// Uncommitted changes.
	writeFixture(t, filepath.Join(root, "modified.txt"), []byte("uncommitted content\n"))
	writeFixture(t, filepath.Join(root, "forced.log"), []byte("forced, modified\n"))
	if err := os.Remove(filepath.Join(root, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "new.txt"), []byte("new\n"))
	writeFixture(t, filepath.Join(root, "newdir", "deep", "file.txt"), []byte("deep\n"))
	writeFixture(t, filepath.Join(root, "with space.txt"), []byte("space\n"))
	writeFixture(t, filepath.Join(root, "unicodé.txt"), []byte("unicode\n"))
	// Ignored, untracked.
	writeFixture(t, filepath.Join(root, "ignored.log"), []byte("ignored\n"))
	writeFixture(t, filepath.Join(root, "build", "out.bin"), []byte("ignored build output\n"))
	writeFixture(t, filepath.Join(root, "notes.tmp"), []byte("globally ignored\n"))
	writeFixture(t, filepath.Join(root, "sub", "scratch.tmp"), []byte("globally ignored\n"))

	want := map[string]string{
		".gitignore":           "*.log\nbuild/\n",
		"tracked.txt":          "tracked\n",
		"modified.txt":         "uncommitted content\n",
		"sub/keep.txt":         "keep\n",
		"forced.log":           "forced, modified\n",
		"build/keep.txt":       "forced build file\n",
		"new.txt":              "new\n",
		"newdir/deep/file.txt": "deep\n",
		"with space.txt":       "space\n",
		"unicodé.txt":          "unicode\n",
	}

	before := snapCaptureState(t, root)
	snapshot, err := NewSnapshot(t.Context(), filepath.Join(root, "sub"))
	if err != nil {
		t.Fatalf("R1: NewSnapshot from a subdirectory: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })

	snapAssertRepository(t, snapshot)
	tree := snapReadTree(t, snapshot.Dir)
	if got, wantPaths := snapSortedKeys(tree), snapSortedKeys(want); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("R1: snapshot commit paths = %q, want %q", got, wantPaths)
	}
	for path, content := range want {
		entry, ok := tree[path]
		if !ok {
			continue
		}
		if entry.Type != "blob" || entry.Mode != "100644" {
			t.Errorf("R1: %s recorded as %s %s, want blob 100644", path, entry.Mode, entry.Type)
			continue
		}
		if got := snapBlob(t, snapshot.Dir, entry.Hash); got != content {
			t.Errorf("R1: %s content in snapshot = %q, want on-disk %q", path, got, content)
		}
	}
	if snapshot.Files != len(want) {
		t.Errorf("R1: Snapshot.Files = %d, want %d", snapshot.Files, len(want))
	}
	snapAssertUnchanged(t, "R1 (after NewSnapshot)", before, snapCaptureState(t, root))

	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	snapAssertUnchanged(t, "R1 (after Remove)", before, snapCaptureState(t, root))
}

// R5

func TestNewSnapshotRefusals(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) (context.Context, string)
	}{
		{
			name: "source outside a Git working tree",
			setup: func(t *testing.T) (context.Context, string) {
				dir, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(dir, "file.txt"), []byte("not in git\n"))
				return t.Context(), dir
			},
		},
		{
			name: "unmerged index paths",
			setup: func(t *testing.T) (context.Context, string) {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "conflict.txt"), []byte("base\n"))
				snapCommitAll(t, root, "base")
				snapGit(t, root, "checkout", "-q", "-b", "other")
				writeFixture(t, filepath.Join(root, "conflict.txt"), []byte("other side\n"))
				snapCommitAll(t, root, "other")
				snapGit(t, root, "checkout", "-q", "-")
				writeFixture(t, filepath.Join(root, "conflict.txt"), []byte("this side\n"))
				snapCommitAll(t, root, "this")
				if _, _, err := snapRunGit(root, "merge", "--no-edit", "other"); err == nil {
					t.Fatal("test setup: merge unexpectedly succeeded")
				}
				if unmerged := snapGit(t, root, "ls-files", "-u"); unmerged == "" {
					t.Fatal("test setup: no unmerged paths after conflicting merge")
				}
				return t.Context(), root
			},
		},
		{
			name: "submodule gitlink entry",
			setup: func(t *testing.T) (context.Context, string) {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "top.txt"), []byte("top\n"))
				snapCommitAll(t, root, "base")
				module := filepath.Join(root, "module")
				snapInitRepository(t, module)
				writeFixture(t, filepath.Join(module, "inner.txt"), []byte("inner\n"))
				snapCommitAll(t, module, "inner")
				snapGit(t, root, "-c", "advice.addEmbeddedRepo=false", "add", "module")
				snapGit(t, root, "commit", "-q", "--no-verify", "-m", "add gitlink")
				if entry := snapGit(t, root, "ls-files", "--stage", "module"); !strings.HasPrefix(entry, "160000 ") {
					t.Fatalf("test setup: module is not a gitlink: %q", entry)
				}
				return t.Context(), root
			},
		},
		{
			name: "untracked nested Git repository",
			setup: func(t *testing.T) (context.Context, string) {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "top.txt"), []byte("top\n"))
				snapCommitAll(t, root, "base")
				nested := filepath.Join(root, "nested")
				snapInitRepository(t, nested)
				writeFixture(t, filepath.Join(nested, "inner.txt"), []byte("inner\n"))
				snapCommitAll(t, nested, "inner")
				if status := snapGit(t, root, "status", "--porcelain=v1"); !strings.Contains(status, "?? nested/") {
					t.Fatalf("test setup: nested repository is not untracked: %q", status)
				}
				return t.Context(), root
			},
		},
		{
			name: "cancelled context",
			setup: func(t *testing.T) (context.Context, string) {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
				snapCommitAll(t, root, "base")
				writeFixture(t, filepath.Join(root, "file.txt"), []byte("changed\n"))
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				return ctx, root
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapIsolate(t)
			ctx, source := testCase.setup(t)
			snapshot, err := NewSnapshot(ctx, source)
			if err == nil {
				_ = snapshot.Remove()
				t.Fatalf("R5: NewSnapshot(%s) succeeded, want an error", testCase.name)
			}
			snapAssertNoDirectories(t, "R5 ("+testCase.name+")")
		})
	}
}

// R6

func TestSnapshotRemoveDeletesCopyAndIsIdempotent(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
	snapCommitAll(t, root, "base")

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	if names := snapEntries(t, true); len(names) != 1 {
		t.Errorf("R7: want exactly one cwdev-snapshot-* directory while the snapshot exists, got %v", names)
	}
	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	if _, err := os.Lstat(snapshot.Dir); !os.IsNotExist(err) {
		t.Errorf("R6: Snapshot.Dir %q still exists after Remove (Lstat error %v)", snapshot.Dir, err)
	}
	if names := snapEntries(t, false); len(names) != 0 {
		t.Errorf("R6: cwdev-snapshot-* entries remain after Remove: %v", names)
	}
	if err := snapshot.Remove(); err != nil {
		t.Errorf("R6: second Remove = %v, want nil", err)
	}
}

// R7 (concurrent runs)

func TestConcurrentSnapshotsOfOneSource(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "a.txt"), []byte("committed\n"))
	snapCommitAll(t, root, "base")
	writeFixture(t, filepath.Join(root, "a.txt"), []byte("modified\n"))
	writeFixture(t, filepath.Join(root, "b.txt"), []byte("untracked\n"))

	const workers = 4
	snapshots := make([]*Snapshot, workers)
	errs := make([]error, workers)
	start := make(chan struct{})
	var group sync.WaitGroup
	for index := range workers {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			snapshots[index], errs[index] = NewSnapshot(t.Context(), root)
		}()
	}
	close(start)
	group.Wait()
	t.Cleanup(func() {
		for _, snapshot := range snapshots {
			if snapshot != nil {
				_ = snapshot.Remove()
			}
		}
	})
	for index, err := range errs {
		if err != nil {
			t.Errorf("R7: concurrent NewSnapshot %d: %v", index, err)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	directories := map[string]bool{}
	for index, snapshot := range snapshots {
		if directories[snapshot.Dir] {
			t.Errorf("R7: concurrent snapshots share Dir %q", snapshot.Dir)
		}
		directories[snapshot.Dir] = true
		if snapshot.Tree != snapshots[0].Tree {
			t.Errorf("R7: snapshot %d Tree = %q, want %q (same as snapshot 0)", index, snapshot.Tree, snapshots[0].Tree)
		}
	}

	if err := snapshots[0].Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	if _, err := os.Lstat(snapshots[0].Dir); !os.IsNotExist(err) {
		t.Errorf("R6: removed snapshot directory still exists (Lstat error %v)", err)
	}
	for index, snapshot := range snapshots[1:] {
		if _, err := os.Lstat(snapshot.Dir); err != nil {
			t.Errorf("R7: removing one snapshot affected snapshot %d: %v", index+1, err)
			continue
		}
		if tree := strings.TrimSpace(snapGit(t, snapshot.Dir, "rev-parse", "HEAD^{tree}")); tree != snapshot.Tree {
			t.Errorf("R7: snapshot %d HEAD^{tree} = %q after another was removed, want %q", index+1, tree, snapshot.Tree)
		}
	}
	for index, snapshot := range snapshots[1:] {
		if err := snapshot.Remove(); err != nil {
			t.Errorf("R6: Remove snapshot %d: %v", index+1, err)
		}
	}
	snapAssertNoDirectories(t, "R6/R7 (after removing all concurrent snapshots)")
}

// R8

func snapPreserveGoEnvironment(t *testing.T) {
	t.Helper()
	// Keep Go's caches where they would be for the real user, so changing HOME
	// neither rebuilds them nor leaves read-only module files in a temp HOME.
	if os.Getenv("GOCACHE") == "" {
		if cache, err := os.UserCacheDir(); err == nil {
			t.Setenv("GOCACHE", filepath.Join(cache, "go-build"))
		}
	}
	if os.Getenv("GOPATH") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			t.Setenv("GOPATH", filepath.Join(home, "go"))
		}
	}
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOTOOLCHAIN", "local")
}

func TestCheckSnapshotRejectsReleaseOptions(t *testing.T) {
	cases := []struct {
		name    string
		options Options
	}{
		{name: "release version", options: Options{ReleaseVersion: "v1.0.1"}},
		{name: "revision", options: Options{Revision: strings.Repeat("0a1b2c3d4e", 4)}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapPreserveGoEnvironment(t)
			snapIsolate(t)
			root := snapNewRepository(t)
			writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
			snapCommitAll(t, root, "base")
			writeFixture(t, filepath.Join(root, "file.txt"), []byte("changed\n"))
			before := snapCaptureState(t, root)

			var output snapLockedBuffer
			options := testCase.options
			options.Source = root
			options.Binary = filepath.Join(t.TempDir(), "cw")
			options.Snapshot = true
			options.Output = &output
			if err := Check(t.Context(), options); err == nil {
				t.Fatalf("R8: Check with Snapshot and %s returned nil; output:\n%s", testCase.name, output.String())
			}
			if text := output.String(); strings.Contains(text, "Snapshot tree:") {
				t.Errorf("R8: Check created a snapshot despite %s; output:\n%s", testCase.name, text)
			}
			snapAssertNoDirectories(t, "R8 ("+testCase.name+")")
			snapAssertUnchanged(t, "R8 ("+testCase.name+")", before, snapCaptureState(t, root))
		})
	}
}

func TestCheckSnapshotReportsTreeAndRemovesCopyWhenGatesFail(t *testing.T) {
	snapPreserveGoEnvironment(t)
	snapIsolate(t)
	root := snapNewRepository(t)
	for path, content := range map[string]string{
		".gitignore": "*.log\n",
		"README.md":  "committed readme\n",
		"lib/a.txt":  "library\n",
		"gone.txt":   "to be deleted\n",
	} {
		writeFixture(t, filepath.Join(root, path), []byte(content))
	}
	snapCommitAll(t, root, "base")
	writeFixture(t, filepath.Join(root, "README.md"), []byte("modified readme\n"))
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(root, "notes.txt"), []byte("untracked notes\n"))
	writeFixture(t, filepath.Join(root, "debug.log"), []byte("ignored\n"))
	const wantFiles = "4" // .gitignore, README.md, lib/a.txt, notes.txt

	before := snapCaptureState(t, root)
	var output snapLockedBuffer
	err := Check(t.Context(), Options{
		Source:   root,
		Binary:   filepath.Join(t.TempDir(), "cw"),
		Snapshot: true,
		Output:   &output,
	})
	text := output.String()
	if err == nil {
		t.Fatalf("R8: Check succeeded on a fixture repository that is not this project; output:\n%s", text)
	}
	match := snapTreeLine.FindStringSubmatch(text)
	if match == nil {
		t.Fatalf("R8: output lacks a line %q; output:\n%s", "Snapshot tree: <hash> (<N> files from <source>)", text)
	}
	if match[2] != wantFiles {
		t.Errorf("R8: snapshot line reports %s files, want %s", match[2], wantFiles)
	}
	if match[3] != root {
		t.Errorf("R8: snapshot line reports source %q, want %q", match[3], root)
	}
	if strings.Contains(text, "Verified snapshot tree:") {
		t.Errorf("R8: failed Check printed a verified snapshot line; output:\n%s", text)
	}
	snapAssertNoDirectories(t, "R8 (after failed Check)")
	snapAssertUnchanged(t, "R8 (after failed Check)", before, snapCaptureState(t, root))

	if committed := snapRealCommitTree(t, root); match[1] != committed {
		t.Errorf("R8/R2: reported snapshot tree %s, want the tree of git add -A && git commit %s", match[1], committed)
	}
}

// Revision 3 additions.

// snapGitWith runs git like snapGit but adds extra environment entries, for
// example a GIT_CONFIG_GLOBAL the user's own commands would see.
func snapGitWith(t *testing.T, dir string, extra []string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(snapGitEnvironment(), extra...)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("git -C %s %s (with %v): %v\n%s%s", dir, strings.Join(args, " "), extra, err, stdout.String(), stderr.String())
	}
	return stdout.String()
}

// R3 (environment-supplied global configuration)

func TestNewSnapshotHonoursGitConfigGlobalExcludes(t *testing.T) {
	_, home := snapIsolate(t)
	// $HOME/.gitconfig excludes *.home; GIT_CONFIG_GLOBAL names a different file
	// that excludes *.envignored. Git reads GIT_CONFIG_GLOBAL instead of
	// $HOME/.gitconfig, so only *.envignored is excluded for the user's git add -A.
	homeIgnore := filepath.Join(home, "home-ignore")
	writeFixture(t, homeIgnore, []byte("*.home\n"))
	writeFixture(t, filepath.Join(home, ".gitconfig"), []byte("[core]\n\texcludesFile = "+homeIgnore+"\n"))
	environmentIgnore := filepath.Join(home, "environment-ignore")
	writeFixture(t, environmentIgnore, []byte("*.envignored\n"))
	environmentConfig := filepath.Join(home, "environment-global.gitconfig")
	writeFixture(t, environmentConfig, []byte("[core]\n\texcludesFile = "+environmentIgnore+"\n"))

	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "tracked.txt"), []byte("tracked\n"))
	snapCommitAll(t, root, "base")
	writeFixture(t, filepath.Join(root, "untracked.txt"), []byte("untracked\n"))
	writeFixture(t, filepath.Join(root, "excluded.envignored"), []byte("excluded by GIT_CONFIG_GLOBAL\n"))
	writeFixture(t, filepath.Join(root, "deep", "excluded.envignored"), []byte("excluded by GIT_CONFIG_GLOBAL\n"))
	writeFixture(t, filepath.Join(root, "included.home"), []byte("only $HOME/.gitconfig excludes this\n"))

	want := map[string]string{
		"tracked.txt":   "tracked\n",
		"untracked.txt": "untracked\n",
		"included.home": "only $HOME/.gitconfig excludes this\n",
	}
	extra := []string{"GIT_CONFIG_GLOBAL=" + environmentConfig}
	t.Setenv("GIT_CONFIG_GLOBAL", environmentConfig)

	before := snapCaptureState(t, root)
	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("R3: NewSnapshot with GIT_CONFIG_GLOBAL set: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	snapAssertRepository(t, snapshot)

	tree := snapReadTree(t, snapshot.Dir)
	if got, wantPaths := snapSortedKeys(tree), snapSortedKeys(want); !reflect.DeepEqual(got, wantPaths) {
		t.Errorf("R3: snapshot paths with GIT_CONFIG_GLOBAL excludes = %q, want %q", got, wantPaths)
	}
	for path, content := range want {
		if entry, ok := tree[path]; ok {
			if got := snapBlob(t, snapshot.Dir, entry.Hash); got != content {
				t.Errorf("R3: %s content = %q, want %q", path, got, content)
			}
		}
	}
	if snapshot.Files != len(want) {
		t.Errorf("R1/R3: Snapshot.Files = %d, want %d", snapshot.Files, len(want))
	}
	tree0 := snapshot.Tree
	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	snapAssertUnchanged(t, "R1 (R3 GIT_CONFIG_GLOBAL fixture)", before, snapCaptureState(t, root))

	snapGitWith(t, root, extra, "add", "-A")
	snapGitWith(t, root, extra, "commit", "-q", "--no-verify", "-m", "user's own git add -A")
	if committed := strings.TrimSpace(snapGitWith(t, root, extra, "rev-parse", "HEAD^{tree}")); tree0 != committed {
		t.Errorf("R3: Snapshot.Tree = %s, want %s from the user's own git add -A with GIT_CONFIG_GLOBAL", tree0, committed)
	}
}

// R5 (skip-worktree and assume-unchanged index entries)

func TestNewSnapshotRefusesHiddenIndexEntries(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) string
	}{
		{
			name: "skip-worktree entry from sparse checkout",
			setup: func(t *testing.T) string {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "a", "x.txt"), []byte("inside the sparse cone\n"))
				writeFixture(t, filepath.Join(root, "b", "y.txt"), []byte("outside the sparse cone\n"))
				writeFixture(t, filepath.Join(root, "top.txt"), []byte("top level\n"))
				snapCommitAll(t, root, "base")
				snapGit(t, root, "sparse-checkout", "set", "--no-cone", "/a/")
				if listing := snapGit(t, root, "ls-files", "-t"); !strings.Contains(listing, "S b/y.txt") {
					t.Fatalf("test setup: b/y.txt is not skip-worktree:\n%s", listing)
				}
				return root
			},
		},
		{
			name: "assume-unchanged entry",
			setup: func(t *testing.T) string {
				root := snapNewRepository(t)
				writeFixture(t, filepath.Join(root, "file.txt"), []byte("committed\n"))
				writeFixture(t, filepath.Join(root, "other.txt"), []byte("other\n"))
				snapCommitAll(t, root, "base")
				snapGit(t, root, "update-index", "--assume-unchanged", "file.txt")
				writeFixture(t, filepath.Join(root, "file.txt"), []byte("modified while assumed unchanged\n"))
				if listing := snapGit(t, root, "ls-files", "-v", "file.txt"); !strings.HasPrefix(listing, "h ") {
					t.Fatalf("test setup: file.txt is not assume-unchanged: %q", listing)
				}
				return root
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapIsolate(t)
			root := testCase.setup(t)
			snapshot, err := NewSnapshot(t.Context(), root)
			if err == nil {
				_ = snapshot.Remove()
				t.Fatalf("R5: NewSnapshot with a %s succeeded, want an error", testCase.name)
			}
			snapAssertNoDirectories(t, "R5 ("+testCase.name+")")
		})
	}
}
