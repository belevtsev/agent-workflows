package graphify

import (
	"context"
	"errors"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// acquireCacheLock locks a stable sibling of a cache entry, waiting until ctx
// is done. Closing releases the lock, including on process death; the lock
// file is never unlinked, which would split waiting builders across inodes.
func acquireCacheLock(ctx context.Context, path string) (func(), error) {
	lock, err := fslock.Acquire(ctx, path, fslock.Options{Poll: 20 * time.Millisecond})
	switch {
	case err == nil:
		return func() { _ = lock.Close() }, nil
	case errors.Is(err, errors.ErrUnsupported):
		return nil, refuse("platform", "The guarded Graphify cache currently supports macOS and Linux")
	case errors.Is(err, fslock.ErrSymlink):
		return nil, refuse("cache_path", "Graphify cache lock must be a regular file, never a symlink: "+path)
	case errors.Is(err, fslock.ErrNotRegular), errors.Is(err, fslock.ErrReplaced):
		return nil, refuse("cache_path", "Graphify cache lock must remain the same regular file: "+path)
	}
	return nil, err
}
