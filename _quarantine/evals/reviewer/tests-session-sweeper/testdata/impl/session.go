// Implementation. The test reviewer must never read this file.
package session

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrInvalidIdle = errors.New("session: idle timeout must be positive")
var ErrNilClock = errors.New("session: clock must not be nil")
var ErrClosed = errors.New("session: store is closed")

type Store struct {
	mu     sync.Mutex
	idle   time.Duration
	clock  func() time.Time
	last   map[string]time.Time
	closed bool
}

func NewStore(idle time.Duration, clock func() time.Time) (*Store, error) {
	if idle <= 0 {
		return nil, ErrInvalidIdle
	}
	if clock == nil {
		return nil, ErrNilClock
	}
	return &Store{idle: idle, clock: clock, last: map[string]time.Time{}}, nil
}

func (s *Store) Touch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	s.last[id] = s.clock()
	return nil
}

func (s *Store) Active(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	last, ok := s.last[id]
	return ok && s.clock().Before(last.Add(s.idle))
}

// sweepBatch is how many sessions Sweep examines between context checks.
const sweepBatch = 256

func (s *Store) Sweep(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	removed, seen := 0, 0
	for id, last := range s.last {
		if seen%sweepBatch == 0 {
			if err := ctx.Err(); err != nil {
				return removed, err
			}
		}
		seen++
		if !now.Before(last.Add(s.idle)) {
			delete(s.last, id)
			removed++
		}
	}
	return removed, nil
}

func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.last)
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}
