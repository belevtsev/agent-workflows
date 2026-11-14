# Interface contract: webhook retries in package `hooks`

Language: Go. Module `example.com/hooks`, package `hooks`.

## Requirements

- R1: `NewSchedule(clock)` returns `ErrNilClock` when `clock` is nil.
- R2: `Schedule.Next(d, failure)` for a delivery whose attempt failed with a
  network error or a 5xx response returns the time of the next attempt, up
  to 6 attempts in all, and `Failed` after the sixth failed attempt.
- R3: The wait before attempts 2 to 6 is 1 s, 2 s, 4 s, 8 s and 16 s after
  the failed attempt, in that order.
- R4: A delivery whose attempt got a 4xx response other than 429 is `Failed`
  without another attempt.
- R5: A retry whose `next_attempt_at` passed while the service was down is
  sent by the first sweep after start-up.
- R6: `EndpointBreaker.Allow(endpointID)` returns false for 10 minutes after
  3 consecutive failed deliveries to that endpoint, and `Reset(endpointID)`
  makes it return true again.

## Types and operations

```go
var ErrNilClock = errors.New("hooks: clock must not be nil")

// Failure classifies a failed attempt.
type Failure int

const (
	NetworkError Failure = iota // network error or send timeout
	ServerError                 // 5xx or 429
	ClientError                 // 4xx other than 429
)

type Outcome struct {
	NextAttempt time.Time
	Failed      bool
}

type Schedule struct{ /* unexported */ }

func NewSchedule(clock func() time.Time) (*Schedule, error)
func (s *Schedule) Next(d Delivery, failure Failure) Outcome

type EndpointBreaker struct{ /* unexported */ }

func NewEndpointBreaker(clock func() time.Time) *EndpointBreaker
func (b *EndpointBreaker) RecordFailure(endpointID int64)
func (b *EndpointBreaker) RecordSuccess(endpointID int64)
func (b *EndpointBreaker) Allow(endpointID int64) bool
func (b *EndpointBreaker) Reset(endpointID int64)
```

## Invariants

- No delivery gets more than 6 attempts.

## Out of scope

- Per-endpoint retry settings, dead-letter queues, manual redelivery.

## Open questions

None.
