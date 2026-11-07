//go:build darwin || linux

package devcheck

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Unix-only tests for contract wp10-snapshot revision 1.

func snapSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func snapChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

// snapLockHeld reports whether some other open file description holds a flock
// on path.
func snapLockHeld(t *testing.T, path string) bool {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return false
	}
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true
	}
	t.Fatalf("flock %s: %v", path, err)
	return false
}

// R2

func TestSnapshotTreeEqualsRealCommitTree(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "a.txt"), []byte("a committed\n"))
	writeFixture(t, filepath.Join(root, "b.txt"), []byte("b\n"))
	writeFixture(t, filepath.Join(root, "gone.txt"), []byte("gone\n"))
	writeFixture(t, filepath.Join(root, "script.sh"), []byte("#!/bin/sh\necho script\n"))
	writeFixture(t, filepath.Join(root, "was-exec.sh"), []byte("#!/bin/sh\necho was\n"))
	snapChmod(t, filepath.Join(root, "was-exec.sh"), 0o755)
	snapSymlink(t, "a.txt", filepath.Join(root, "rel-link"))
	snapCommitAll(t, root, "base")

	writeFixture(t, filepath.Join(root, "a.txt"), []byte("a modified\n"))
	if err := os.Remove(filepath.Join(root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	snapChmod(t, filepath.Join(root, "script.sh"), 0o755)
	snapChmod(t, filepath.Join(root, "was-exec.sh"), 0o644)
	writeFixture(t, filepath.Join(root, "c.txt"), []byte("c added\n"))
	writeFixture(t, filepath.Join(root, "nested", "x.txt"), []byte("nested added\n"))
	writeFixture(t, filepath.Join(root, "tool.sh"), []byte("#!/bin/sh\necho tool\n"))
	snapChmod(t, filepath.Join(root, "tool.sh"), 0o755)
	if err := os.Remove(filepath.Join(root, "rel-link")); err != nil {
		t.Fatal(err)
	}
	snapSymlink(t, "b.txt", filepath.Join(root, "rel-link"))
	snapSymlink(t, "/nonexistent/cwdev-snapshot-test/absolute-target", filepath.Join(root, "abs-link"))
	snapSymlink(t, "missing/target", filepath.Join(root, "dangling-link"))

	before := snapCaptureState(t, root)
	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("R2: NewSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	snapAssertRepository(t, snapshot)
	tree := snapshot.Tree
	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	snapAssertUnchanged(t, "R1 (R2 fixture)", before, snapCaptureState(t, root))

	if committed := snapRealCommitTree(t, root); tree != committed {
		t.Errorf("R2: Snapshot.Tree = %s, want %s from git add -A && git commit", tree, committed)
	}
}

// R3

func TestNewSnapshotUsesFixedIdentityAndIgnoresUserConfiguration(t *testing.T) {
	_, home := snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "real.txt"), []byte("committed\n"))
	snapCommitAll(t, root, "base")
	writeFixture(t, filepath.Join(root, "real.txt"), []byte("uncommitted\n"))
	writeFixture(t, filepath.Join(root, "added.txt"), []byte("added\n"))

	decoy := snapNewRepository(t)
	writeFixture(t, filepath.Join(decoy, "decoy.txt"), []byte("decoy\n"))
	snapCommitAll(t, decoy, "decoy")
	writeFixture(t, filepath.Join(decoy, "decoy-untracked.txt"), []byte("decoy untracked\n"))

	before := snapCaptureState(t, root)
	decoyBefore := snapCaptureState(t, decoy)

	failing := []byte("#!/bin/sh\nexit 1\n")
	gpg := filepath.Join(home, "bin", "failing-gpg")
	writeFixture(t, gpg, failing)
	snapChmod(t, gpg, 0o755)
	hooks := filepath.Join(home, "hooks")
	for _, hook := range []string{"pre-commit", "commit-msg"} {
		writeFixture(t, filepath.Join(hooks, hook), failing)
		snapChmod(t, filepath.Join(hooks, hook), 0o755)
	}
	writeFixture(t, filepath.Join(home, ".gitconfig"), []byte(
		"[user]\n\tname = Config User\n\temail = config-user@example.invalid\n"+
			"[commit]\n\tgpgsign = true\n"+
			"[gpg]\n\tprogram = "+gpg+"\n"+
			"[core]\n\thooksPath = "+hooks+"\n"))

	t.Setenv("GIT_AUTHOR_NAME", "Environment Author")
	t.Setenv("GIT_AUTHOR_EMAIL", "environment-author@example.invalid")
	t.Setenv("GIT_COMMITTER_NAME", "Environment Committer")
	t.Setenv("GIT_COMMITTER_EMAIL", "environment-committer@example.invalid")
	t.Setenv("GIT_DIR", filepath.Join(decoy, ".git"))
	t.Setenv("GIT_WORK_TREE", decoy)
	t.Setenv("GIT_INDEX_FILE", filepath.Join(decoy, ".git", "index"))

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("R3: NewSnapshot with hostile user configuration and environment: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })

	identity := snapGit(t, snapshot.Dir, "log", "-1", "--format=%an <%ae>%n%cn <%ce>")
	if want := snapIdentity + "\n" + snapIdentity + "\n"; identity != want {
		t.Errorf("R3: snapshot author/committer = %q, want %q", identity, want)
	}
	tree := snapReadTree(t, snapshot.Dir)
	if _, ok := tree["decoy.txt"]; ok {
		t.Errorf("R3: snapshot contains the GIT_DIR repository's decoy.txt")
	}
	for path, content := range map[string]string{"real.txt": "uncommitted\n", "added.txt": "added\n"} {
		entry, ok := tree[path]
		if !ok {
			t.Errorf("R3: snapshot lacks %s from the source repository; paths %v", path, snapSortedKeys(tree))
			continue
		}
		if got := snapBlob(t, snapshot.Dir, entry.Hash); got != content {
			t.Errorf("R3: %s content = %q, want %q", path, got, content)
		}
	}
	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	snapAssertUnchanged(t, "R3 (source)", before, snapCaptureState(t, root))
	snapAssertUnchanged(t, "R3 (GIT_DIR repository)", decoyBefore, snapCaptureState(t, decoy))
}

// R4

func TestNewSnapshotRecordsModesAndSymlinks(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "plain.txt"), []byte("plain\n"))
	writeFixture(t, filepath.Join(root, "sub", "inner.txt"), []byte("inner\n"))
	snapSymlink(t, "plain.txt", filepath.Join(root, "rel-link"))
	snapCommitAll(t, root, "base")

	type expectation struct {
		mode    string
		content string // blob content, or link target for 120000
	}
	want := map[string]expectation{
		"plain.txt":          {"100644", "plain\n"},
		"sub/inner.txt":      {"100644", "inner\n"},
		"exec.sh":            {"100755", "#!/bin/sh\n"},
		"owner-only-exec.sh": {"100755", "#!/bin/sh\necho owner\n"},
		"group-exec.sh":      {"100644", "#!/bin/sh\necho group\n"},
		"rel-link":           {"120000", "plain.txt"},
		"abs-link":           {"120000", "/nonexistent/cwdev-snapshot-test/target"},
		"dangling-link":      {"120000", "missing/target"},
		"dir-link":           {"120000", "sub"},
	}
	for path, mode := range map[string]os.FileMode{"exec.sh": 0o755, "owner-only-exec.sh": 0o744, "group-exec.sh": 0o655} {
		writeFixture(t, filepath.Join(root, path), []byte(want[path].content))
		snapChmod(t, filepath.Join(root, path), mode)
	}
	for _, path := range []string{"abs-link", "dangling-link", "dir-link"} {
		snapSymlink(t, want[path].content, filepath.Join(root, path))
	}

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("R4: NewSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	snapAssertRepository(t, snapshot)

	tree := snapReadTree(t, snapshot.Dir)
	if got, wantPaths := snapSortedKeys(tree), snapSortedKeys(want); strings.Join(got, "\n") != strings.Join(wantPaths, "\n") {
		t.Errorf("R4: snapshot paths = %q, want %q", got, wantPaths)
	}
	for path, expected := range want {
		entry, ok := tree[path]
		if !ok {
			continue
		}
		if entry.Mode != expected.mode {
			t.Errorf("R4: %s recorded with mode %s, want %s", path, entry.Mode, expected.mode)
		}
		if got := snapBlob(t, snapshot.Dir, entry.Hash); got != expected.content {
			t.Errorf("R4: %s recorded content/target %q, want %q", path, got, expected.content)
		}
		if expected.mode != "120000" {
			continue
		}
		copied := filepath.Join(snapshot.Dir, path)
		info, err := os.Lstat(copied)
		if err != nil {
			t.Errorf("R4: symlink %s missing from Snapshot.Dir: %v", path, err)
			continue
		}
		if info.Mode()&os.ModeSymlink == 0 {
			t.Errorf("R4: %s in Snapshot.Dir is %v, want a symbolic link", path, info.Mode())
			continue
		}
		if target, err := os.Readlink(copied); err != nil || target != expected.content {
			t.Errorf("R4: %s in Snapshot.Dir points to %q (%v), want %q", path, target, err, expected.content)
		}
	}
	if snapshot.Files != len(want) {
		t.Errorf("R1: Snapshot.Files = %d, want %d", snapshot.Files, len(want))
	}
}

