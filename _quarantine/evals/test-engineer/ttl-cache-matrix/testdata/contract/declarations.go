// Declaration extract for package cache (bodies stripped).
package cache

import (
	"errors"
	"time"
)

var ErrInvalidCapacity = errors.New("cache: capacity must be at least 1")
var ErrInvalidTTL = errors.New("cache: ttl must be positive")
var ErrNilClock = errors.New("cache: clock must not be nil")

type Cache[K comparable, V any] struct{}

func New[K comparable, V any](capacity int, ttl time.Duration, clock func() time.Time) (*Cache[K, V], error)
func (c *Cache[K, V]) Set(key K, value V)
func (c *Cache[K, V]) Get(key K) (V, bool)
func (c *Cache[K, V]) Delete(key K) bool
func (c *Cache[K, V]) Len() int
