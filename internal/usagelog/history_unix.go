//go:build darwin || linux

package usagelog

import (
	"context"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// historyLockWait bounds how long an import or report waits for another
// writer before failing; the lock is never broken.
const historyLockWait = 30 * time.Second

// lockHistory takes an flock on the stable sibling path+".lock", exclusive
// for writers and shared for readers. Closing the descriptor releases the
// lock, including on process death, so a crashed writer never leaves a stale
// lock; the lock file is never unlinked, which would split waiters across
// inodes. A reader finding no lock file reads unlocked: no writer has run.
func lockHistory(ctx context.Context, path string, exclusive bool) (func(), error) {
	lockPath := path + ".lock"
	flags, how := syscall.O_RDWR|syscall.O_CREAT, syscall.LOCK_EX
	if !exclusive {
		flags, how = syscall.O_RDONLY, syscall.LOCK_SH
	}
	fd, err := syscall.Open(lockPath, flags|syscall.O_CLOEXEC|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		if !exclusive && errors.Is(err, syscall.ENOENT) {
			return func() {}, nil
		}
		if errors.Is(err, syscall.ELOOP) {
			return nil, fmt.Errorf("cost history lock must be a regular file, not a symlink: %s", lockPath)
		}
		return nil, &os.PathError{Op: "open cost history lock", Path: lockPath, Err: err}
	}
	file := os.NewFile(uintptr(fd), lockPath)
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, fmt.Errorf("cost history lock must be a regular file: %s", lockPath)
	}
	deadline := time.NewTimer(historyLockWait)
	defer deadline.Stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Flock(fd, how|syscall.LOCK_NB)
		if err == nil {
			// A lock file removed or replaced while waiting no longer
			// excludes writers that open the new name.
			held, heldErr := file.Stat()
			named, namedErr := os.Lstat(lockPath)
			if heldErr != nil || namedErr != nil || !os.SameFile(held, named) {
				_ = file.Close()
				return nil, fmt.Errorf("cost history lock %s was removed or replaced while waiting; retry", lockPath)
			}
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EINTR) && !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, &os.PathError{Op: "lock cost history", Path: lockPath, Err: err}
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = file.Close()
			return nil, fmt.Errorf("cost history %s is still locked by another cw process after %s", path, historyLockWait)
		case <-ticker.C:
		}
	}
}

// openHistory opens the history without following a final symlink and
// without blocking on a FIFO, and refuses anything but a regular file. A
// writer creates it with mode 0600 and appends; created reports whether this
// call created it.
func openHistory(path string, write bool) (file *os.File, created bool, err error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, false, fmt.Errorf("cost history must be a regular file, not a symlink, directory or device: %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) || !write {
		return nil, false, fmt.Errorf("read cost history: %w", err)
	} else {
		created = true
	}
	flags := os.O_RDONLY
	if write {
		flags = os.O_RDWR | os.O_CREATE | os.O_APPEND
	}
	file, err = os.OpenFile(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, false, fmt.Errorf("cost history must be a regular file, not a symlink: %s", path)
		}
		return nil, false, fmt.Errorf("open cost history: %w", err)
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, false, fmt.Errorf("cost history must be a regular file: %s", path)
	}
	return file, created, nil
}
