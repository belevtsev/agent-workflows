# Coordinator protocol

## ADDED Requirements

### Requirement: Complete scripted transport accounting

Offline replay SHALL transport and charge assignments in both compared paths,
record exact declared serialized boundary payloads, and distinguish bytes/calls
from unavailable provider tokens and model quality. It SHALL retain retries,
rejected requests, full original worker/skill reads and every required role.

Source: owner-authorized item 1 and proposal.md.

#### Scenario: Ordinary assignment delivery

- **WHEN** the ordinary saved-handoff replay supplies an assignment
- **THEN** its delivery is actually recorded and charged before equivalent facts are compared.

#### Scenario: Complete scripted role workflow

- **WHEN** scripted workers perform red, implementation, review finding, repair, fresh review and Git completion
- **THEN** every actual protocol payload is charged and completion requires the real local commit and current gate.

#### Scenario: Interrupted Git preparation

- **WHEN** review finishes but execution stops before the Git worker completes
- **THEN** replay retains an incomplete workflow rather than inferring success from passing tests or a prepared packet.

### Requirement: Managed coordinator protocol

Managed guidance SHALL reuse prepare/finish mechanics instead of duplicating
equivalent manual baseline/write checks. It SHALL avoid unnecessary short polls
and repeated unchanged capability discovery while retaining progress, deadlines,
cancellation, meaningful red inspection, independent review and original roles.

Source: owner-authorized item 2 and proposal.md.

#### Scenario: Managed role boundary

- **WHEN** an opted-in coordinator uses managed preparation and finalization
- **THEN** equivalent manual bookkeeping is unnecessary but semantic checks and final Git requirements remain mandatory.

#### Scenario: Waiting for a native worker

- **WHEN** a native worker runs without an actionable change
- **THEN** guidance prefers completion notification or a bounded wait compatible with host and progress obligations, not repeated short polls or time-pressure messages.

### Requirement: Generated native dispatch

Explicit dispatch preparation SHALL return a versioned generated native launch
description while preserving default preparation output. It SHALL retain full
original saved packets and skills, effective model/effort and fresh-context
requirements. It SHALL invoke no host, model or paid service.

Source: owner-authorized item 3 and proposal.md.

#### Scenario: Fresh native dispatch

- **WHEN** the coordinator explicitly requests a generated dispatch
- **THEN** it receives a bounded label-first prompt bound to the saved packet and model policy, leaving actual launch to the host.

### Requirement: Structured untrusted returns

Return ingestion SHALL strictly validate bounded input and identity, inspect
current local write/evidence facts independently, and retain claims separately
from verified observations. It SHALL preserve the active interval until explicit
finish and SHALL NOT certify native execution, skill loading, review or task
completion. Missing or oversized mandatory findings/warnings SHALL NOT be hidden.
Every finding SHALL preserve a valid blocker, major or minor severity.

Source: owner-authorized item 3 and proposal.md.

#### Scenario: Valid worker hand-back

- **WHEN** the coordinator ingests a valid return for the active dispatch
- **THEN** it retains bounded claims with their local observations without finishing the interval or running checks.

#### Scenario: Invalid or stale worker hand-back

- **WHEN** input is malformed, references another dispatch/worktree/packet, violates writes, or conflicts with current policy/evidence
- **THEN** ingestion rejects it without granting approval or closing the interval.
