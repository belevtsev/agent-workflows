package quota

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type fixedLimits struct {
	limit int64
	err   error
}

func (f fixedLimits) Limit(context.Context, string) (int64, error) { return f.limit, f.err }

// R1: a reservation within the limit is recorded.
func TestReserveWithinLimit(t *testing.T) {
	tr := NewTracker(fixedLimits{limit: 100})
	if err := tr.Reserve(t.Context(), "acme", 60); err != nil {
		t.Fatalf("Reserve(60) = %v, want nil", err)
	}
	if err := tr.Reserve(t.Context(), "acme", 40); err != nil {
		t.Fatalf("Reserve(40) up to the limit = %v, want nil", err)
	}
	if got := tr.Usage("acme"); got != 100 {
		t.Fatalf("Usage = %d, want 100", got)
	}
}

// R2: a reservation past the limit fails and leaves usage unchanged.
func TestReservePastLimit(t *testing.T) {
	tr := NewTracker(fixedLimits{limit: 100})
	if err := tr.Reserve(t.Context(), "acme", 80); err != nil {
		t.Fatal(err)
	}
	if err := tr.Reserve(t.Context(), "acme", 21); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("Reserve(21) = %v, want ErrQuotaExceeded", err)
	}
	if got := tr.Usage("acme"); got != 80 {
		t.Fatalf("Usage = %d, want 80", got)
	}
}

// R4: release gives bytes back.
func TestRelease(t *testing.T) {
	tr := NewTracker(fixedLimits{limit: 100})
	if err := tr.Reserve(t.Context(), "acme", 70); err != nil {
		t.Fatal(err)
	}
	tr.Release("acme", 30)
	if got := tr.Usage("acme"); got != 40 {
		t.Fatalf("Usage = %d, want 40", got)
	}
}

// R5: a limit-source failure fails the reservation closed.
func TestReserveLimitSourceError(t *testing.T) {
	down := errors.New("limits service down")
	tr := NewTracker(fixedLimits{err: down})
	if err := tr.Reserve(t.Context(), "acme", 1); !errors.Is(err, down) {
		t.Fatalf("Reserve = %v, want an error wrapping %v", err, down)
	}
	if got := tr.Usage("acme"); got != 0 {
		t.Fatalf("Usage = %d, want 0", got)
	}
}

type recordingStore struct {
	saves chan map[string]int64
}

func (s recordingStore) Save(_ context.Context, usage map[string]int64) error {
	s.saves <- usage
	return nil
}

// R6: Run saves on every interval and once more when its context ends.
func TestFlusherSavesPeriodicallyAndOnShutdown(t *testing.T) {
	tr := NewTracker(fixedLimits{limit: 100})
	if err := tr.Reserve(t.Context(), "acme", 5); err != nil {
		t.Fatal(err)
	}
	store := recordingStore{saves: make(chan map[string]int64, 64)}
	f := NewFlusher(tr, store, time.Millisecond)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- f.Run(ctx) }()
	for range 2 {
		if got := <-store.saves; got["acme"] != 5 {
			t.Fatalf("periodic save = %v, want acme=5", got)
		}
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

// R7: concurrent reservations never pass the limit.
func TestConcurrentReserve(t *testing.T) {
	tr := NewTracker(fixedLimits{limit: 50})
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() { _ = tr.Reserve(context.Background(), "acme", 1) })
	}
	wg.Wait()
	if got := tr.Usage("acme"); got != 50 {
		t.Fatalf("Usage = %d, want 50", got)
	}
}
