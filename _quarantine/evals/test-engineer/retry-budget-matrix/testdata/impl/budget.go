// Implementation. The test engineer must never read this file.
package retry

import (
	"errors"
	"math"
	"sync"
	"time"
)

var ErrInvalidRatio = errors.New("retry: ratio must be in (0, 1]")
var ErrInvalidMin = errors.New("retry: minRetries must not be negative")
var ErrNilClock = errors.New("retry: clock must not be nil")

const window = 10 * time.Second

type Stats struct{ Requests, Retries int }

type Budget struct {
	mu       sync.Mutex
	ratio    float64
	min      int
	clock    func() time.Time
	requests []time.Time
	retries  []time.Time
}

func NewBudget(ratio float64, minRetries int, clock func() time.Time) (*Budget, error) {
	if ratio <= 0 || ratio > 1 {
		return nil, ErrInvalidRatio
	}
	if minRetries < 0 {
		return nil, ErrInvalidMin
	}
	if clock == nil {
		return nil, ErrNilClock
	}
	return &Budget{ratio: ratio, min: minRetries, clock: clock}, nil
}

// prune drops timestamps more than window before now, so a timestamp exactly
// window old still counts (R5).
func prune(ts []time.Time, cutoff time.Time) []time.Time {
	i := 0
	for i < len(ts) && ts[i].Before(cutoff) {
		i++
	}
	return ts[i:]
}

func (b *Budget) RecordRequest() {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.requests = append(prune(b.requests, now.Add(-window)), now)
}

func (b *Budget) Allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.requests = prune(b.requests, now.Add(-window))
	b.retries = prune(b.retries, now.Add(-window))
	limit := int(math.Ceil(b.ratio * float64(len(b.requests))))
	if limit < b.min {
		limit = b.min
	}
	if len(b.retries) >= limit {
		return false
	}
	b.retries = append(b.retries, now)
	return true
}

func (b *Budget) Stats() Stats {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.clock()
	b.requests = prune(b.requests, now.Add(-window))
	b.retries = prune(b.retries, now.Add(-window))
	return Stats{Requests: len(b.requests), Retries: len(b.retries)}
}
