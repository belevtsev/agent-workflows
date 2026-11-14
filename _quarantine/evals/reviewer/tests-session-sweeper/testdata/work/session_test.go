package session_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"example.com/session"
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
	return &fakeClock{now: time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)}
}

// R1: invalid arguments fail with the documented errors.
func TestNewStoreRejectsInvalidArguments(t *testing.T) {
	clock := newClock().Now
	if _, err := session.NewStore(0, clock); !errors.Is(err, session.ErrInvalidIdle) {
		t.Fatalf("NewStore(0) error = %v, want ErrInvalidIdle", err)
	}
	if _, err := session.NewStore(-time.Second, clock); !errors.Is(err, session.ErrInvalidIdle) {
		t.Fatalf("NewStore(-1s) error = %v, want ErrInvalidIdle", err)
	}
	if _, err := session.NewStore(time.Second, nil); !errors.Is(err, session.ErrNilClock) {
		t.Fatalf("NewStore(nil clock) error = %v, want ErrNilClock", err)
	}
}

// R2, R3: a touched session is active before its idle timeout.
func TestTouchMakesActive(t *testing.T) {
	clock := newClock()
	s, err := session.NewStore(time.Minute, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Touch("a"); err != nil {
		t.Fatal(err)
	}
	clock.Advance(59 * time.Second)
	if !s.Active("a") {
		t.Fatal("Active(a) = false 59s after Touch with a 1m idle timeout, want true")
	}
}

// R3: a session expires after its idle timeout.
func TestActiveExpiresAfterIdle(t *testing.T) {
	s, err := session.NewStore(20*time.Millisecond, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Touch("a"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if s.Active("a") {
		t.Fatal("Active(a) = true after the idle timeout, want false")
	}
}

// R4, R5: Sweep removes every expired session.
func TestSweepRemovesExpired(t *testing.T) {
	const n = 200_000
	clock := newClock()
	s, err := session.NewStore(time.Minute, clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := s.Touch(fmt.Sprintf("s%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	clock.Advance(2 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	removed, err := s.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep error = %v, want nil", err)
	}
	if removed != n {
		t.Fatalf("Sweep removed %d, want %d", removed, n)
	}
	if got := s.Len(); got != 0 {
		t.Fatalf("Len after Sweep = %d, want 0", got)
	}
}

// R6: Close succeeds.
func TestClose(t *testing.T) {
	s, err := session.NewStore(time.Minute, newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close error = %v, want nil", err)
	}
}

// R7: concurrent Touch calls create every session.
func TestConcurrentTouch(t *testing.T) {
	s, err := session.NewStore(time.Minute, newClock().Now)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.Touch(fmt.Sprintf("s%d", i)); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if got := s.Len(); got != 64 {
		t.Fatalf("Len = %d after 64 concurrent Touch calls, want 64", got)
	}
}
