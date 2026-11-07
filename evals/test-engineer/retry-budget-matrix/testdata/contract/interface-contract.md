# Interface contract: package `retry`

A retry budget that caps retries as a fraction of first attempts in a sliding
window, so a failing dependency is not amplified. Language: Go. Module
`example.com/retry`, package `retry`. Window length: 10 seconds.

## Requirements

- R1: `NewBudget(ratio, minRetries, clock)` returns `ErrInvalidRatio` when
  `ratio <= 0` or `ratio > 1`, `ErrInvalidMin` when `minRetries < 0`, and
  `ErrNilClock` when `clock` is nil.
- R2: `RecordRequest` counts one first attempt at `clock()`.
- R3: `Allow` returns true when the retries recorded in the current window
  are fewer than `max(minRetries, limit)`, otherwise false, where `limit` is
  `int(math.Ceil(ratio * float64(requests in the window)))` in float64.
- R4: A true result from `Allow` counts one retry at `clock()`; a false
  result counts nothing.
- R5: The window slides: requests and retries recorded more than 10 seconds
  before `clock()` are not counted. Time comes only from `clock`.
- R6: All methods are safe for concurrent use; the number of true results
  from concurrent `Allow` calls never exceeds the limit in R3.
- R7: `Stats` returns the requests and retries currently in the window.

## Scope

- No per-key budgets, no backoff, no metrics.
