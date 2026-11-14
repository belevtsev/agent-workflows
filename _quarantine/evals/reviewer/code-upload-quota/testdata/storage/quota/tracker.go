// Package quota tracks per-tenant storage usage against a limit.
package quota

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrQuotaExceeded is returned when a reservation would pass the tenant's limit.
var ErrQuotaExceeded = errors.New("quota: limit exceeded")

// ErrInvalidSize is returned for a reservation of zero or fewer bytes.
var ErrInvalidSize = errors.New("quota: invalid size")

// defaultLimit is used when the limit service does not answer in time.
const defaultLimit = 1 << 30

// LimitSource returns a tenant's storage limit in bytes.
type LimitSource interface {
	Limit(ctx context.Context, tenant string) (int64, error)
}

// Tracker holds each tenant's reserved bytes. It is safe for concurrent use.
type Tracker struct {
	limits LimitSource
	mu     sync.Mutex
	used   map[string]int64
}

// NewTracker returns an empty Tracker that reads limits from limits.
func NewTracker(limits LimitSource) *Tracker {
	return &Tracker{limits: limits, used: map[string]int64{}}
}

// Reserve adds bytes to tenant's usage when the result stays within its limit.
func (t *Tracker) Reserve(ctx context.Context, tenant string, bytes int64) error {
	if bytes <= 0 {
		return ErrInvalidSize
	}
	limit, err := t.limits.Limit(ctx, tenant)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			limit = defaultLimit
		} else {
			return fmt.Errorf("quota: limit for %s: %w", tenant, err)
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if bytes > limit-t.used[tenant] {
		return ErrQuotaExceeded
	}
	t.used[tenant] += bytes
	return nil
}

// Release returns bytes to tenant's quota.
func (t *Tracker) Release(tenant string, bytes int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.used[tenant] -= bytes
}

// Usage returns tenant's reserved bytes.
func (t *Tracker) Usage(tenant string) int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.used[tenant]
}

// Snapshot returns a copy of every tenant's usage.
func (t *Tracker) Snapshot() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make(map[string]int64, len(t.used))
	for tenant, bytes := range t.used {
		out[tenant] = bytes
	}
	return out
}
