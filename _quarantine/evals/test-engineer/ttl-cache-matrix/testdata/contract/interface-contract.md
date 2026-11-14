# Interface contract: package `cache`

A bounded in-memory cache with least-recently-used eviction and a
time-to-live per entry. Language: Go. Module `example.com/cache`, package
`cache`. Generic over comparable keys and any values.

## Requirements

- R1: `New[K, V](capacity, ttl, clock)` returns `ErrInvalidCapacity` when
  `capacity < 1`, `ErrInvalidTTL` when `ttl <= 0`, `ErrNilClock` when
  `clock` is nil.
- R2: `Set(k, v)` stores `v` under `k` with expiry `clock() + ttl`,
  replacing any existing entry and its expiry, and marks `k` most recently
  used.
- R3: `Get(k)` returns `(v, true)` for a live entry and marks it most
  recently used; it returns `(zero, false)` for a missing or expired entry
  and removes an expired one.
- R4: When `Set` adds a new key to a cache holding `capacity` live entries,
  the least recently used live entry is evicted first; expired entries are
  evicted before any live one.
- R5: `Delete(k)` removes the entry and returns whether a live entry was
  present.
- R6: `Len` returns the number of live entries; it never exceeds `capacity`.
- R7: An entry is expired when `clock()` is at or after its expiry. Time
  comes only from `clock`.
- R8: All methods are safe for concurrent use.

## Scope

- No eviction callbacks, no per-entry TTL, no size in bytes.
