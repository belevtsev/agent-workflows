# Design: webhook retries

## Context

The reviewed change `add-webhook-retries` asks for retries of deliveries that
fail with a network error or a 5xx response, on the schedule 1 s, 2 s, 4 s,
8 s and 16 s, and for retries that survive a restart. Today
`Sweeper.sweepOnce` sends each pending delivery once and marks a failure
`failed` (`hooks/delivery.go:52-69`). Delivery state already lives in the
`deliveries` table, which the sweeper polls every 5 s
(`hooks/delivery.go:26-33`).

## Options considered

1. **In-memory timer per failed delivery.** When an attempt fails, a
   `time.AfterFunc` fires the next attempt. Consequence: exact timing, but
   every pending retry is lost on a restart or redeploy, which the spec's
   `Retries survive a restart` forbids.
2. **Persisted next attempt time, read by the sweeper** (smallest change).
   A failed attempt stores `next_attempt_at` on the delivery row and keeps it
   `pending`; the sweeper sends rows whose `next_attempt_at` has passed.
   Consequence: retries survive a restart; an attempt can start up to one
   sweep interval late, so the sweeper interval drops from 5 s to 250 ms.

## Decision and why

Option 2: it reuses the sweeper and the table that already survive restarts,
and the 250 ms interval keeps each attempt within the spec's schedule.

## Component boundaries and interfaces

- `Sweeper` (existing, composition) stays the only component that sends. It
  selects `pending` rows with `next_attempt_at <= now`.
- `Schedule` (new, decision) maps a failed attempt to the next
  `next_attempt_at` or to `failed`.
- `EndpointBreaker` (new, decision) counts consecutive failed deliveries per
  endpoint. After 3 in a row it opens for 10 minutes: the sweeper skips every
  delivery to that endpoint while it is open, and an admin call
  `POST /admin/endpoints/{id}/breaker/reset` closes it early. This protects
  struggling endpoints beyond the backoff schedule.
- `Sender` is unchanged.

## Failure behaviour

- Network error or 5xx: store `next_attempt_at` per R3 and keep the row
  `pending`; after the sixth failed attempt mark it `failed`.
- 4xx other than 429: mark the delivery `failed`.
- Send timeout (10 s): treated as a network error.
- Restart or redeploy: pending rows and their `next_attempt_at` are in the
  table, so the next sweep after start-up sends every retry that is due.
- Open breaker: deliveries to that endpoint stay `pending` and are not
  attempted until the breaker closes.

## Rollout and rollback

Add the nullable column `next_attempt_at` and the table `endpoint_breakers`,
then deploy the new build. Rolling back to the previous build leaves both in
place; the previous build ignores them. The column and the table are additive
and need no data migration.

## Verification plan

- R1 to R4: unit tests with an injected clock.
- R5: integration test that restarts the sweeper between attempts.
- R6: unit test of the breaker with an injected clock.