// R5 (special files)

// snapNewSnapshotGuarded runs NewSnapshot and fails the test instead of hanging
// when the implementation blocks, for example by opening the FIFO at fifo.
func snapNewSnapshotGuarded(t *testing.T, root, fifo string) (*Snapshot, error) {
	t.Helper()
	type result struct {
		snapshot *Snapshot
		err      error
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan result, 1)
	go func() {
		snapshot, err := NewSnapshot(ctx, root)
		done <- result{snapshot, err}
	}()
	select {
	case outcome := <-done:
		return outcome.snapshot, outcome.err
	case <-time.After(30 * time.Second):
		cancel()
		// Release a reader blocked opening the FIFO.
		if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			_ = writer.Close()
		}
		t.Fatal("R5: NewSnapshot did not return within 30s with a FIFO in the working tree (is it reading the FIFO?)")
		return nil, nil
	}
}

func TestNewSnapshotRefusesTrackedPathReplacedBySpecialFile(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
	writeFixture(t, filepath.Join(root, "replaced.txt"), []byte("tracked, then replaced by a FIFO\n"))
	snapCommitAll(t, root, "base")
	fifo := filepath.Join(root, "replaced.txt")
	if err := os.Remove(fifo); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := snapNewSnapshotGuarded(t, root, fifo)
	if err == nil {
		_ = snapshot.Remove()
		t.Fatal("R5: NewSnapshot succeeded with a tracked path replaced by a FIFO, want an error")
	}
	snapAssertNoDirectories(t, "R5 (tracked path replaced by a special file)")
}

