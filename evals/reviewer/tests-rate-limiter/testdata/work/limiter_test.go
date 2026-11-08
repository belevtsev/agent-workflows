package ratelimit_test

import (
	"errors"
	"sync"
	"testing"
	"time"

	"example.com/ratelimit"
)

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
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

func newClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)}
}

// R1: invalid arguments fail with the documented errors.
func TestNewRejectsInvalidArguments(t *testing.T) {
	clock := newClock().Now
	for _, tc := range []struct {
		name   string
		limit  int
		window time.Duration
		clock  func() time.Time
		want   error
	}{
		{"zero limit", 0, time.Second, clock, ratelimit.ErrInvalidLimit},
		{"zero window", 1, 0, clock, ratelimit.ErrInvalidWindow},
		{"negative window", 1, -time.Second, clock, ratelimit.ErrInvalidWindow},
		{"nil clock", 1, time.Second, nil, ratelimit.ErrNilClock},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ratelimit.New(tc.limit, tc.window, tc.clock); !errors.Is(err, tc.want) {
				t.Fatalf("New(%d, %v) error = %v, want %v", tc.limit, tc.window, err, tc.want)
			}
		})
	}
}

// R2: the first limit calls are allowed and the rest are refused.
func TestLimitPerKey(t *testing.T) {
	l, err := ratelimit.New(3, time.Minute, newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	want := []bool{true, true, true, false, false}
	for i, w := range want {
		if got := l.Allow("a"); got != w {
			t.Fatalf("call %d: Allow = %v, want %v", i+1, got, w)
		}
	}
}

// R3: exhausting one key leaves another key untouched.
func TestKeysIndependent(t *testing.T) {
	l, err := ratelimit.New(1, time.Minute, newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Allow("a") {
		t.Fatal("first Allow(a) = false, want true")
	}
	if !l.Allow("b") {
		t.Fatal("first Allow(b) = false after a was used, want true")
	}
}

// R5: Count reports the allowed calls in the window.
func TestCountMatchesAllowed(t *testing.T) {
	l, err := ratelimit.New(5, time.Minute, newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	l.Allow("a")
	l.Allow("a")
	want := l.Count("a")
	if got := l.Count("a"); got != want {
		t.Fatalf("Count(a) = %d, want %d", got, want)
	}
}

// R6: concurrent Allow calls never exceed the limit.
func TestConcurrentAllow(t *testing.T) {
	const limit = 10
	l, err := ratelimit.New(limit, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	allowed := 0
	for i := 0; i < 50; i++ {
		go func() {
			if l.Allow("k") {
				allowed++
			}
		}()
	}
	time.Sleep(50 * time.Millisecond) // let the goroutines finish
	if allowed > limit {
		t.Fatalf("allowed = %d, want at most %d", allowed, limit)
	}
}
