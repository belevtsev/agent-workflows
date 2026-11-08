# SEC-77: Export the audit log for a date range

Type: Story
Reporter: Tomas Berg (Security)
Updated: 2026-10-27

## Description

Auditors need the audit log for a period as a file they can archive.

## Acceptance criteria

1. `GET /audit/export?from=<date>&to=<date>` returns the audit events dated
   from `from` to `to` inclusive (dates are `YYYY-MM-DD`, UTC) as CSV with the
   header row `time,actor,action,target`.
2. Only users with the `auditor` role may export; anyone else gets HTTP 403.
3. A range in which no events happened returns a file with only the header
   row.
4. `from` after `to` returns HTTP 400 with the message
   `from must not be after to`.
5. Document the endpoint in `docs/api.md`.