func TestNewSnapshotExcludesUntrackedSpecialFile(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("committed\n"))
	snapCommitAll(t, root, "base")
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("modified\n"))
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	snapshot, err := snapNewSnapshotGuarded(t, root, fifo)
	if err != nil {
		t.Fatalf("R5: NewSnapshot with an untracked FIFO: %v, want success with the FIFO excluded", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	snapAssertRepository(t, snapshot)

	tree := snapReadTree(t, snapshot.Dir)
	if got := snapSortedKeys(tree); strings.Join(got, "\n") != "file.txt" {
		t.Errorf("R5: snapshot paths = %q, want only file.txt (untracked FIFO excluded)", got)
	}
	if entry, ok := tree["file.txt"]; ok {
		if got := snapBlob(t, snapshot.Dir, entry.Hash); got != "modified\n" {
			t.Errorf("R1: file.txt content = %q, want %q", got, "modified\n")
		}
	}
	if snapshot.Files != 1 {
		t.Errorf("R5/R1: Snapshot.Files = %d, want 1 (untracked FIFO not copied)", snapshot.Files)
	}
	if _, err := os.Lstat(filepath.Join(snapshot.Dir, "pipe")); !os.IsNotExist(err) {
		t.Errorf("R5: untracked FIFO was copied into Snapshot.Dir (Lstat error %v)", err)
	}

	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	snapAssertNoDirectories(t, "R5 (untracked special file)")
}

// R6 (read-only directory)

func TestSnapshotRemoveHandlesReadOnlyDirectory(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
	snapCommitAll(t, root, "base")

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })
	readOnly := filepath.Join(snapshot.Dir, "left-behind")
	writeFixture(t, filepath.Join(readOnly, "file.txt"), []byte("stuck\n"))
	t.Cleanup(func() { _ = os.Chmod(readOnly, 0o755) })
	snapChmod(t, readOnly, 0o500)

	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove with a read-only directory inside Snapshot.Dir: %v", err)
	}
	if _, err := os.Lstat(snapshot.Dir); !os.IsNotExist(err) {
		t.Errorf("R6: Snapshot.Dir still exists after Remove (Lstat error %v)", err)
	}
	if names := snapEntries(t, false); len(names) != 0 {
		t.Errorf("R6: cwdev-snapshot-* entries remain after Remove: %v", names)
	}
}

