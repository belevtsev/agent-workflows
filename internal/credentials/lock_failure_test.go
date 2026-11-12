package credentials

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/fslock"
)

// Oracle: a lock failure maps onto the store's errors by cause. A platform
// without file locking is ErrLockUnsupported, never ErrUnsafeStore; a held
// lock is ErrBusy; a context error is returned unchanged; any other failure,
// including Check's own refusal, is ErrUnsafeStore.
func TestLockFailureNamesItsCause(t *testing.T) {
	cases := []struct{ err, want error }{
		{&os.PathError{Op: "lock", Path: lockName, Err: errors.ErrUnsupported}, ErrLockUnsupported},
		{&os.PathError{Op: "lock", Path: lockName, Err: fslock.ErrBusy}, ErrBusy},
		{context.Canceled, context.Canceled},
		{context.DeadlineExceeded, context.DeadlineExceeded},
		{&os.PathError{Op: "open", Path: lockName, Err: fslock.ErrSymlink}, ErrUnsafeStore},
		{ErrUnsafeStore, ErrUnsafeStore},
	}
	for _, c := range cases {
		if got := lockFailure(c.err); got != c.want {
			t.Errorf("lockFailure(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}
