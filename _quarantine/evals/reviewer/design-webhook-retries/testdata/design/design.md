# Design: webhook retries

## Context

The reviewed change `add-webhook-retries` asks for retries of deliveries that
fail with a network error or a 5xx response. Today `Sweeper.sweepOnce` sends
each pending delivery once and marks a failure `failed`
(`hooks/delivery.go:52-69`).

## Options considered

1. **In-memory timer per failed delivery.** When an attempt fails, the
   sweeper starts a `time.AfterFunc` for the next attempt and keeps the
   delivery in a map keyed by ID until it succeeds or runs out of attempts.
   Consequence: retries fire at exactly the scheduled instant, with no
   polling delay.

## Decision and why

Option 1: it gives exact timing and needs no schema change.

## Component boundaries and interfaces

- `Retrier` (new, composition) owns the map of pending retries and their
  timers. It is created by `main` next to the `Sweeper` and stopped with it.
- `Sweeper` keeps sending first attempts. On a retryable failure it hands
  the delivery to `Retrier.Schedule` instead of marking it failed.
- `Sender` is unchanged and is the only component that calls endpoints.

## Failure behaviour

- Network error or 5xx: schedule the next attempt.
- 4xx other than 429: mark the delivery `failed`.
- Send timeout (10 s): treated as a network error.
- Context cancellation: `Retrier.Stop` cancels every timer; deliveries in
  flight finish their current attempt.
- Duplicate failure report for the same delivery: `Schedule` ignores an ID
  that already has a timer.

## Rollout and rollback

No schema change. Deploy the new build; roll back by deploying the previous
build. Nothing is irreversible.

## Verification plan

- R1, R2, R4: unit tests with an injected clock.
- R3: manual check against a staging endpoint.
- R5: unit test with concurrent `Schedule` calls.
