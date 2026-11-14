package pricecache

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fakeUpstream struct {
	mu     sync.Mutex
	prices map[string]int64
	err    error
	calls  int
}

func (f *fakeUpstream) Fetch(_ context.Context, sku string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return 0, f.err
	}
	return f.prices[sku], nil
}

func (f *fakeUpstream) Calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

// R1, R2: a miss fetches once; a fresh hit does not fetch again.
func TestGetCachesWithinTTL(t *testing.T) {
	up := &fakeUpstream{prices: map[string]int64{"A1": 1299}}
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	c := New(up, time.Minute, 10*time.Second, clk.Now)
	for range 2 {
		price, err := c.Get(t.Context(), "A1")
		if err != nil || price != 1299 {
			t.Fatalf("Get = %d, %v; want 1299, nil", price, err)
		}
	}
	if up.Calls() != 1 {
		t.Fatalf("upstream calls = %d, want 1", up.Calls())
	}
}

// R2: a stale entry is fetched again.
func TestGetRefetchesStale(t *testing.T) {
	up := &fakeUpstream{prices: map[string]int64{"A1": 1299}}
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	c := New(up, time.Minute, 10*time.Second, clk.Now)
	if _, err := c.Get(t.Context(), "A1"); err != nil {
		t.Fatal(err)
	}
	up.prices["A1"] = 1499
	clk.t = clk.t.Add(time.Minute)
	price, err := c.Get(t.Context(), "A1")
	if err != nil || price != 1499 {
		t.Fatalf("Get after ttl = %d, %v; want 1499, nil", price, err)
	}
	if up.Calls() != 2 {
		t.Fatalf("upstream calls = %d, want 2", up.Calls())
	}
}

// R4: an upstream error is returned wrapped.
func TestGetUpstreamError(t *testing.T) {
	boom := errors.New("pricing service unavailable")
	up := &fakeUpstream{err: boom}
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	c := New(up, time.Minute, 10*time.Second, clk.Now)
	if _, err := c.Get(t.Context(), "A1"); !errors.Is(err, boom) {
		t.Fatalf("Get = %v, want an error wrapping %v", err, boom)
	}
}

// R5: concurrent Gets are safe.
func TestGetConcurrent(t *testing.T) {
	up := &fakeUpstream{prices: map[string]int64{"A1": 1299, "B2": 500}}
	clk := &clock{t: time.Unix(1_700_000_000, 0)}
	c := New(up, time.Minute, 10*time.Second, clk.Now)
	var wg sync.WaitGroup
	for i := range 50 {
		sku := []string{"A1", "B2"}[i%2]
		wg.Go(func() {
			if _, err := c.Get(context.Background(), sku); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
}
