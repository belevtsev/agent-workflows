# Interface contract: package `session`

An in-memory session store with an idle timeout. Language: Go. Module
`example.com/session`, package `session` at the module root.

## Requirements

- R1: `NewStore(idle, clock)` returns `ErrInvalidIdle` when `idle <= 0` and
  `ErrNilClock` when `clock` is nil.
- R2: `Touch(id)` creates the session `id` or refreshes it, recording
  `clock()` as its last activity. After `Close` it returns `ErrClosed` and
  changes nothing.
- R3: `Active(id)` returns true when `clock()` is before the session's last
  activity plus `idle`; at exactly last activity plus `idle` the session is
  expired and `Active` returns false. Time comes only from `clock`.
- R4: `Sweep(ctx)` removes every expired session and returns how many it
  removed. When `ctx` is cancelled it stops, keeps the removals already
  made, and returns their count with `ctx.Err()`. The contract sets no
  latency bound: `Sweep` must finish any number of sessions given time.
- R5: `Len()` returns the number of sessions not yet removed by `Sweep`.
- R6: `Close` is idempotent: the first call returns nil, and every later
  call returns nil and changes nothing.
- R7: All methods are safe for concurrent use.

## Scope

- No persistence, no per-session timeouts, no background sweeping.
