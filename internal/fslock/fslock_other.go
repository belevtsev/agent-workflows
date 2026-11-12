//go:build !linux && !darwin

package fslock

import (
	"context"
	"errors"
	"os"
)

// Lock is a held lock. This platform cannot take one.
type Lock struct{}

// Acquire fails: file locks are supported on Linux and macOS only.
func Acquire(_ context.Context, path string, _ Options) (*Lock, error) {
	return nil, &os.PathError{Op: "lock", Path: path, Err: errors.ErrUnsupported}
}

// Verify fails: no lock can be held on this platform.
func (*Lock) Verify() error { return errors.ErrUnsupported }

// Close does nothing: no lock can be held on this platform.
func (*Lock) Close() error { return nil }
