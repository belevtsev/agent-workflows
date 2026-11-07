package cache_test

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"example.com/cache"
)

// fakeClock is the injected clock; tests advance it instead of sleeping.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

const ttl = 10 * time.Second

func mustNew(t *testing.T, capacity int, clk func() time.Time) *cache.Cache[string, string] {
	t.Helper()
	c, err := cache.New[string, string](capacity, ttl, clk)
	if err != nil {
		t.Fatalf("New(%d, %v): unexpected error %v", capacity, ttl, err)
	}
	if c == nil {
		t.Fatalf("New(%d, %v) returned a nil cache with a nil error", capacity, ttl)
	}
	return c
}

func wantGet(t *testing.T, c *cache.Cache[string, string], key, want string) {
	t.Helper()
	got, ok := c.Get(key)
	if !ok || got != want {
		t.Fatalf("Get(%q) = (%q, %v), want (%q, true)", key, got, ok, want)
	}
}

func wantMissing(t *testing.T, c *cache.Cache[string, string], key string) {
	t.Helper()
	got, ok := c.Get(key)
	if ok || got != "" {
		t.Fatalf("Get(%q) = (%q, %v), want (\"\", false)", key, got, ok)
	}
}

func wantLen(t *testing.T, c *cache.Cache[string, string], want int) {
	t.Helper()
	if got := c.Len(); got != want {
		t.Fatalf("Len() = %d, want %d", got, want)
	}
}

// R1: constructor errors.
func TestNewRejectsInvalidArguments_R1(t *testing.T) {
	clk := newClock()
	cases := []struct {
		name     string
		capacity int
		ttl      time.Duration
		clock    func() time.Time
		want     error
	}{
		{"capacity zero", 0, time.Second, clk.Now, cache.ErrInvalidCapacity},
		{"capacity negative", -1, time.Second, clk.Now, cache.ErrInvalidCapacity},
		{"ttl zero", 1, 0, clk.Now, cache.ErrInvalidTTL},
		{"ttl negative", 1, -time.Nanosecond, clk.Now, cache.ErrInvalidTTL},
		{"nil clock", 1, time.Second, nil, cache.ErrNilClock},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := cache.New[string, int](tc.capacity, tc.ttl, tc.clock)
			if !errors.Is(err, tc.want) {
				t.Fatalf("New(%d, %v, clock) error = %v, want %v", tc.capacity, tc.ttl, err, tc.want)
			}
		})
	}
}

// R1: the smallest valid arguments are accepted.
func TestNewAcceptsSmallestValidArguments_R1(t *testing.T) {
	clk := newClock()
	c, err := cache.New[string, int](1, time.Nanosecond, clk.Now)
	if err != nil || c == nil {
		t.Fatalf("New(1, 1ns, clock) = (%v, %v), want a cache and nil error", c, err)
	}
}

// R2: Set stores a value and replaces an existing one.
func TestSetStoresAndReplaces_R2(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	wantGet(t, c, "a", "1")
	c.Set("a", "2")
	wantGet(t, c, "a", "2")
	wantLen(t, c, 1)
}

// R2: Set replaces the expiry with clock() + ttl.
func TestSetReplacesExpiry_R2(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1") // expires at t0+10s
	clk.Advance(6 * time.Second)
	c.Set("a", "2")              // expires at t0+16s
	clk.Advance(6 * time.Second) // t0+12s: past the first expiry
	wantGet(t, c, "a", "2")
	clk.Advance(4 * time.Second) // t0+16s: at the new expiry
	wantMissing(t, c, "a")
}

// R2: Set on an existing key marks it most recently used.
func TestSetMarksMostRecentlyUsed_R2(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("a", "3") // a is now most recently used, b least
	c.Set("c", "4") // evicts b
	wantMissing(t, c, "b")
	wantGet(t, c, "a", "3")
	wantGet(t, c, "c", "4")
	wantLen(t, c, 2)
}

// R3: Get on a missing key returns the zero value and false.
func TestGetMissing_R3(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	wantMissing(t, c, "nope")
	ci, err := cache.New[int, int](1, ttl, clk.Now)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := ci.Get(7); ok || v != 0 {
		t.Fatalf("Get(7) = (%d, %v), want (0, false)", v, ok)
	}
}

// R3: Get marks the entry most recently used.
func TestGetMarksMostRecentlyUsed_R3(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	c.Set("b", "2")
	wantGet(t, c, "a", "1") // b is now least recently used
	c.Set("c", "3")
	wantMissing(t, c, "b")
	wantGet(t, c, "a", "1")
	wantGet(t, c, "c", "3")
}

// R3: Get on an expired entry returns the zero value, not the stale value.
func TestGetExpiredReturnsZero_R3(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "stale")
	clk.Advance(ttl)
	wantMissing(t, c, "a")
	wantMissing(t, c, "a")
	wantLen(t, c, 0)
}

