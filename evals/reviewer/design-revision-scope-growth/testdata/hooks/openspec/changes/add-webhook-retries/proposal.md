## Why
A webhook delivery that fails once is lost (HOOK-88): the sweeper marks it
failed and never sends it again.

## What Changes
- Retry a delivery that fails with a network error or a 5xx response up to
  5 times with a fixed backoff schedule.
- Keep a delivery that waits for a retry across restarts and redeploys.

## Capabilities
### New Capabilities
- `webhook-retries`: retries of failed webhook deliveries.

### Modified Capabilities
None.

## Impact
The delivery sweeper in `hooks` and the `deliveries` table.

## Non-goals
- Per-endpoint retry settings.
- A dead-letter queue or manual redelivery.

## Sources
- HOOK-88, updated 2026-10-20.

## Open Questions
None.
