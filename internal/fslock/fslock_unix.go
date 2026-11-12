//go:build linux || darwin

package fslock

import (
	"context"
	"errors"
	"os"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
)

// Lock is a held lock. Close releases it.
type Lock struct {
	file  *os.File
	dir   *os.File
	path  string
	check func(*os.File) error
}

// Acquire opens or creates the lock file at path and takes the lock, retrying
// every Options.Poll while another holder has it.
func Acquire(ctx context.Context, path string, opts Options) (*Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	poll := opts.Poll
	if poll <= 0 {
		poll = defaultPoll
	}
	var deadline <-chan time.Time
	if opts.Wait > 0 {
		timer := time.NewTimer(opts.Wait)
		defer timer.Stop()
		deadline = timer.C
	}
	lock := &Lock{dir: opts.Dir, path: path, check: opts.Check}
	if err := lock.open(ctx, opts, deadline); err != nil {
		return nil, err
	}
	held := false
	defer func() {
		if !held {
			_ = lock.file.Close()
		}
	}()
	if err := lock.Verify(); err != nil {
		return nil, err
	}
	how := unix.LOCK_EX
	if opts.Shared {
		how = unix.LOCK_SH
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := lock.control(func(fd int) error { return unix.Flock(fd, how|unix.LOCK_NB) })
		if err == nil {
			break
		}
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return nil, &os.PathError{Op: "flock", Path: path, Err: err}
		}
		if opts.Wait < 0 {
			return nil, &os.PathError{Op: "lock", Path: path, Err: ErrBusy}
		}
		if err := pause(ctx, deadline, poll, path); err != nil {
			return nil, err
		}
	}
	if err := lock.Verify(); err != nil {
		return nil, err
	}
	held = true
	return lock, nil
}

// open opens the lock file without following a final symlink or blocking on
// a FIFO. A missing file is created with O_EXCL: APFS can fail simultaneous
// non-exclusive O_CREAT opens with ENOENT, so one exclusive creator arbitrates
// and every contender then opens the one inode.
func (l *Lock) open(ctx context.Context, opts Options, deadline <-chan time.Time) error {
	flags := unix.O_RDWR
	if opts.Shared {
		flags = unix.O_RDONLY
	}
	flags |= unix.O_NOFOLLOW | unix.O_NONBLOCK | unix.O_CLOEXEC
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		fd, err := unix.Openat(l.dirFD(), l.path, flags, 0)
		if errors.Is(err, unix.ENOENT) && !opts.NoCreate {
			fd, err = unix.Openat(l.dirFD(), l.path, flags|unix.O_CREAT|unix.O_EXCL, 0o600)
		}
		runtime.KeepAlive(l.dir)
		switch {
		case err == nil:
			l.file = os.NewFile(uintptr(fd), l.path)
			return nil
		case errors.Is(err, unix.EEXIST), errors.Is(err, unix.EINTR):
			if err := pause(ctx, deadline, time.Millisecond, l.path); err != nil {
				return err
			}
		case errors.Is(err, unix.ELOOP):
			return &os.PathError{Op: "open", Path: l.path, Err: ErrSymlink}
		case errors.Is(err, unix.EISDIR):
			return &os.PathError{Op: "open", Path: l.path, Err: ErrNotRegular}
		default:
			return &os.PathError{Op: "open", Path: l.path, Err: err}
		}
	}
}

func (l *Lock) dirFD() int {
	if l.dir == nil {
		return unix.AT_FDCWD
	}
	return int(l.dir.Fd())
}

// control runs fn on the lock file's descriptor. It fails once the Lock is
// closed, so fn never sees a number the process has since reused.
func (l *Lock) control(fn func(fd int) error) error {
	raw, err := l.file.SyscallConn()
	if err != nil {
		return err
	}
	var fnErr error
	if err := raw.Control(func(fd uintptr) { fnErr = fn(int(fd)) }); err != nil {
		return err
	}
	return fnErr
}

// pause waits for d, and fails with ctx.Err() or, once deadline fires, ErrBusy.
func pause(ctx context.Context, deadline <-chan time.Time, d time.Duration, path string) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-deadline:
		return &os.PathError{Op: "lock", Path: path, Err: ErrBusy}
	case <-timer.C:
		return nil
	}
}

// Verify reports whether the lock file is a regular file that path still
// names, and runs Options.Check. A holder that writes under the lock long
// after Acquire can call it again before committing.
func (l *Lock) Verify() error {
	var held, named unix.Stat_t
	if err := l.control(func(fd int) error { return unix.Fstat(fd, &held) }); err != nil {
		return &os.PathError{Op: "stat", Path: l.path, Err: err}
	}
	if held.Mode&unix.S_IFMT != unix.S_IFREG {
		return &os.PathError{Op: "lock", Path: l.path, Err: ErrNotRegular}
	}
	err := unix.Fstatat(l.dirFD(), l.path, &named, unix.AT_SYMLINK_NOFOLLOW)
	runtime.KeepAlive(l.dir)
	if err != nil || named.Dev != held.Dev || named.Ino != held.Ino {
		return &os.PathError{Op: "lock", Path: l.path, Err: ErrReplaced}
	}
	if l.check != nil {
		return l.check(l.file)
	}
	return nil
}

// Close releases the lock by closing its descriptor. The lock file stays.
func (l *Lock) Close() error { return l.file.Close() }
