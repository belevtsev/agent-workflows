// Stub built from the declaration extract for red-first runs: every
// operation returns its zero value. It is not the implementation.
package session

import (
	"context"
	"errors"
	"time"
)

var ErrInvalidIdle = errors.New("session: idle timeout must be positive")
var ErrNilClock = errors.New("session: clock must not be nil")
var ErrClosed = errors.New("session: store is closed")

type Store struct{}

func NewStore(idle time.Duration, clock func() time.Time) (*Store, error) {
	return &Store{}, nil
}

func (s *Store) Touch(id string) error { return nil }

func (s *Store) Active(id string) bool { return false }

func (s *Store) Sweep(ctx context.Context) (int, error) { return 0, nil }

func (s *Store) Len() int { return 0 }

func (s *Store) Close() error { return nil }