// R7 (layout and lock)

func TestSnapshotLayoutAndOwnerLock(t *testing.T) {
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
	snapCommitAll(t, root, "base")

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("NewSnapshot: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })

	if base := filepath.Base(snapshot.Dir); base != "source" {
		t.Errorf("R7: Snapshot.Dir ends in %q, want source", base)
	}
	parent := filepath.Dir(snapshot.Dir)
	if !strings.HasPrefix(filepath.Base(parent), snapPrefix) {
		t.Errorf("R7: Snapshot.Dir parent %q is not named cwdev-snapshot-*", parent)
	}
	physicalTemp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(parent) != physicalTemp {
		t.Errorf("R7: snapshot directory %q is not directly under os.TempDir() %q", parent, physicalTemp)
	}
	lock := filepath.Join(parent, "owner.lock")
	info, err := os.Lstat(lock)
	if err != nil {
		t.Fatalf("R7: owner.lock: %v", err)
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		t.Errorf("R7: owner.lock mode %v size %d, want a non-empty regular file", info.Mode(), info.Size())
	}
	if !snapLockHeld(t, lock) {
		t.Errorf("R7: owner.lock is not exclusively locked while the snapshot exists")
	}

	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	if _, err := os.Lstat(parent); !os.IsNotExist(err) {
		t.Errorf("R6: snapshot directory %q remains after Remove (Lstat error %v)", parent, err)
	}
}

// R7 (reclaiming copies of dead runs)

func TestNewSnapshotReclaimsOnlyAbandonedSnapshotDirectories(t *testing.T) {
	tmp, _ := snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "file.txt"), []byte("content\n"))
	snapCommitAll(t, root, "base")

	old := time.Now().Add(-2 * time.Hour)
	text := func(value string) *string { return &value }
	// makeDirectory creates <tmp>/<name>/source/file.txt and, unless lock is nil,
	// <tmp>/<name>/owner.lock; a non-zero mtime is applied last.
	makeDirectory := func(name string, lock *string, mtime time.Time) string {
		dir := filepath.Join(tmp, name)
		writeFixture(t, filepath.Join(dir, "source", "file.txt"), []byte("copy\n"))
		if lock != nil {
			writeFixture(t, filepath.Join(dir, "owner.lock"), []byte(*lock))
		}
		if !mtime.IsZero() {
			paths := []string{filepath.Join(dir, "source")}
			if lock != nil {
				paths = append(paths, filepath.Join(dir, "owner.lock"))
			}
			paths = append(paths, dir)
			for _, path := range paths {
				if err := os.Chtimes(path, mtime, mtime); err != nil {
					t.Fatal(err)
				}
			}
		}
		return dir
	}

	dead := makeDirectory(snapPrefix+"dead-owner", text("4242\n"), time.Time{})
	live := makeDirectory(snapPrefix+"live-owner", text("4243\n"), old)
	freshEmpty := makeDirectory(snapPrefix+"fresh-empty-lock", text(""), time.Time{})
	freshMissing := makeDirectory(snapPrefix+"fresh-missing-lock", nil, time.Time{})
	oldEmpty := makeDirectory(snapPrefix+"old-empty-lock", text(""), old)
	oldMissing := makeDirectory(snapPrefix+"old-missing-lock", nil, old)
	otherName := makeDirectory("other-snapshot-dead-owner", text("4244\n"), old)
	plainFile := filepath.Join(tmp, snapPrefix+"plain-file")
	writeFixture(t, plainFile, []byte("not a directory\n"))
	if err := os.Chtimes(plainFile, old, old); err != nil {
		t.Fatal(err)
	}

	holder, err := os.Open(filepath.Join(live, "owner.lock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = holder.Close() })
	if err := syscall.Flock(int(holder.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatalf("test setup: lock live owner: %v", err)
	}
	if err := os.Chtimes(live, old, old); err != nil {
		t.Fatal(err)
	}

	snapshot, err := NewSnapshot(t.Context(), root)
	if err != nil {
		t.Fatalf("R7: NewSnapshot with leftover snapshot directories: %v", err)
	}
	t.Cleanup(func() { _ = snapshot.Remove() })

	for name, dir := range map[string]string{
		"non-empty unlocked owner.lock (dead owner)": dead,
		"empty owner.lock older than one hour":       oldEmpty,
		"missing owner.lock older than one hour":     oldMissing,
	} {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) {
			t.Errorf("R7: %s: directory %s was not reclaimed (Lstat error %v)", name, filepath.Base(dir), err)
		}
	}
	assertKept := func(stage string) {
		t.Helper()
		for name, dir := range map[string]string{
			"exclusively locked owner.lock (live run)": live,
			"empty owner.lock younger than one hour":   freshEmpty,
			"missing owner.lock younger than one hour": freshMissing,
			"name without the cwdev-snapshot- prefix":  otherName,
		} {
			if _, err := os.Stat(filepath.Join(dir, "source", "file.txt")); err != nil {
				t.Errorf("R7 (%s): %s: directory %s was not kept intact: %v", stage, name, filepath.Base(dir), err)
			}
		}
		if data, err := os.ReadFile(plainFile); err != nil || string(data) != "not a directory\n" {
			t.Errorf("R7 (%s): plain file with the cwdev-snapshot- prefix was touched: %q, %v", stage, data, err)
		}
		if data, err := os.ReadFile(filepath.Join(live, "owner.lock")); err != nil || string(data) != "4243\n" {
			t.Errorf("R7 (%s): live owner's owner.lock changed: %q, %v", stage, data, err)
		}
	}
	assertKept("after NewSnapshot")

	if err := snapshot.Remove(); err != nil {
		t.Fatalf("R6: Remove: %v", err)
	}
	assertKept("after Remove")
}

// R1/R2 (revision 3): tracked files behind a leading directory that is no
// longer a real directory are deletions.

func TestNewSnapshotTreatsPathsBehindReplacedDirectoryAsDeleted(t *testing.T) {
	type expectation struct {
		mode    string
		content string // blob content, or link target for 120000
	}
	cases := []struct {
		name string
		// setup builds and commits the fixture, replaces dir/ and returns the
		// expected snapshot contents.
		setup func(t *testing.T, root string) map[string]expectation
	}{
		{
			name: "ignored symlink to an outside directory with same-named files",
			setup: func(t *testing.T, root string) map[string]expectation {
				writeFixture(t, filepath.Join(root, "dir", "a.txt"), []byte("tracked a\n"))
				writeFixture(t, filepath.Join(root, "dir", "sub", "b.txt"), []byte("tracked b\n"))
				writeFixture(t, filepath.Join(root, "keep.txt"), []byte("keep\n"))
				snapCommitAll(t, root, "base")
				writeFixture(t, filepath.Join(root, ".gitignore"), []byte("/dir\n"))
				snapCommitAll(t, root, "ignore dir")
				outside, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(outside, "a.txt"), []byte("outside a, must not be copied\n"))
				writeFixture(t, filepath.Join(outside, "sub", "b.txt"), []byte("outside b, must not be copied\n"))
				if err := os.RemoveAll(filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
				snapSymlink(t, outside, filepath.Join(root, "dir"))
				return map[string]expectation{
					".gitignore": {"100644", "/dir\n"},
					"keep.txt":   {"100644", "keep\n"},
				}
			},
		},
		{
			name: "non-ignored symlink to an inside directory",
			setup: func(t *testing.T, root string) map[string]expectation {
				writeFixture(t, filepath.Join(root, "dir", "a.txt"), []byte("tracked a\n"))
				writeFixture(t, filepath.Join(root, "other", "a.txt"), []byte("other a\n"))
				writeFixture(t, filepath.Join(root, "keep.txt"), []byte("keep\n"))
				snapCommitAll(t, root, "base")
				if err := os.RemoveAll(filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
				snapSymlink(t, "other", filepath.Join(root, "dir"))
				return map[string]expectation{
					"dir":         {"120000", "other"},
					"other/a.txt": {"100644", "other a\n"},
					"keep.txt":    {"100644", "keep\n"},
				}
			},
		},
		{
			name: "directory replaced by a regular file",
			setup: func(t *testing.T, root string) map[string]expectation {
				writeFixture(t, filepath.Join(root, "dir", "a.txt"), []byte("tracked a\n"))
				writeFixture(t, filepath.Join(root, "dir", "sub", "b.txt"), []byte("tracked b\n"))
				writeFixture(t, filepath.Join(root, "keep.txt"), []byte("keep\n"))
				snapCommitAll(t, root, "base")
				if err := os.RemoveAll(filepath.Join(root, "dir")); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(root, "dir"), []byte("now a regular file\n"))
				return map[string]expectation{
					"dir":      {"100644", "now a regular file\n"},
					"keep.txt": {"100644", "keep\n"},
				}
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			snapIsolate(t)
			root := snapNewRepository(t)
			want := testCase.setup(t, root)

			before := snapCaptureState(t, root)
			snapshot, err := NewSnapshot(t.Context(), root)
			if err != nil {
				t.Fatalf("R1: NewSnapshot with tracked files behind a replaced directory: %v", err)
			}
			t.Cleanup(func() { _ = snapshot.Remove() })
			snapAssertRepository(t, snapshot)

			tree := snapReadTree(t, snapshot.Dir)
			if got, wantPaths := snapSortedKeys(tree), snapSortedKeys(want); strings.Join(got, "\n") != strings.Join(wantPaths, "\n") {
				t.Errorf("R1: snapshot paths = %q, want %q", got, wantPaths)
			}
			for path, expected := range want {
				entry, ok := tree[path]
				if !ok {
					continue
				}
				if entry.Mode != expected.mode {
					t.Errorf("R1: %s recorded with mode %s, want %s", path, entry.Mode, expected.mode)
				}
				if got := snapBlob(t, snapshot.Dir, entry.Hash); got != expected.content {
					t.Errorf("R1: %s recorded content/target %q, want %q", path, got, expected.content)
				}
			}
			if snapshot.Files != len(want) {
				t.Errorf("R1: Snapshot.Files = %d, want %d", snapshot.Files, len(want))
			}
			tree0 := snapshot.Tree
			if err := snapshot.Remove(); err != nil {
				t.Fatalf("R6: Remove: %v", err)
			}
			snapAssertUnchanged(t, "R1 (replaced leading directory)", before, snapCaptureState(t, root))

			if committed := snapRealCommitTree(t, root); tree0 != committed {
				t.Errorf("R2: Snapshot.Tree = %s, want %s from git add -A && git commit", tree0, committed)
			}
		})
	}
}

// R5 (revision 4): an unreadable leading directory above a tracked file is
// refused, not treated as a deletion.

func TestNewSnapshotRefusesUnreadableLeadingDirectory(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("R5: root can inspect a mode 000 directory")
	}
	snapIsolate(t)
	root := snapNewRepository(t)
	writeFixture(t, filepath.Join(root, "a", "b", "c"), []byte("tracked under an unreadable directory\n"))
	writeFixture(t, filepath.Join(root, "keep.txt"), []byte("keep\n"))
	snapCommitAll(t, root, "base")

	unreadable := filepath.Join(root, "a")
	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })
	snapChmod(t, unreadable, 0o000)

	snapshot, err := NewSnapshot(t.Context(), root)
	if err == nil {
		paths := snapSortedKeys(snapReadTree(t, snapshot.Dir))
		_ = snapshot.Remove()
		t.Fatalf("R5: NewSnapshot succeeded with tracked a/b/c under unreadable a/ (snapshot paths %q), want an error", paths)
	}
	snapAssertNoDirectories(t, "R5 (unreadable leading directory)")
}
