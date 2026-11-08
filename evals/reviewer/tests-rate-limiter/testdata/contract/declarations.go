// Declaration extract for package ratelimit (bodies stripped).
package ratelimit

import (
	"errors"
	"time"
)

var ErrInvalidLimit = errors.New("ratelimit: limit must be at least 1")
var ErrInvalidWindow = errors.New("ratelimit: window must be positive")
var ErrNilClock = errors.New("ratelimit: clock must not be nil")

type Limiter struct{}

func New(limit int, window time.Duration, clock func() time.Time) (*Limiter, error)
func (l *Limiter) Allow(key string) bool
func (l *Limiter) Count(key string) int
