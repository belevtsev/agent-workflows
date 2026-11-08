# Interface contract: package `memo`

A memoising cache in front of a loader function. Language: Go. Module
`example.com/memo`, package `memo` at the module root.

## Public interface

```go
type Cache struct{ /* unexported fields */ }

func New(load func(key string) (string, error)) *Cache
func (c *Cache) Get(key string) (string, error)
```

## Requirements

- R1: `Get(key)` on a key with no cached value calls `load(key)` and returns
  its value with a nil error. A successful value is cached: every later
  `Get(key)` returns it without calling `load` again.
- R2: When `load(key)` returns a non-nil error, `Get` returns `""` and that
  error (`errors.Is` matches it), and nothing is cached: the next `Get(key)`
  calls `load(key)` again.
- R3: While a call of `load(key)` is in flight, every other `Get(key)` waits
  for it instead of calling `load`, and returns the same value and error when
  it completes. For one key, at most one call of `load` runs at a time, and a
  burst of overlapping `Get(key)` calls makes exactly one call of `load`.
- R4: All methods are safe for concurrent use; Gets for different keys do not
  wait for each other's loads.

## Scope

- No eviction, no expiry, no context or cancellation, no `Delete`.
- `load` is never nil; `New(nil)` is out of scope.
