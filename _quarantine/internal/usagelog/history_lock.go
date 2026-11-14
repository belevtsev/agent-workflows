package usagelog

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// historyLockWait bounds how long an import or report waits for another
// writer before failing; the lock is never broken.
const historyLockWait = 30 * time.Second

var errHistoryPlatform = errors.New("the cost history currently supports macOS and Linux")

// lockHistory takes an flock on the stable sibling path+".lock", exclusive
// for writers and shared for readers. Closing the descriptor releases the
// lock, including on process death, so a crashed writer never leaves a stale
// lock; the lock file is never unlinked, which would split waiters across
// inodes. A reader finding no lock file reads unlocked: no writer has run.
func lockHistory(ctx context.Context, path string, exclusive bool) (func(), error) {
	lockPath := path + ".lock"
	lock, err := fslock.Acquire(ctx, lockPath, fslock.Options{
		Shared:   !exclusive,
		NoCreate: !exclusive,
		Wait:     historyLockWait,
		Poll:     20 * time.Millisecond,
	})
	switch {
	case err == nil:
		return func() { _ = lock.Close() }, nil
	case errors.Is(err, errors.ErrUnsupported):
		return nil, errHistoryPlatform
	case errors.Is(err, fslock.ErrSymlink):
		return nil, fmt.Errorf("cost history lock must be a regular file, not a symlink: %s", lockPath)
	case errors.Is(err, fslock.ErrNotRegular):
		return nil, fmt.Errorf("cost history lock must be a regular file: %s", lockPath)
	case errors.Is(err, fslock.ErrReplaced):
		// A lock file removed or replaced while waiting no longer excludes
		// writers that open the new name.
		return nil, fmt.Errorf("cost history lock %s was removed or replaced while waiting; retry", lockPath)
	case errors.Is(err, fslock.ErrBusy):
		return nil, fmt.Errorf("cost history %s is still locked by another cw process after %s", path, historyLockWait)
	case !exclusive && errors.Is(err, fs.ErrNotExist):
		return func() {}, nil
	}
	return nil, err
}
