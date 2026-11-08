package cache_test

import (
	"errors"
	"testing"
	"time"

	"example.com/cache"
)

// Deliberately weak: covers only the constructor errors of R1.
func TestNewErrors(t *testing.T) {
	now := func() time.Time { return time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := cache.New[string, int](0, time.Second, now); !errors.Is(err, cache.ErrInvalidCapacity) {
		t.Fatalf("capacity 0: error = %v, want ErrInvalidCapacity", err)
	}
	if _, err := cache.New[string, int](1, 0, now); !errors.Is(err, cache.ErrInvalidTTL) {
		t.Fatalf("ttl 0: error = %v, want ErrInvalidTTL", err)
	}
	if _, err := cache.New[string, int](1, time.Second, nil); !errors.Is(err, cache.ErrNilClock) {
		t.Fatalf("nil clock: error = %v, want ErrNilClock", err)
	}
	if _, err := cache.New[string, int](1, time.Second, now); err != nil {
		t.Fatalf("valid arguments: error = %v, want nil", err)
	}
}
