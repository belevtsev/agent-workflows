// Package fslock takes advisory flock(2) locks on a stable lock file.
//
// The kernel drops a lock when its last descriptor closes, including when the
// holding process dies, so a killed holder never leaves a stale lock. A lock
// file is created once, with mode 0600, and never unlinked: unlinking it would
// let later contenders lock a new inode while a waiter still holds the old one.
// Acquire refuses a symlink, a non-regular file, and a file that path no longer
// names once the lock is held.
//
// Acquire polls with LOCK_NB, so a wait is bounded by the context and by
// Options.Wait. Only Linux and macOS are supported; elsewhere Acquire fails
// with an error that matches errors.ErrUnsupported.
package fslock

import (
	"errors"
	"os"
	"time"
)

// DefaultPoll is the retry interval when Options.Poll is zero.
const DefaultPoll = 25 * time.Millisecond

// NoWait, as Options.Wait, makes Acquire try once and fail with ErrBusy when
// another holder has the lock.
const NoWait time.Duration = -1

// Options select the kind of lock and how long Acquire waits for it.
type Options struct {
	// Dir, when non-nil, is an open directory that path is resolved against
	// with openat(2), so a caller that reached it without following symlinks
	// keeps that guarantee. Dir must stay open until the Lock is closed.
	Dir *os.File
	// Shared takes a shared lock instead of an exclusive one, and opens the
	// file read-only, so a reader needs only read permission.
	Shared bool
	// NoCreate opens an existing lock file only; a missing one fails with an
	// error that matches fs.ErrNotExist.
	NoCreate bool
	// Wait bounds how long Acquire retries while another holder has the lock.
	// Zero waits until ctx is done; a negative value (NoWait) tries once. When
	// Wait elapses, Acquire fails with ErrBusy.
	Wait time.Duration
	// Poll is the retry interval; zero means DefaultPoll.
	Poll time.Duration
	// Check, when non-nil, vets the open lock file before Acquire waits, once
	// the lock is held, and on every Verify. Its error is returned unchanged.
	Check func(*os.File) error
}

// Errors returned by Acquire and Verify, wrapped in an *os.PathError that
// names the lock file. A context error is returned as ctx.Err(), unwrapped.
var (
	// ErrBusy: another holder kept the lock for the whole wait.
	ErrBusy = errors.New("lock is held by another holder")
	// ErrSymlink: the lock path is a symbolic link.
	ErrSymlink = errors.New("lock file is a symbolic link")
	// ErrNotRegular: the lock path is not a regular file.
	ErrNotRegular = errors.New("lock file is not a regular file")
	// ErrReplaced: the lock path no longer names the file that was locked.
	ErrReplaced = errors.New("lock file was removed or replaced")
)
