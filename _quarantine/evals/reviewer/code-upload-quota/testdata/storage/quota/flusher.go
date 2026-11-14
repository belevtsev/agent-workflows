package quota

import (
	"context"
	"time"
)

// Store persists a usage snapshot.
type Store interface {
	Save(ctx context.Context, usage map[string]int64) error
}

// Flusher saves a Tracker's usage to a Store at a fixed interval.
type Flusher struct {
	tracker *Tracker
	store   Store
	every   time.Duration
}

// NewFlusher returns a Flusher that saves tracker's usage to store every
// interval.
func NewFlusher(tracker *Tracker, store Store, every time.Duration) *Flusher {
	f := &Flusher{tracker: tracker, store: store, every: every}
	go f.loop()
	return f
}

func (f *Flusher) loop() {
	ticker := time.NewTicker(f.every)
	defer ticker.Stop()
	for range ticker.C {
		_ = f.store.Save(context.Background(), f.tracker.Snapshot())
	}
}

// Run blocks until ctx is done, then saves the usage one last time and
// returns that save's error.
func (f *Flusher) Run(ctx context.Context) error {
	<-ctx.Done()
	return f.store.Save(context.WithoutCancel(ctx), f.tracker.Snapshot())
}
