//go:build darwin || linux

package graphify

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"
)

// acquireCacheLock locks a stable sibling of a cache entry. Closing releases
// the lock, including on process death; unlinking would split waiting builders
// across different inodes and is deliberately never part of release.
func acquireCacheLock(ctx context.Context, path string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		if errors.Is(err, syscall.ELOOP) {
			return nil, refuse("cache_path", "Graphify cache lock must be a regular file, never a symlink: "+path)
		}
		return nil, &os.PathError{Op: "open cache lock", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	identity := func() error {
		opened, err := file.Stat()
		if err != nil {
			return err
		}
		named, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !opened.Mode().IsRegular() || !named.Mode().IsRegular() || !os.SameFile(opened, named) {
			return refuse("cache_path", "Graphify cache lock must remain the same regular file: "+path)
		}
		return nil
	}
	if err := identity(); err != nil {
		_ = file.Close()
		return nil, err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			if err := identity(); err != nil {
				_ = file.Close()
				return nil, err
			}
			return func() { _ = file.Close() }, nil
		}
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, &os.PathError{Op: "lock cache", Path: path, Err: err}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
}
