## ADDED Requirements

### Requirement: Pin approved role routing

The system SHALL save one host, model, reasoning setting and selected skill
set per active role. The originating chat host SHALL be usable for task start
and resume.

Source: Developer request for one host and role configuration shared across Codex and Claude Code.

#### Scenario: Resume a configured task from another host

- **WHEN** the developer resumes in the original worktree
- **THEN** the system uses the saved assignments and runner profile

### Requirement: Run configured work through an independently managed runner

The system SHALL require a machine-registered runner with reviewed policy
and native acceptance. The runner SHALL use disposable worker and check
jobs and sign observed results outside those jobs.

Source: Configured workflow review findings 1–7, 11, and 13–16, and the decision to require independently provisioned runners.

#### Scenario: Import accepted worker output

- **WHEN** a signed result names the reserved job, fresh session and assigned settings
- **THEN** the system may import only the approved role’s allowed changes
- **AND** it rejects host, ledger, Git control and ignored-path writes

#### Scenario: Verify an accepted candidate

- **WHEN** the system verifies a configured candidate
- **THEN** it requires the intake assertion, additional reviewed assertions and required checks
- **AND** a red baseline requires the approved test’s structured failure
- **AND** a new assertion or candidate requires new evidence

#### Scenario: Require an independent reviewer

- **WHEN** the developer or project policy requires review
- **THEN** a fresh read-only reviewer returns a signed verdict with its assigned settings and candidate

### Requirement: Retain results across interruption

The system SHALL reserve a job before launch and release the ledger lock
while the runner works. Resume SHALL consume the original bound result.
Release SHALL require signed job termination and runner operator authorization.

Source: Configured workflow review findings 8–10 and 12.

#### Scenario: Resume a disrupted application

- **WHEN** a prior attempt is outstanding
- **THEN** the system checks that attempt’s status before accepting a result or allowing replacement

### Requirement: Preserve installation defaults

A new installation SHALL select the latest matching artifacts by default.
The second plugin install SHALL keep the saved personal host unless the
developer explicitly replaces it.

Source: Configured workflow review finding 17.

#### Scenario: Choose the other host

- **WHEN** the second plugin setup succeeds
- **THEN** the existing personal default stays selected without a replace flag

### Requirement: Require complete push input

The Git gate SHALL deny malformed or incomplete ref updates and record
`no_ref_updates` for an empty Git push stream without verified content.

Source: The outstanding pre-push input finding in the 18 November review.

#### Scenario: Git reports nothing to push

- **WHEN** Git supplies an empty update stream
- **THEN** the no-op is allowed and carries no tree or passing check evidence
