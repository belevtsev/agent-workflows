## Purpose
Auditors export the audit log for a date range as CSV.

## ADDED Requirements
### Requirement: Export audit events for a date range
The system SHALL return, for `GET /audit/export?from=<date>&to=<date>`, every audit event dated from `from` to `to` inclusive (UTC, `YYYY-MM-DD`) as CSV with the header row `time,actor,action,target`.

#### Scenario: Events inside the range
- **GIVEN** audit events at 2026-04-01T08:00:00Z and 2026-04-03T17:30:00Z
- **WHEN** an auditor requests `GET /audit/export?from=2026-04-01&to=2026-04-02`
- **THEN** the CSV contains the header row and only the 2026-04-01T08:00:00Z event

### Scenario: Export of an empty range
- **GIVEN** no audit events on 2026-05-01
- **WHEN** an auditor requests `GET /audit/export?from=2026-05-01&to=2026-05-01`
- **THEN** the response body is exactly the header row `time,actor,action,target`

### Requirement: Only auditors export
The system SHALL reject an export request from a user without the `auditor` role with HTTP 403.

#### Scenario: Non-auditor requests an export
- **GIVEN** user `sam` without the `auditor` role
- **WHEN** `sam` requests `GET /audit/export?from=2026-04-01&to=2026-04-02`

### Requirement: Reversed range
The system SHALL reject a request whose `from` is after `to` with HTTP 400 and the message `from must not be after to`.

#### Scenario: from after to
- **WHEN** an auditor requests `GET /audit/export?from=2026-04-05&to=2026-04-01`
- **THEN** the response is HTTP 400 with the message `from must not be after to`
