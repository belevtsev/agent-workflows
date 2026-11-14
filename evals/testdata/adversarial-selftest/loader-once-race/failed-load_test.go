package memo

import (
	"errors"
	"testing"
	"time"
)

// Fixture probe, not a kept-file self-test: R2 and R3. A Get that overlaps a
// failing load of its key waits for it and returns what the Get that called
// load returns, "" with the error. Fails on the seeded source, where the
// second Get calls load again, and must pass after fix.patch.
func TestRefOverlappingFailedLoadReturnsEmpty(t *testing.T) {
	errBoom := errors.New("boom")
	started := make(chan struct{}, 2)
	release := make(chan struct{})
	c := New(func(key string) (string, error) {
		started <- struct{}{}
		<-release
		return "partial", errBoom
	})

	type result struct {
		v   string
		err error
	}
	results := make(chan result, 2)
	get := func() {
		v, err := c.Get("k")
		results <- result{v, err}
	}
	go get()
	<-started // the first load is now in flight, blocked on release
	go get()

	select {
	case <-started:
		close(release)
		<-results
		<-results
		t.Fatalf("R3 violated: a second load of %q started while the first was in flight", "k")
	case <-time.After(time.Second): // bounds the wait for a second load only
	}
	close(release)
	for i := 0; i < 2; i++ {
		r := <-results
		if r.v != "" || !errors.Is(r.err, errBoom) {
			t.Errorf("Get(k) = %q, %v; want \"\", boom (R2, R3)", r.v, r.err)
		}
	}
}
