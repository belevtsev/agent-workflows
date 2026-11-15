//go:build windows

package fslock_test

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/fslock"
)

// Environment of a re-executed test binary that holds a lock until killed:
// the lock file, and "1" for a shared lock.
const (
	winHoldEnv   = "FSLOCK_TEST_HOLD_PATH"
	winSharedEnv = "FSLOCK_TEST_HOLD_SHARED"
)

func TestMain(m *testing.M) {
	if path := os.Getenv(winHoldEnv); path != "" {
		os.Exit(winHold(path, os.Getenv(winSharedEnv) == "1"))
	}
	os.Exit(m.Run())
}

// winHold takes the lock, prints "held", and keeps it until stdin ends.
func winHold(path string, shared bool) int {
	lock, err := fslock.Acquire(context.Background(), path, fslock.Options{Shared: shared, Wait: fslock.NoWait})
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

// startHolder starts a process that holds the lock at path and returns once
// it holds it.
func startHolder(t *testing.T, path string, shared bool) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command(exe, "-test.run=^$")
	child.Env = append(os.Environ(), winHoldEnv+"="+path)
	if shared {
		child.Env = append(child.Env, winSharedEnv+"=1")
	}
	var stderr bytes.Buffer
	child.Stderr = &stderr
	stdin, err := child.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = child.Process.Kill(); _ = child.Wait() })
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || line != "held\n" {
		t.Fatalf("holder did not take the lock: %q %v; stderr: %s", line, err, stderr.String())
	}
	return child
}

func tryLockOnce(path string, shared bool) error {
	lock, err := fslock.Acquire(context.Background(), path, fslock.Options{Shared: shared, Wait: fslock.NoWait})
	if err == nil {
		err = lock.Close()
	}
	return err
}

// Oracle: while another process holds the exclusive lock, an exclusive and a
// shared NoWait Acquire both fail with ErrBusy.
func TestSecondProcessCannotTakeAnExclusiveLockTheFirstHolds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	startHolder(t, path, false)
	if err := tryLockOnce(path, false); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("exclusive Acquire beside another process's exclusive lock: %v, want ErrBusy", err)
	}
	if err := tryLockOnce(path, true); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("shared Acquire beside another process's exclusive lock: %v, want ErrBusy", err)
	}
}

// Oracle: while another process holds a shared lock, a shared NoWait Acquire
// succeeds and an exclusive one fails with ErrBusy.
func TestSharedLocksOfTwoProcessesCoexist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	startHolder(t, path, true)
	if err := tryLockOnce(path, true); err != nil {
		t.Fatalf("shared Acquire beside another process's shared lock: %v, want success", err)
	}
	if err := tryLockOnce(path, false); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("exclusive Acquire beside another process's shared lock: %v, want ErrBusy", err)
	}
}

// Oracle: once the process holding the exclusive lock is killed and reaped,
// a NoWait Acquire succeeds at once.
func TestLockIsFreeAtOnceAfterItsHolderIsKilled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.lock")
	child := startHolder(t, path, false)
	if err := tryLockOnce(path, false); !errors.Is(err, fslock.ErrBusy) {
		t.Fatalf("Acquire beside the holder's lock: %v, want ErrBusy", err)
	}
	if err := child.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := child.Wait(); err == nil || child.ProcessState.Success() {
		t.Fatalf("holder was not killed: %v", err)
	}
	if err := tryLockOnce(path, false); err != nil {
		t.Fatalf("lock still held after its holder was killed: %v", err)
	}
}
