//go:build windows

package fslock

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// On Windows the lock is a LockFileEx byte-range lock. The system releases it
// when its handle closes, including when the holding process dies, so a killed
// holder never leaves a stale lock, as with flock(2).
//
// Differences from Linux and macOS:
//   - A Windows byte-range lock is mandatory: other handles cannot read or
//     write the locked bytes. The lock therefore covers one byte far past any
//     data a lock file holds (lockOffset), so readers of the file's contents
//     are not blocked.
//   - A new lock file gets the access control list it inherits from its
//     directory; there are no mode bits, so it is not created 0600, and no
//     owner or ACL check is made here (Options.Check can add one).
//   - A reparse point (a symbolic link, a junction, or any other kind) at the
//     lock path is refused with ErrSymlink; it is opened, never followed.
//   - "Regular file" means a disk file that is not a directory.
//   - The file is opened with every share mode, so another process can open
//     it to lock it and can delete or rename it; Verify then reports
//     ErrReplaced. The file identity compared is the volume serial number and
//     the 64-bit file index, which ReFS does not guarantee to be unique.

// lockOffset is where the one locked byte lies: far past any data, and far
// below the end of the 64-bit range so offset plus length cannot overflow.
const (
	lockOffsetHigh = 0x7FFFFFFF
	lockOffsetLow  = 0
)

// shareAll lets other handles read, write, delete and rename the lock file.
const shareAll = windows.FILE_SHARE_READ | windows.FILE_SHARE_WRITE | windows.FILE_SHARE_DELETE

// Lock is a held lock. Close releases it.
type Lock struct {
	file  *os.File
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
	lock := &Lock{path: path, check: opts.Check}
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
	flags := uint32(windows.LOCKFILE_FAIL_IMMEDIATELY)
	if !opts.Shared {
		flags |= windows.LOCKFILE_EXCLUSIVE_LOCK
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		err := lock.control(func(h windows.Handle) error {
			return windows.LockFileEx(h, flags, 0, 1, 0, lockRange())
		})
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
			return nil, &os.PathError{Op: "LockFileEx", Path: path, Err: err}
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

// lockRange returns the position of the locked byte. The handle is
// synchronous, so the structure only carries the offset.
func lockRange() *windows.Overlapped {
	return &windows.Overlapped{OffsetHigh: lockOffsetHigh, Offset: lockOffsetLow}
}

// open opens the lock file without following a reparse point. A missing file
// is created with CREATE_NEW, so one creator arbitrates and every contender
// then opens the one file.
func (l *Lock) open(ctx context.Context, opts Options, deadline <-chan time.Time) error {
	access := uint32(windows.GENERIC_READ)
	if !opts.Shared {
		access |= windows.GENERIC_WRITE
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		h, err := openFile(l.path, access, false)
		if errors.Is(err, fs.ErrNotExist) && !opts.NoCreate {
			h, err = openFile(l.path, access, true)
		}
		switch {
		case err == nil:
			l.file = os.NewFile(uintptr(h), l.path)
			return nil
		case errors.Is(err, windows.ERROR_FILE_EXISTS), errors.Is(err, windows.ERROR_ALREADY_EXISTS):
			if err := pause(ctx, deadline, time.Millisecond, l.path); err != nil {
				return err
			}
		default:
			return &os.PathError{Op: "open", Path: l.path, Err: err}
		}
	}
}

// openFile opens path, or creates it when create is set, without following
// a final reparse point. The handle is synchronous and not inherited by
// children. Directories open too (backup semantics), so Verify can name them.
func openFile(path string, access uint32, create bool) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	disposition := uint32(windows.OPEN_EXISTING)
	if create {
		disposition = windows.CREATE_NEW
	}
	return windows.CreateFile(name, access, shareAll, nil, disposition,
		windows.FILE_ATTRIBUTE_NORMAL|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
}

// control runs fn on the lock file's handle. It fails once the Lock is
// closed, so fn never sees a handle value the process has since reused.
func (l *Lock) control(fn func(h windows.Handle) error) error {
	raw, err := l.file.SyscallConn()
	if err != nil {
		return err
	}
	var fnErr error
	if err := raw.Control(func(fd uintptr) { fnErr = fn(windows.Handle(fd)) }); err != nil {
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
	var held windows.ByHandleFileInformation
	var kind uint32
	if err := l.control(func(h windows.Handle) error {
		if err := windows.GetFileInformationByHandle(h, &held); err != nil {
			return err
		}
		var err error
		kind, err = windows.GetFileType(h)
		return err
	}); err != nil {
		return &os.PathError{Op: "stat", Path: l.path, Err: err}
	}
	if held.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return &os.PathError{Op: "lock", Path: l.path, Err: ErrSymlink}
	}
	if held.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY != 0 || kind != windows.FILE_TYPE_DISK {
		return &os.PathError{Op: "lock", Path: l.path, Err: ErrNotRegular}
	}
	if !l.named(&held) {
		return &os.PathError{Op: "lock", Path: l.path, Err: ErrReplaced}
	}
	if l.check != nil {
		return l.check(l.file)
	}
	return nil
}

// named reports whether path still opens the file described by held.
func (l *Lock) named(held *windows.ByHandleFileInformation) bool {
	h, err := openFile(l.path, windows.FILE_READ_ATTRIBUTES, false)
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var now windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &now); err != nil {
		return false
	}
	return now.VolumeSerialNumber == held.VolumeSerialNumber &&
		now.FileIndexHigh == held.FileIndexHigh && now.FileIndexLow == held.FileIndexLow
}

// Close releases the lock and closes its handle. The lock file stays. Closing
// the handle alone would release the lock too, but Windows may do that some
// time later, so the byte is unlocked first.
func (l *Lock) Close() error {
	_ = l.control(func(h windows.Handle) error {
		return windows.UnlockFileEx(h, 0, 1, 0, lockRange())
	})
	return l.file.Close()
}
