# Codex pilot

## ADDED Requirements

### Requirement: Frozen paired experiment

The current pilot SHALL assign exactly 24 measured identities from twelve pinned real
tasks, one repetition and two compact-context conditions, with the same
Sol/high model policy, eight-minute limit and small-task role topology. Assets and order SHALL be
frozen before measured execution. Failures SHALL NOT be silently retried.

The earlier 72-run Sol/max profile SHALL remain readable. A user-directed budget
change SHALL preserve interrupted work and counters as a separate phase rather
than rewrite its frozen inputs or pool unlike conditions.

Source: user-selected replacement of the original pilot with 24 Sol/high runs at eight minutes, and proposal.md.

#### Scenario: Ordinary tests

- **WHEN** a contributor runs the ordinary test suite
- **THEN** paid model execution is skipped.

#### Scenario: Changed source or prompt

- **WHEN** a frozen asset changes before a run
- **THEN** the harness rejects the changed input.

#### Scenario: User changes the budget

- **WHEN** the operator stops a claimed attempt before its result is finalized
- **THEN** offline recovery preserves the cancellation, partial candidate and observed usage without invoking or retrying the solver.

### Requirement: Independent quality evidence

Acceptance checks SHALL demonstrate behavioral failure on broken source and
success on reference source before trials. Solvers SHALL NOT access grading
assets, references, other task snapshots or remote solutions. Functional
acceptance, regressions, protocol status and infrastructure SHALL be distinct.
Blind reviewers SHALL receive read-only opaque candidates without arm or token
information. A timed-out review SHALL remain unknown. Additional confirmation
probes SHALL be labeled posthoc rather than rewriting the frozen score.

Source: user request for real-world quality evidence and proposal.md.

#### Scenario: Candidate changes its own tests

- **WHEN** a candidate alters a test or build declaration
- **THEN** independent grading retains its frozen requirements and test evidence.

#### Scenario: Blind review runtime

- **WHEN** an independent reviewer inspects an opaque candidate
- **THEN** its namespace exposes the public contract and read-only source without private mapping, grading outcomes or solver homes.

### Requirement: Actual complete token accounting

The pilot SHALL retain usage for all unique coordinator and worker sessions,
including failed attempts. Cached input and reasoning output SHALL NOT be
double counted. Missing evidence SHALL remain unknown. Reports SHALL distinguish
calibration, measured solver work and independent grading.

Source: user request for actual token usage and proposal.md.

#### Scenario: Repeated usage event

- **WHEN** cumulative usage is repeated in stdout and a rollout
- **THEN** it contributes once to measured usage.

#### Scenario: Missing worker rollout

- **WHEN** a native worker has no captured usage
- **THEN** the run's accounting is incomplete rather than reported as zero.

#### Scenario: Forced stop during a request

- **WHEN** a run ends at its hard timeout or is cancelled
- **THEN** captured counters remain lower bounds and the run is excluded from complete paired token ratios even if its last captured usage event was complete.
