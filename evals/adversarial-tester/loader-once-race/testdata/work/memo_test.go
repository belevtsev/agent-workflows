package memo_test

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"example.com/memo"
)

// countingLoader returns "v:<key>" and counts calls per key.
type countingLoader struct {
	mu    sync.Mutex
	calls map[string]int
}

func newCountingLoader() *countingLoader {
	return &countingLoader{calls: make(map[string]int)}
}

func (l *countingLoader) load(key string) (string, error) {
	l.mu.Lock()
	l.calls[key]++
	l.mu.Unlock()
	return "v:" + key, nil
}

func (l *countingLoader) count(key string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.calls[key]
}

// R1: fails if Get does not return load's value on a miss.
func TestGetReturnsLoadedValue(t *testing.T) {
	l := newCountingLoader()
	c := memo.New(l.load)
	got, err := c.Get("a")
	if err != nil || got != "v:a" {
		t.Fatalf("Get(a) = %q, %v; want %q, nil", got, err, "v:a")
	}
	if n := l.count("a"); n != 1 {
		t.Fatalf("load(a) called %d times, want 1", n)
	}
}

// R1: fails if a successful value is not cached.
func TestGetCachesValue(t *testing.T) {
	l := newCountingLoader()
	c := memo.New(l.load)
	for i := 0; i < 3; i++ {
		got, err := c.Get("a")
		if err != nil || got != "v:a" {
			t.Fatalf("Get(a) #%d = %q, %v; want %q, nil", i, got, err, "v:a")
		}
	}
	if n := l.count("a"); n != 1 {
		t.Fatalf("load(a) called %d times over three sequential Gets, want 1", n)
	}
}

// R2: fails if an error is not returned, or if it is cached.
func TestGetErrorIsNotCached(t *testing.T) {
	errBoom := errors.New("boom")
	calls := 0
	c := memo.New(func(key string) (string, error) {
		calls++
		if calls == 1 {
			return "ignored", errBoom
		}
		return "v:" + key, nil
	})
	got, err := c.Get("a")
	if !errors.Is(err, errBoom) || got != "" {
		t.Fatalf("first Get(a) = %q, %v; want \"\", boom", got, err)
	}
	got, err = c.Get("a")
	if err != nil || got != "v:a" {
		t.Fatalf("second Get(a) = %q, %v; want %q, nil", got, err, "v:a")
	}
	if calls != 2 {
		t.Fatalf("load called %d times, want 2 (the error must not be cached)", calls)
	}
}

// R1: fails if keys share cached values.
func TestGetKeysAreIndependent(t *testing.T) {
	l := newCountingLoader()
	c := memo.New(l.load)
	for _, k := range []string{"a", "b", "c"} {
		got, err := c.Get(k)
		if err != nil || got != "v:"+k {
			t.Fatalf("Get(%s) = %q, %v; want %q, nil", k, got, err, "v:"+k)
		}
	}
	for _, k := range []string{"a", "b", "c"} {
		if n := l.count(k); n != 1 {
			t.Fatalf("load(%s) called %d times, want 1", k, n)
		}
	}
}

// R4: fails on a data race or a wrong value when many goroutines read
// cached keys and load distinct new keys at once.
func TestGetConcurrentUse(t *testing.T) {
	l := newCountingLoader()
	c := memo.New(l.load)
	const warm = 8
	for i := 0; i < warm; i++ {
		if _, err := c.Get(fmt.Sprintf("warm%d", i)); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for g := 0; g < 32; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			// Each goroutine owns one fresh key, so no two Gets share a key.
			own := fmt.Sprintf("own%d", g)
			if got, err := c.Get(own); err != nil || got != "v:"+own {
				errs <- fmt.Errorf("Get(%s) = %q, %v", own, got, err)
				return
			}
			for i := 0; i < warm; i++ {
				k := fmt.Sprintf("warm%d", i)
				if got, err := c.Get(k); err != nil || got != "v:"+k {
					errs <- fmt.Errorf("Get(%s) = %q, %v", k, got, err)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	for i := 0; i < warm; i++ {
		k := fmt.Sprintf("warm%d", i)
		if n := l.count(k); n != 1 {
			t.Errorf("load(%s) called %d times, want 1 (it was cached before the goroutines started)", k, n)
		}
	}
}
