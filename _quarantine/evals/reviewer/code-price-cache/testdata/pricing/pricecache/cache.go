// Package pricecache caches SKU prices fetched from the pricing service.
package pricecache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrNotFound is what an Upstream wraps when the SKU does not exist.
var ErrNotFound = errors.New("pricecache: upstream: not found")

// ErrUnknownSKU is returned by Get for a SKU the upstream does not know.
var ErrUnknownSKU = errors.New("pricecache: unknown SKU")

// Upstream fetches a SKU's current price in cents.
type Upstream interface {
	Fetch(ctx context.Context, sku string) (int64, error)
}

type entry struct {
	price     int64
	missing   bool
	fetchedAt time.Time
}

// Cache holds prices for ttl and unknown SKUs for negativeTTL.
type Cache struct {
	up          Upstream
	ttl         time.Duration
	negativeTTL time.Duration
	now         func() time.Time

	mu      sync.Mutex
	entries map[string]entry
}

// New returns an empty Cache.
func New(up Upstream, ttl, negativeTTL time.Duration, now func() time.Time) *Cache {
	return &Cache{up: up, ttl: ttl, negativeTTL: negativeTTL, now: now, entries: map[string]entry{}}
}

// Get returns sku's price, from the cache while it is fresh.
func (c *Cache) Get(ctx context.Context, sku string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cached, ok := c.entries[sku]
	age := c.now().Sub(cached.fetchedAt)
	if ok && cached.missing && age < c.negativeTTL {
		return 0, ErrUnknownSKU
	}
	if ok && !cached.missing && age < c.ttl {
		return cached.price, nil
	}
	price, err := c.up.Fetch(ctx, sku)
	switch {
	case errors.Is(err, ErrNotFound):
		c.entries[sku] = entry{missing: true, fetchedAt: c.now()}
		return 0, ErrUnknownSKU
	case err != nil:
		if ok && !cached.missing {
			return cached.price, nil
		}
		return 0, fmt.Errorf("pricecache: fetch %s: %w", sku, err)
	}
	c.entries[sku] = entry{price: price, fetchedAt: c.now()}
	return price, nil
}
