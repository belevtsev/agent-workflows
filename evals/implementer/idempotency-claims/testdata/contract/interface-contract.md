# Interface contract: package `idem`

An in-memory store of idempotency keys with a time-to-live, safe for
concurrent use. Language: Go. Module `example.com/idem`, package `idem`.

## Public interface

```go
package idem

var ErrInvalidTTL = errors.New("idem: ttl must be positive")
var ErrNilClock = errors.New("idem: clock must not be nil")
var ErrEmptyKey = errors.New("idem: empty key")
var ErrDuplicate = errors.New("idem: key already claimed")
var ErrNotFound = errors.New("idem: key not claimed")

type Claim struct {
	Key       string
	ExpiresAt time.Time
}

type Store struct { /* unexported */ }

func New(clock func() time.Time, ttl time.Duration) (*Store, error)
func (s *Store) Claim(key string) (Claim, error)
func (s *Store) Release(key string) error
func (s *Store) Len() int
func (s *Store) Sweep() int
```

## Requirements

- R1: `New` returns `ErrInvalidTTL` when `ttl <= 0` and `ErrNilClock` when
  `clock` is nil.
- R2: `Claim("")` returns `ErrEmptyKey`.
- R3: The first `Claim(key)` succeeds and returns `Claim{Key: key,
  ExpiresAt: clock() + ttl}` using the clock value read during that call.
- R4: A second `Claim(key)` while the first is unexpired returns
  `ErrDuplicate`. A claim is expired when `clock()` is at or after `ExpiresAt`.
- R5: `Claim(key)` after the earlier claim expired succeeds and replaces it.
- R6: `Release(key)` frees an unexpired claim so the next `Claim` succeeds;
  for an unknown or expired key it returns `ErrNotFound`.
- R7: All methods are safe for concurrent use. Of N concurrent `Claim` calls
  for the same key, exactly one succeeds.
- R8: `Len` returns the number of unexpired claims. `Sweep` removes every
  expired claim and returns how many it removed. No background goroutine
  runs; expiry is evaluated against `clock()` on each call.

## Scope

- No persistence, no metrics, no per-key TTL.
