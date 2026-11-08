# Interface contract: package `ratelimit`

A per-key fixed-window rate limiter. Language: Go. Module
`example.com/ratelimit`, package `ratelimit` at the module root.

## Requirements

- R1: `New(limit, window, clock)` returns `ErrInvalidLimit` when
  `limit < 1`, `ErrInvalidWindow` when `window <= 0`, and `ErrNilClock`
  when `clock` is nil.
- R2: `Allow(key)` returns true for the first `limit` calls for `key` in
  its current window and false afterwards; a false result is not counted.
- R3: Keys are independent: calls for one key never change the result for
  another key.
- R4: A key's window starts at its first counted call. When `clock()` is at
  or after the window start plus `window`, the count resets and a new
  window starts at that call. Time comes only from `clock`.
- R5: `Count(key)` returns the number of true results for `key` in its
  current window, and 0 for an unknown key or an elapsed window.
- R6: All methods are safe for concurrent use; concurrent `Allow` calls for
  one key return true at most `limit` times per window.

## Scope

- No sliding windows, no per-key limits, no eviction of idle keys.