// R4: the least recently used live entry is evicted first.
func TestEvictsLeastRecentlyUsed_R4(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 3, clk.Now)
	c.Set("a", "1")
	c.Set("b", "2")
	c.Set("c", "3")
	c.Set("d", "4")
	wantMissing(t, c, "a")
	wantGet(t, c, "b", "2")
	wantGet(t, c, "c", "3")
	wantGet(t, c, "d", "4")
	wantLen(t, c, 3)
}

// R4: an expired entry is evicted before a live one, even when the live one
// is the least recently used.
func TestEvictsExpiredBeforeLive_R4(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("b", "old") // t0, expires t0+10s
	clk.Advance(5 * time.Second)
	c.Set("a", "live") // t0+5s, expires t0+15s; LRU order: b, a
	wantGet(t, c, "b", "old")
	// LRU order now: a, b. Advance to b's expiry.
	clk.Advance(5 * time.Second) // t0+10s: b expired, a live and least recently used
	c.Set("c", "new")
	wantGet(t, c, "a", "live")
	wantGet(t, c, "c", "new")
	wantMissing(t, c, "b")
	wantLen(t, c, 2)
}

// R4: Set on an expired key adds a new entry and still respects capacity.
func TestSetExpiredKeyRespectsCapacity_R4(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("x", "1") // expires t0+10s
	clk.Advance(5 * time.Second)
	c.Set("y", "2") // expires t0+15s
	clk.Advance(5 * time.Second)
	c.Set("z", "3") // x expired: y and z live
	c.Set("x", "4") // re-adding x: y is the least recently used live entry
	wantLen(t, c, 2)
	wantMissing(t, c, "y")
	wantGet(t, c, "z", "3")
	wantGet(t, c, "x", "4")
}

// R5: Delete removes a live entry and reports whether one was present.
func TestDelete_R5(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	c.Set("b", "2")
	if !c.Delete("a") {
		t.Fatal("Delete(a) of a live entry = false, want true")
	}
	wantMissing(t, c, "a")
	wantLen(t, c, 1)
	if c.Delete("a") {
		t.Fatal("second Delete(a) = true, want false")
	}
	if c.Delete("never") {
		t.Fatal("Delete(never) = true, want false")
	}
	wantGet(t, c, "b", "2")
}

// R5: Delete of an expired entry reports that no live entry was present.
func TestDeleteExpired_R5(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	clk.Advance(ttl)
	if c.Delete("a") {
		t.Fatal("Delete(a) of an expired entry = true, want false")
	}
}

// R5: Delete frees capacity so a later Set evicts nothing.
func TestDeleteFreesCapacity_R5(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	c.Set("b", "2")
	c.Delete("b")
	c.Set("c", "3")
	wantGet(t, c, "a", "1")
	wantGet(t, c, "c", "3")
	wantLen(t, c, 2)
}

// R6: Len counts live entries only.
func TestLenCountsLiveEntries_R6(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 3, clk.Now)
	wantLen(t, c, 0)
	c.Set("a", "1") // expires t0+10s
	clk.Advance(5 * time.Second)
	c.Set("b", "2") // expires t0+15s
	wantLen(t, c, 2)
	clk.Advance(5 * time.Second) // a at expiry
	wantLen(t, c, 1)
	clk.Advance(5 * time.Second) // b at expiry
	wantLen(t, c, 0)
}

// R6: Len never exceeds capacity.
func TestLenNeverExceedsCapacity_R6(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 3, clk.Now)
	for i := 0; i < 100; i++ {
		c.Set(fmt.Sprintf("k%d", i), "v")
		if n := c.Len(); n > 3 {
			t.Fatalf("after %d sets Len() = %d, exceeds capacity 3", i+1, n)
		}
	}
	wantLen(t, c, 3)
	wantGet(t, c, "k99", "v")
	wantGet(t, c, "k98", "v")
	wantGet(t, c, "k97", "v")
	wantMissing(t, c, "k96")
}

// R7: an entry is live strictly before its expiry and expired at it.
func TestExpiryBoundary_R7(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 2, clk.Now)
	c.Set("a", "1")
	clk.Advance(ttl - time.Nanosecond)
	wantGet(t, c, "a", "1")
	wantLen(t, c, 1)
	clk.Advance(time.Nanosecond)
	wantLen(t, c, 0)
	wantMissing(t, c, "a")
}

// R7: time comes only from the injected clock (a frozen clock far in the past
// or future never expires anything).
func TestTimeOnlyFromClock_R7(t *testing.T) {
	for _, frozen := range []time.Time{
		time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC),
	} {
		at := frozen
		c, err := cache.New[string, string](2, time.Hour, func() time.Time { return at })
		if err != nil {
			t.Fatal(err)
		}
		c.Set("a", "1")
		wantGet(t, c, "a", "1")
		wantLen(t, c, 1)
	}
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
	c, err := cache.New[string, string](2, time.Nanosecond, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	c.Set("a", "1")
	wantGet(t, c, "a", "1")
}

