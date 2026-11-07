//go:build darwin || linux

package devcheck

import (
	"io"
	"io/fs"
	"os"
	"syscall"
)

// lockSnapshot creates and locks a copy's owner file, then writes the marker
// that tells a later run the owner once held the lock. The kernel releases the
// lock when the owner dies, however it dies.
func lockSnapshot(path string) (func(), error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_EXCL|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, &os.PathError{Op: "create snapshot lock", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	// Blocking: a reclaiming run may hold it briefly, finds no marker and
	// leaves the young directory alone.
	if err := syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		_ = file.Close()
		return nil, &os.PathError{Op: "lock snapshot", Path: path, Err: err}
	}
	if _, err := io.WriteString(file, snapshotMarker); err != nil {
		_ = file.Close()
		return nil, err
	}
	return func() { _ = file.Close() }, nil
}

// tryLockSnapshot takes another copy's lock without waiting. ok is false when a
// live owner holds it or it cannot be inspected; a missing lock file counts as
// lockable and unmarked.
func tryLockSnapshot(path string) (release func(), marked, ok bool) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0)
	if err == syscall.ENOENT {
		return func() {}, false, true
	}
	if err != nil {
		return nil, false, false
	}
	file := os.NewFile(uintptr(fd), path)
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, false, false
	}
	content, err := io.ReadAll(io.LimitReader(file, 256))
	return func() { _ = file.Close() }, err == nil && len(content) != 0, true
}

func ownedByCurrentUser(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(stat.Uid) == os.Getuid()
}
