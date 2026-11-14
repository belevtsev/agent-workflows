//go:build darwin || linux

package usagelog

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

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
