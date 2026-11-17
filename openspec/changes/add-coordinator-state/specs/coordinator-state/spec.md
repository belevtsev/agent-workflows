# Coordinator state

## ADDED Requirements

### Requirement: Bounded complete snapshot

Explicit compact task views SHALL expose complete bounded sections for source,
recorded task stage, active interval, checks, open recorded findings, effective
policy, known completion prerequisites and Git state. They SHALL return original
references instead of full records and preserve ordinary output compatibility.
They MUST distinguish unknown small-task history and untrusted worker claims
from observed facts, and MUST NOT certify task completion or run models/checks.

Source: owner request and proposal.md.

#### Scenario: Full and small task state

- **WHEN** a coordinator explicitly requests a compact snapshot
- **THEN** all sections and original references are returned while small workflow completion remains unknown.

#### Scenario: Referenced findings and untrusted returns

- **WHEN** canonical findings or an active worker handback exist
- **THEN** original finding references and handback counts/severities remain distinct without printing their prose or granting authority.

#### Scenario: Complete output budget

- **WHEN** the byte limit cannot hold complete mandatory sections
- **THEN** the command refuses with no partial JSON and saves no new baseline.

### Requirement: Reliable current observations

Snapshots SHALL bind task/worktree/schema identity and consistent source/raw
records, check evidence and effective policies, including original selected
skills for both hosts. Changing state SHALL be refused rather than becoming a
mixed reusable baseline. Existing quality and final Git requirements MUST remain.

Source: owner request to establish reliable snapshots before deltas.

#### Scenario: Source and ignored record changes

- **WHEN** raw sources or ignored task records change
- **THEN** current source identity changes even when the relevant Git tree does not.

#### Scenario: Policy and evidence changes

- **WHEN** host role policy, selected external skill content or current check evidence changes
- **THEN** the affected sections and missing prerequisites reflect the live state without executing a worker or adviser.

### Requirement: Deltas from complete known snapshots

Deltas SHALL be available only from retained complete baselines of the same
task/worktree/version within the lifetime limit. Only changed sections SHALL
be returned, explicit empty states SHALL clear facts, and applying the entire
delta MUST reconstruct the current full projection. Complete current baselines
SHALL also be retained behind successful delta responses.

Source: owner request for changes since a known snapshot.

#### Scenario: Unchanged and changed delta chains

- **WHEN** a complete known baseline is supplied across check, finding or interval events
- **THEN** unchanged sections are omitted and applying each delta yields the current full state.

#### Scenario: Invalid or unavailable baseline

- **WHEN** a baseline is unknown, evicted, expired, future-dated, tampered, linked, incomplete or mismatched
- **THEN** no delta is emitted and the coordinator is directed to request a fresh full snapshot.

### Requirement: Private bounded cache

Snapshots SHALL use contained private atomic storage and bounded retention,
without mutating tracked task history, active intervals or worker receipts.

Source: existing opt-in local protocol and proposal.md.

#### Scenario: Safe retention and observation

- **WHEN** views are retained or the snapshot cache fills
- **THEN** private snapshots are bounded and managed intervals/receipts and tracked records remain unchanged.
