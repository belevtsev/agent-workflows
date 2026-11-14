// Package memo is a memoising cache in front of a loader function.
package memo

import "sync"

// call is one in-flight or completed load of a key.
type call struct {
	done chan struct{}
	val  string
	err  error
}

// Cache memoises the successful results of a loader function.
type Cache struct {
	load func(key string) (string, error)

	mu       sync.RWMutex
	vals     map[string]string
	inflight map[string]*call
}

// New returns a cache that loads missing keys with load.
func New(load func(key string) (string, error)) *Cache {
	return &Cache{
		load:     load,
		vals:     make(map[string]string),
		inflight: make(map[string]*call),
	}
}

// Get returns the value for key, calling load on a miss.
func (c *Cache) Get(key string) (string, error) {
	c.mu.RLock()
	v, ok := c.vals[key]
	c.mu.RUnlock()
	if ok {
		return v, nil
	}

	c.mu.Lock()
	if v, ok := c.vals[key]; ok {
		c.mu.Unlock()
		return v, nil
	}
	if cl, ok := c.inflight[key]; ok {
		c.mu.Unlock()
		<-cl.done
		return cl.result()
	}
	cl := c.begin(key)
	c.mu.Unlock()

	cl.val, cl.err = c.load(key)
	c.finish(key, cl)
	if cl.err != nil {
		return "", cl.err
	}
	return cl.val, nil
}

// begin starts the in-flight call for key. The caller holds the write lock.
func (c *Cache) begin(key string) *call {
	cl := &call{done: make(chan struct{})}
	return cl
}

// finish records the outcome of cl, removes it from the in-flight map and
// wakes its waiters.
func (c *Cache) finish(key string, cl *call) {
	c.mu.Lock()
	if cl.err == nil {
		c.vals[key] = cl.val
	}
	delete(c.inflight, key)
	c.mu.Unlock()
	close(cl.done)
}

// result is what a Get that waited for cl returns: the loaded value, or ""
// with the error when the load failed.
func (cl *call) result() (string, error) {
	if cl.err != nil {
		return "", cl.err
	}
	return cl.val, nil
}
