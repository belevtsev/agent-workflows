# Design: upload quota

Change `add-upload-quota`. Decision record for the per-tenant upload quota.

## Context

Uploads must stop when a tenant reaches its plan's storage limit. Limits come
from the billing service through `LimitSource`. Usage is kept in memory and
persisted so a restart does not reset it.

## Decision

Two components in package `quota`:

- `Tracker` holds usage in a map guarded by one mutex. It is passive: it
  starts no goroutines, owns no timers and does no I/O except the
  `LimitSource.Limit` call in `Reserve`.
- `Flusher` persists `Tracker.Snapshot()` through `Store`. All of its work
  happens inside `Run(ctx)`, which owns the ticker. `NewFlusher` only stores
  its arguments.

## Ownership and lifecycle

| Resource | Owner | Started | Stopped |
| --- | --- | --- | --- |
| usage map and mutex | `Tracker` | `NewTracker` | garbage collected |
| persistence ticker and its goroutine | `Flusher.Run` | when the server's errgroup calls `Run` | when `Run` returns, after `ctx` is done |
| `Flusher.Run` call | `cmd/server` (outside this change) | at startup, in the server's errgroup | on shutdown, by cancelling `ctx`; the server waits for `Run` to return before exiting |

Nothing in `quota` may outlive the server's errgroup: shutdown cancels the
context, `Run` makes its final save and returns, and no goroutine or ticker
from this package remains.

## Failure behaviour

| Boundary | Failure | Behaviour |
| --- | --- | --- |
| `LimitSource.Limit` | any error, including a timeout | `Reserve` fails closed: returns the wrapped error, reserves nothing |
| `Store.Save` during `Run` | error | logged by the caller's store wrapper; the next tick retries |
| `Store.Save` after `ctx` is done | error | `Run` returns it, so the server reports a lost final save |

## Options considered

1. Persist on every `Reserve` and `Release`. Rejected: one store write per
   upload.
2. A background goroutine started by the tracker. Rejected: its lifetime
   would not be tied to the server's shutdown.
3. **Chosen:** a `Flusher` whose `Run` the server owns.
