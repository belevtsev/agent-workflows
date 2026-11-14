# Interface contract: webhook retries in package `hooks`

Language: Go. Module `example.com/hooks`, package `hooks`.

## Requirements

- R1: `NewRetrier(sender, clock, store)` returns `ErrNilSender`,
  `ErrNilClock` or `ErrNilStore` when the matching argument is nil.
- R2: `Retrier.Schedule(d)` for a delivery whose attempt failed with a
  network error or a 5xx response sends another attempt, up to 6 attempts
  in all, then marks the delivery `failed` in the store.
- R3: Retries back off so that a struggling endpoint is not overwhelmed.
- R4: A delivery whose attempt got a 4xx response other than 429 is marked
  `failed` without another attempt.
- R5: `Schedule` is safe for concurrent use; a second `Schedule` for an ID
  that already has a pending retry is ignored.

## Types and operations

```go
var ErrNilSender = errors.New("hooks: sender must not be nil")
var ErrNilClock = errors.New("hooks: clock must not be nil")
var ErrNilStore = errors.New("hooks: store must not be nil")

type Retrier struct{ /* unexported */ }

func NewRetrier(sender Sender, clock func() time.Time, store Store) (*Retrier, error)
func (r *Retrier) Schedule(d Delivery)
func (r *Retrier) Stop()
```

## Invariants

- At most one pending retry per delivery ID.
- No delivery gets more than 6 attempts.

## Out of scope

- Per-endpoint retry settings, dead-letter queues, manual redelivery.

## Open questions

None.
