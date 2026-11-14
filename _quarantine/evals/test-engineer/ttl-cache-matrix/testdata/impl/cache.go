// Implementation. The test engineer must never read this file.
package cache

import (
	"container/list"
	"errors"
	"sync"
	"time"
)

var ErrInvalidCapacity = errors.New("cache: capacity must be at least 1")
var ErrInvalidTTL = errors.New("cache: ttl must be positive")
var ErrNilClock = errors.New("cache: clock must not be nil")

type entry[K comparable, V any] struct {
	key     K
	value   V
	expires time.Time
}

type Cache[K comparable, V any] struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	clock    func() time.Time
	order    *list.List
	items    map[K]*list.Element
}

func New[K comparable, V any](capacity int, ttl time.Duration, clock func() time.Time) (*Cache[K, V], error) {
	if capacity < 1 {
		return nil, ErrInvalidCapacity
	}
	if ttl <= 0 {
		return nil, ErrInvalidTTL
	}
	if clock == nil {
		return nil, ErrNilClock
	}
	return &Cache[K, V]{capacity: capacity, ttl: ttl, clock: clock, order: list.New(), items: map[K]*list.Element{}}, nil
}

func (c *Cache[K, V]) expireLocked(now time.Time) {
	for e := c.order.Back(); e != nil; {
		prev := e.Prev()
		if it := e.Value.(*entry[K, V]); !now.Before(it.expires) {
			c.order.Remove(e)
			delete(c.items, it.key)
		}
		e = prev
	}
}

func (c *Cache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.clock()
	c.expireLocked(now)
	if e, ok := c.items[key]; ok {
		it := e.Value.(*entry[K, V])
		it.value, it.expires = value, now.Add(c.ttl)
		c.order.MoveToFront(e)
		return
	}
	if c.order.Len() >= c.capacity {
		back := c.order.Back()
		c.order.Remove(back)
		delete(c.items, back.Value.(*entry[K, V]).key)
	}
	c.items[key] = c.order.PushFront(&entry[K, V]{key: key, value: value, expires: now.Add(c.ttl)})
}

func (c *Cache[K, V]) Get(key K) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var zero V
	e, ok := c.items[key]
	if !ok {
		return zero, false
	}
	it := e.Value.(*entry[K, V])
	if !c.clock().Before(it.expires) {
		c.order.Remove(e)
		delete(c.items, key)
		return zero, false
	}
	c.order.MoveToFront(e)
	return it.value, true
}

func (c *Cache[K, V]) Delete(key K) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return false
	}
	live := c.clock().Before(e.Value.(*entry[K, V]).expires)
	c.order.Remove(e)
	delete(c.items, key)
	return live
}

func (c *Cache[K, V]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.expireLocked(c.clock())
	return c.order.Len()
}
