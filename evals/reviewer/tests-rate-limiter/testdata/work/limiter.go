// Stub built from the declaration extract for red-first runs: every
// operation returns its zero value. It is not the implementation.
package ratelimit

import (
	"errors"
	"time"
)

var ErrInvalidLimit = errors.New("ratelimit: limit must be at least 1")
var ErrInvalidWindow = errors.New("ratelimit: window must be positive")
var ErrNilClock = errors.New("ratelimit: clock must not be nil")

type Limiter struct{}

func New(limit int, window time.Duration, clock func() time.Time) (*Limiter, error) {
	return &Limiter{}, nil
}

func (l *Limiter) Allow(key string) bool { return false }

func (l *Limiter) Count(key string) int { return 0 }