// R8 and R6: concurrent use keeps Len within capacity and does not race.
func TestConcurrentUse_R8(t *testing.T) {
	clk := newClock()
	c := mustNew(t, 4, clk.Now)
	var over atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 300; i++ {
				key := fmt.Sprintf("k%d", (g*31+i)%16)
				switch i % 4 {
				case 0:
					c.Set(key, key)
				case 1:
					if v, ok := c.Get(key); ok && v != key {
						over.Add(1000)
					}
				case 2:
					c.Delete(key)
				case 3:
					if g == 0 {
						clk.Advance(time.Second)
					}
				}
				if c.Len() > 4 {
					over.Add(1)
				}
			}
		}(g)
	}
	wg.Wait()
	if n := over.Load(); n != 0 {
		t.Fatalf("concurrent use observed %d violations (Len above capacity 4 or a wrong value)", n)
	}
	if n := c.Len(); n > 4 {
		t.Fatalf("Len() = %d after concurrent use, exceeds capacity 4", n)
	}
}

// modelEntry is one entry of the in-test model.
type modelEntry struct {
	key    string
	value  string
	expiry time.Time
}

// model is an ordered list, least recently used first, of live entries.
type model struct {
	capacity int
	entries  []modelEntry
}

func (m *model) live(now time.Time) []modelEntry {
	var out []modelEntry
	for _, e := range m.entries {
		if now.Before(e.expiry) {
			out = append(out, e)
		}
	}
	return out
}

func (m *model) index(key string) int {
	for i, e := range m.entries {
		if e.key == key {
			return i
		}
	}
	return -1
}

func (m *model) set(now time.Time, key, value string) {
	m.entries = m.live(now)
	if i := m.index(key); i >= 0 {
		m.entries = append(m.entries[:i], m.entries[i+1:]...)
	} else if len(m.entries) >= m.capacity {
		m.entries = m.entries[1:]
	}
	m.entries = append(m.entries, modelEntry{key: key, value: value, expiry: now.Add(ttl)})
}

func (m *model) get(now time.Time, key string) (string, bool) {
	m.entries = m.live(now)
	i := m.index(key)
	if i < 0 {
		return "", false
	}
	e := m.entries[i]
	m.entries = append(m.entries[:i], m.entries[i+1:]...)
	m.entries = append(m.entries, e)
	return e.value, true
}

func (m *model) del(now time.Time, key string) bool {
	m.entries = m.live(now)
	i := m.index(key)
	if i < 0 {
		return false
	}
	m.entries = append(m.entries[:i], m.entries[i+1:]...)
	return true
}

// R2-R7 property: random sequences of Set, Get, Delete and clock advances
// agree with the model on every observable result, including eviction order.
func TestModelBasedEvictionOrder_R2_R3_R4_R5_R6_R7(t *testing.T) {
	steps := []time.Duration{0, time.Second, 3 * time.Second, 5 * time.Second, ttl - time.Nanosecond, ttl}
	keys := []string{"a", "b", "c", "d", "e"}
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		capacity := 1 + rng.Intn(4)
		clk := newClock()
		c := mustNew(t, capacity, clk.Now)
		m := &model{capacity: capacity}
		var trace []string
		for op := 0; op < 120; op++ {
			key := keys[rng.Intn(len(keys))]
			now := clk.Now()
			switch rng.Intn(5) {
			case 0, 1:
				value := fmt.Sprintf("v%d", op)
				trace = append(trace, fmt.Sprintf("Set(%s,%s)", key, value))
				c.Set(key, value)
				m.set(now, key, value)
			case 2:
				trace = append(trace, fmt.Sprintf("Get(%s)", key))
				gotV, gotOK := c.Get(key)
				wantV, wantOK := m.get(now, key)
				if gotV != wantV || gotOK != wantOK {
					t.Fatalf("seed %d capacity %d: Get(%s) = (%q, %v), model (%q, %v); trace %v", seed, capacity, key, gotV, gotOK, wantV, wantOK, trace)
				}
			case 3:
				trace = append(trace, fmt.Sprintf("Delete(%s)", key))
				got := c.Delete(key)
				want := m.del(now, key)
				if got != want {
					t.Fatalf("seed %d capacity %d: Delete(%s) = %v, model %v; trace %v", seed, capacity, key, got, want, trace)
				}
			case 4:
				d := steps[rng.Intn(len(steps))]
				trace = append(trace, fmt.Sprintf("Advance(%v)", d))
				clk.Advance(d)
			}
			if got, want := c.Len(), len(m.live(clk.Now())); got != want {
				t.Fatalf("seed %d capacity %d: Len() = %d, model %d; trace %v", seed, capacity, got, want, trace)
			}
		}
	}
}
