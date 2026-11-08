## Why
Auditors need the audit log for a period as a file they can archive
(SEC-77).

## What Changes
- Add `GET /audit/export?from=<date>&to=<date>` returning the events in an
  inclusive UTC date range as CSV.
- Only the `auditor` role may export.

## Capabilities
### New Capabilities
- `audit-export`: exporting the audit log for a date range.

### Modified Capabilities
None.

## Impact
New endpoint in the audit service and an entry in `docs/api.md`.

## Non-goals
- Other export formats.
- Scheduled exports.

## Sources
- SEC-77, updated 2026-10-27.

## Open Questions
None.
