# Offline verification

## ADDED Requirements

### Requirement: Retrospective defect calibration

Offline calibration SHALL read actual retained public charset stdout/contracts,
check pinned hashes and evaluate independently labeled individual behaviors.
It SHALL preserve a fixed public retrospective development/validation split
and report both known original-coverage gaps and expanded-check outcomes.
Historical frozen grades and evidence MUST remain unchanged.

Source: proposal.md.

#### Scenario: Replay labeled historical behaviors

- **WHEN** offline calibration reads the retained baseline, reference and candidate observations
- **THEN** every label and split membership is retained, with detected defects, behavior controls and original-coverage gaps reported separately.

### Requirement: Distinguish evidence from conclusions

Calibration SHALL distinguish detected defects, false positives, correct
behavior controls and unknown/unusable evidence. Missing, malformed, stale,
unexecuted or hash-mismatched evidence MUST NOT count as a detected defect or
passing behavior. Individual correct behavior controls MUST NOT imply a fully
correct program, and the retrospective split MUST NOT imply unseen or blind
validation or model-review quality.

Source: proposal.md and design.md.

#### Scenario: Reject unusable calibration evidence

- **WHEN** source evidence is malformed, missing, mismatched or unexecuted
- **THEN** calibration reports unknown or rejects the evidence without manufacturing a passing behavior or detected defect.

### Requirement: Recovered context equivalence

Generated checks SHALL compare evidence recovered through bounded reads,
searches and required expansions with direct source observations. They SHALL
exercise UTF-8, line boundaries, freshness, cursor binding and metadata budgets.
Git observations SHALL agree with direct staged-diff and state-identity output.
Native Go fuzz targets SHALL replay deterministic seeds in ordinary tests and
support explicitly bounded campaigns and failing-corpus replay.

Source: proposal.md.

#### Scenario: Recover generated read and search evidence

- **WHEN** generated source is observed through bounded read/search pages and all needed expansions
- **THEN** recovered evidence agrees with the direct source observations.

#### Scenario: Refresh changed source evidence

- **WHEN** source bytes change after an observation
- **THEN** subsequent observations agree with the current source instead of silently reusing stale evidence.

#### Scenario: Reject invalid context continuation or metadata budget

- **WHEN** a cursor is malformed or belongs to another item, or a response budget cannot retain required metadata
- **THEN** the request is rejected without silently losing evidence or accepting an unrelated continuation.

#### Scenario: Preserve staged Git changes and state identity

- **WHEN** a fixture stages added, modified and deleted paths
- **THEN** collected Git observations agree with direct Git diff and state-identity observations.

### Requirement: Protocol and state equivalence

Generated worker-return checks SHALL preserve accepted claims, reject invalid
inputs and retain separation between claims, local observations and authority.
Coordinator snapshot/delta checks SHALL reconstruct bounded projections and
compare with current state across interval, check and policy transitions,
including cleared state. These checks MUST NOT grant completion authority.

Source: proposal.md.

#### Scenario: Preserve worker claims without granting authority

- **WHEN** generated valid claims or malformed worker returns are ingested
- **THEN** accepted claims preserve their content and bindings while invalid inputs cannot gain check, review or completion authority.

#### Scenario: Reconstruct coordinator transitions including clearing

- **WHEN** checks, policy or worker intervals change and snapshots/deltas are observed
- **THEN** applying the reported changes reconstructs the current bounded projection, including cleared sections.

### Requirement: Reproducible local verification

Documentation SHALL provide deterministic test commands, bounded Go fuzz
commands, failing-corpus replay and an optional new-file-only calibration
receipt. All examples SHALL execute locally without invoking a model. Coverage
and limitations SHALL be explicit; a passing finite campaign MUST NOT imply
exhaustive equivalence or a broader token/billing benefit.

Source: proposal.md.

#### Scenario: Write a separate calibration receipt

- **WHEN** a caller explicitly requests an offline receipt at a new absolute path
- **THEN** the test writes the calibration result there and refuses to overwrite existing evidence.

### Requirement: Preserve workflow scope

This change MUST keep compact opt-in, models, routing, roles, permissions,
original inputs, approvals and gate authority unchanged. It SHALL add no CLI
command, live benchmark or paid pilot. Calibration SHALL remain distinct from
native model/token-accounting calibration and leave historical paid reports,
frozen/private assets and experimental artifacts unchanged.

Source: proposal.md and owner's no-paid-pilot restriction.

#### Scenario: Preserve ordinary task starts and original worker inputs

- **WHEN** ordinary or explicitly compact tasks prepare workers
- **THEN** existing opt-in behavior, complete original instructions and role authority remain unchanged.
