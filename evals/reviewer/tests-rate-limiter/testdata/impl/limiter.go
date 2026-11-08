// Implementation. The test reviewer must never read this file.
package ratelimit

import (
	"errors"
	"sync"
	"time"
)

var ErrInvalidLimit = errors.New("ratelimit: limit must be at least 1")
var ErrInvalidWindow = errors.New("ratelimit: window must be positive")
var ErrNilClock = errors.New("ratelimit: clock must not be nil")

type bucket struct {
	start time.Time
	count int
}

type Limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clock   func() time.Time
	buckets map[string]*bucket
}

func New(limit int, window time.Duration, clock func() time.Time) (*Limiter, error) {
	switch {
	case limit < 1:
		return nil, ErrInvalidLimit
	case window <= 0:
		return nil, ErrInvalidWindow
	case clock == nil:
		return nil, ErrNilClock
	}
	return &Limiter{limit: limit, window: window, clock: clock, buckets: map[string]*bucket{}}, nil
}

// windowFor returns key's bucket, starting a new window when the current one
// has elapsed (R4).
func (l *Limiter) windowFor(key string, now time.Time) *bucket {
	b := l.buckets[key]
	if b == nil || !now.Before(b.start.Add(l.window)) {
		b = &bucket{start: now}
		l.buckets[key] = b
	}
	return b
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.windowFor(key, l.clock())
	if b.count >= l.limit {
		return false
	}
	b.count++
	return true
}

func (l *Limiter) Count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil || !l.clock().Before(b.start.Add(l.window)) {
		return 0
	}
	return b.count
}
