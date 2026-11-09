package memo

import (
	"sync"
	"testing"
	"time"
)

// Self-test fixture (AG4): one probe that fails on the seeded source and one
// test that passes on it. TestGet is a prefix of the probe's name, so the gate
// only reports it as passing when it runs each kept test alone (-run '^Name$').

// R3 and DR1: the reference probe, renamed. Fails when a second Get for a key
// whose load is in flight calls load again instead of waiting for it.
func TestGetOverlappingLoadsOnce(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	c := New(func(key string) (string, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		started <- key
		<-release
		return "v:" + key, nil
	})

	var wg sync.WaitGroup
	results := make([]string, 2)
	get := func(i int) {
		defer wg.Done()
		v, err := c.Get("k")
		if err != nil {
			t.Errorf("Get #%d: %v", i, err)
		}
		results[i] = v
	}
	wg.Add(1)
	go get(0)
	<-started // the first load is now in flight, blocked on release
	wg.Add(1)
	go get(1)

	select {
	case <-started:
		close(release)
		wg.Wait()
		t.Fatalf("R3/DR1 violated: a second load of %q started while the first was in flight", "k")
	case <-time.After(time.Second): // bounds the wait for a second load only
	}
	close(release)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 || results[0] != "v:k" || results[1] != "v:k" {
		t.Fatalf("calls = %d, results = %q; want 1 call and both %q", calls, results, "v:k")
	}
}

// R1, sequential Gets only: behaviour the seeded source already has, so the
// gate must name this test. Fails when a cached value is loaded again.
func TestGet(t *testing.T) {
	calls := 0
	c := New(func(key string) (string, error) {
		calls++
		return "v:" + key, nil
	})
	for i := 0; i < 2; i++ {
		v, err := c.Get("k")
		if err != nil || v != "v:k" {
			t.Fatalf("Get #%d = %q, %v; want %q, nil", i, v, err, "v:k")
		}
	}
	if calls != 1 {
		t.Fatalf("load called %d times for two sequential Gets, want 1", calls)
	}
}
