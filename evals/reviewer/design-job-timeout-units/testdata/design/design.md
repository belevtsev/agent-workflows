# Design: job timeouts in milliseconds

## Context

The reviewed change `add-timeout-ms` adds millisecond timeouts to the jobs
API. Today requests and responses carry whole seconds in `timeout_seconds`
(`jobs/api.go:11`, `jobs/api.go:18`), validated to 1..3600
(`jobs/api.go:29-31`).

## Options considered

1. **Add `timeout_ms` next to `timeout_seconds`.** Both fields live on the
   request and response types; the handler converts between them.
   Consequence: two fields to validate and keep consistent on every path.
2. **Rename `timeout_seconds` to `timeout_ms`.** One field, one unit, in
   both directions. Consequence: a single conversion at the queue boundary
   and simpler validation.

## Decision and why

Option 2: one unit everywhere is easier to validate and to document, and it
avoids carrying two representations of the same value.

## Component boundaries and interfaces

- `jobs.Server.Create` (existing, composition) decodes the request,
  validates the timeout and enqueues the job; it owns no state.
- `queue.Enqueue` (existing, adapter) takes the timeout in milliseconds
  instead of seconds.
- `JobRequest` and `JobStatus` (existing, values) carry `TimeoutMS`.

## Failure behaviour

- Malformed body: HTTP 400 `malformed_body`, no job created.
- Timeout outside 1..3600000 ms: HTTP 400 `invalid_timeout`, no job created.
- Queue unavailable: HTTP 503, no job created; the client retries.

## Rollout and rollback

Deploy the new server, then release Go SDK 3.0, which reads `timeout_ms`.
Roll back by redeploying the previous server build.

## Verification plan

- R1, R2: unit tests of the handler with table cases.
- R3: integration test against a local server.
