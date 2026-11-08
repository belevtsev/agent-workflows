## Why
Short jobs need timeouts below one second (API-412); the jobs API only takes
whole seconds in `timeout_seconds`.

## What Changes
- Accept `timeout_ms` on job creation, alongside `timeout_seconds`.
- Return both fields in job status responses during the deprecation period.
- Reject a request whose two timeout fields disagree.

## Capabilities
### New Capabilities
None.

### Modified Capabilities
- `jobs-api`: job timeouts in milliseconds.

## Impact
`POST /v1/jobs` and `GET /v1/jobs/{id}` in `jobs/api.go`; the Go SDK in
`jobs/client` keeps working unchanged.

## Non-goals
- Removing `timeout_seconds` (no earlier than release 2027.2, in a later change).

## Sources
- API-412, updated 2026-10-28.

## Open Questions
None.
