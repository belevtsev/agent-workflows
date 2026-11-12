//go:build linux || darwin

package fslock_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// holdEnv names the lock file a re-executed test binary holds until killed.
const holdEnv = "FSLOCK_TEST_HOLD_PATH"

func TestMain(m *testing.M) {
	if path := os.Getenv(holdEnv); path != "" {
		os.Exit(holdUntilKilled(path))
	}
	os.Exit(m.Run())
}

func holdUntilKilled(path string) int {
	lock, err := fslock.Acquire(context.Background(), path, fslock.Options{Wait: fslock.NoWait})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	defer lock.Close()
	if _, err := fmt.Println("held"); err != nil {
		return 3
	}
	// The parent keeps stdin open, so this returns only if the parent dies.
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 4
}

func tempLock(t *testing.T) string { return filepath.Join(t.TempDir(), "state.lock") }

func hold(t *testing.T, path string, opts fslock.Options) *fslock.Lock {
	t.Helper()
	lock, err := fslock.Acquire(t.Context(), path, opts)
	if err != nil {
		t.Fatalf("Acquire %s: %v", path, err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	return lock
}

func tryOnce(path string, shared bool) error {
	lock, err := fslock.Acquire(context.Background(), path, fslock.Options{Shared: shared, Wait: fslock.NoWait})
	if err == nil {
		err = lock.Close()
	}
	return err
}

// waiter starts Acquire in a goroutine. reached closes when Acquire has opened
// and vetted the lock file, that is, just before it starts waiting.
func waiter(ctx context.Context, path string, opts fslock.Options) (reached <-chan struct{}, result <-chan error) {
	opened, done := make(chan struct{}), make(chan error, 1)
	var once sync.Once
	opts.Check = func(*os.File) error { once.Do(func() { close(opened) }); return nil }
	go func() {
		lock, err := fslock.Acquire(ctx, path, opts)
		if err == nil {
			err = lock.Close()
		}
		done <- err
	}()
	return opened, done
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("Acquire did not return within 10s")
		panic("unreachable")
	}
}

// Oracle: while one descriptor holds the exclusive lock, an exclusive and a
// shared NoWait Acquire on another descriptor both fail with ErrBusy.
func TestExclusiveHolderExcludesEveryOtherHolder(t *testing.T) {
	path := tempLock(t)
	hold(t, path, fslock.Options{})
	for _, shared := range []bool{false, true} {
		if err := tryOnce(path, shared); !errors.Is(err, fslock.ErrBusy) {
			t.Fatalf("shared=%v beside an exclusive holder: %v, want ErrBusy", shared, err)
		}
	}
}

// Oracle: two shared holders coexist; an exclusive NoWait Acquire fails with
// ErrBusy while either holds and succeeds once both have closed.
func TestSharedHoldersCoexistAndExcludeAWriter(t *testing.T) {
	path := tempLock(t)
	first := hold(t, path, fslock.Options{Shared: true})
	second := hold(t, path, fslock.Options{Shared: true, Wait: fslock.NoWait})
	for _, lock := range []*fslock.Lock{first, second} {
		if err := tryOnce(path, false); !errors.Is(err, fslock.ErrBusy) {
			t.Fatalf("exclusive beside a shared holder: %v, want ErrBusy", err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := tryOnce(path, false); err != nil {
		t.Fatalf("exclusive after both readers closed: %v", err)
	}
}

// Oracle: Acquire creates the lock file with mode 0600; after Close a NoWait
// Acquire succeeds, and the lock file is still the same inode.
func TestCloseReleasesTheLockAndKeepsTheFile(t *testing.T) {
	path := tempLock(t)
	lock := hold(t, path, fslock.Options{})
	before, err := os.Lstat(path)
	if err != nil || before.Mode() != 0o600 {
		t.Fatalf("lock file: %v %v, want a regular file with mode 0600", before, err)
	}
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	if err := tryOnce(path, false); err != nil {
		t.Fatalf("Acquire after Close: %v", err)
	}
	if after, err := os.Lstat(path); err != nil || !os.SameFile(before, after) {
		t.Fatalf("lock file was unlinked or replaced: %v %v", after, err)
	}
}

// Oracle: a waiter with no Wait bound, which opened the lock file while the
// holder held it, gets the lock once the holder closes.
func TestWaiterTakesTheLockWhenTheHolderCloses(t *testing.T) {
	path := tempLock(t)
	holder := hold(t, path, fslock.Options{})
	reached, result := waiter(t.Context(), path, fslock.Options{Poll: time.Millisecond})
	<-reached
	if err := holder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := await(t, result); err != nil {
		t.Fatalf("waiter after the holder closed: %v", err)
	}
}

// Oracle: with the lock held, Acquire returns ErrBusy when Wait elapses, the
// context error when the context ends first, and Canceled at once for a
// context already done, without creating the lock file.
func TestWaitEndsWithErrBusyOrTheContextError(t *testing.T) {
	path := tempLock(t)
	hold(t, path, fslock.Options{})

	bound, unbind := context.WithTimeout(t.Context(), 10*time.Second)
	defer unbind()
	start := time.Now()
	_, err := fslock.Acquire(bound, path, fslock.Options{Wait: 50 * time.Millisecond})
	if !errors.Is(err, fslock.ErrBusy) || time.Since(start) < 50*time.Millisecond {
		t.Fatalf("Wait 50ms: %v after %s, want ErrBusy after at least 50ms", err, time.Since(start))
	}

	ctx, stop := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer stop()
	if _, err := fslock.Acquire(ctx, path, fslock.Options{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("context deadline while waiting: %v, want DeadlineExceeded", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	reached, result := waiter(ctx, path, fslock.Options{})
	<-reached
	cancel()
	if err := await(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel while waiting: %v, want Canceled", err)
	}

	missing := filepath.Join(t.TempDir(), "never.lock")
	if _, err := fslock.Acquire(ctx, missing, fslock.Options{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("context already done: %v, want Canceled", err)
	}
	if _, err := os.Lstat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("a done context created the lock file: %v", err)
	}
}

// Oracle: a child process takes the lock and prints "held"; Acquire then
// fails with ErrBusy; once the child is killed with SIGKILL and reaped, a
// NoWait Acquire succeeds at once.
func TestLockIsReleasedWhenTheHolderProcessDies(t *testing.T) {
	path := tempLock(t)
	child := exec.Command(os.Args[0], "-test.run=^$")
	child.Env = append(os.Environ(), holdEnv+"="+path)
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "held\n" {
		t.Fatalf("child did not take the lock: %q %v; stderr: %s", line, err, stderr.String())
	}
	if err := tryOnce(path, false); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("Acquire beside the child's lock: %v, want ErrBusy", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil || child.ProcessState.Success() {
		t.Fatalf("child was not killed: %v", err)
	}
	if err := tryOnce(path, false); err != nil {
		t.Fatalf("lock still held after its holder died: %v", err)
	}
}

// Oracle: a waiter whose lock file is removed, or renamed aside and recreated,
// before the holder closes fails with ErrReplaced, never with an error a
// NoCreate reader would take for "no lock file".
func TestWaiterRefusesALockFileReplacedWhileWaiting(t *testing.T) {
	swaps := map[string]func(t *testing.T, path string){
		"removed": func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		},
		"renamed and recreated": func(t *testing.T, path string) {
			if err := os.Rename(path, path+".aside"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, swap := range swaps {
		t.Run(name, func(t *testing.T) {
			path := tempLock(t)
			holder := hold(t, path, fslock.Options{})
			reached, result := waiter(t.Context(), path, fslock.Options{Shared: true, NoCreate: true, Poll: time.Millisecond})
			<-reached
			swap(t, path)
			if err := holder.Close(); err != nil {
				t.Fatal(err)
			}
			err := await(t, result)
			if !errors.Is(err, fslock.ErrReplaced) || errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("waiter on a %s lock file: %v, want ErrReplaced only", name, err)
			}
		})
	}
}

// Oracle: a symlink fails with ErrSymlink, a FIFO or a directory with
// ErrNotRegular, a missing file under NoCreate with fs.ErrNotExist, and a
// failing Check with its own error; none of them creates or changes a file.
func TestAcquireRefusesUnsafeOrMissingLockFiles(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link, fifo, sub, missing := filepath.Join(dir, "link"), filepath.Join(dir, "fifo"), filepath.Join(dir, "sub"), filepath.Join(dir, "missing")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	refuse := errors.New("check refused")
	cases := []struct {
		path string
		opts fslock.Options
		want error
	}{
		{link, fslock.Options{}, fslock.ErrSymlink},
		{fifo, fslock.Options{}, fslock.ErrNotRegular},
		{sub, fslock.Options{Shared: true}, fslock.ErrNotRegular},
		{missing, fslock.Options{Shared: true, NoCreate: true}, fs.ErrNotExist},
		{target, fslock.Options{Check: func(*os.File) error { return refuse }}, refuse},
	}
	for _, c := range cases {
		if _, err := fslock.Acquire(t.Context(), c.path, c.opts); !errors.Is(err, c.want) {
			t.Errorf("Acquire %s: %v, want %v", filepath.Base(c.path), err, c.want)
		}
	}
	entries, err := os.ReadDir(dir)
	if data, readErr := os.ReadFile(target); err != nil || len(entries) != 4 || readErr != nil || string(data) != "keep" {
		t.Fatalf("a refused Acquire changed the directory: %d entries %v, target %q %v", len(entries), err, data, readErr)
	}
}

// Oracle: with Dir, the lock file is opened in that open directory; after the
// directory is renamed and its old path made a symlink elsewhere, Verify still
// succeeds and a path-based Acquire under the new name finds the lock held.
func TestDirAnchorsTheLockFileToAnOpenDirectory(t *testing.T) {
	parent := t.TempDir()
	original, moved, elsewhere := filepath.Join(parent, "store"), filepath.Join(parent, "moved"), t.TempDir()
	if err := os.Mkdir(original, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.Open(original)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	lock := hold(t, "state.lock", fslock.Options{Dir: dir})
	if err := os.Rename(original, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(elsewhere, original); err != nil {
		t.Fatal(err)
	}
	if err := lock.Verify(); err != nil {
		t.Fatalf("Verify after the directory path moved: %v", err)
	}
	if err := tryOnce(filepath.Join(moved, "state.lock"), false); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("Acquire in the moved directory: %v, want ErrBusy", err)
	}
}
