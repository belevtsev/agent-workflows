# Compact handoffs

## ADDED Requirements

### Requirement: Typed context operations

The CLI SHALL provide native source-read and literal/Go-regexp search commands
that construct the existing versioned request in Go. They SHALL preserve source
containment, explicit errors/skips/omissions, original coordinates, byte budgets,
expansion cursors and freshness checks. Invalid operands SHALL fail before cache access.

Source: owner request to implement item 1 and proposal.md.

#### Scenario: Native read and search

- **WHEN** the caller supplies native paths, ranges or a search pattern
- **THEN** the CLI returns the same bounded original evidence as a valid equivalent collection request.

#### Scenario: Invalid typed request

- **WHEN** an operand, range, regexp or flag is invalid
- **THEN** the command reports usage without collecting source or writing selection receipts.

### Requirement: Saved canonical worker assignments

An explicit saved-brief operation SHALL preserve original role instructions,
effective constraints, selected skill references, raw assignment and the required
task label in a private canonical file. It SHALL return a small dispatch envelope
with model/effort, original baseline, path and hash, without reprinting the assignment.
The host SHALL retain worker-launch authority. Existing inline briefs SHALL remain compatible.

Source: owner request to implement item 2 and proposal.md.

#### Scenario: Direct saved handoff

- **WHEN** the coordinator saves a valid assignment for Codex or Claude
- **THEN** the canonical file preserves the assignment and required host instructions while stdout contains only dispatch metadata.

#### Scenario: Invalid assignment

- **WHEN** assignment bytes or saved output parameters are invalid
- **THEN** the command fails without recording a new skill selection or publishing a usable dispatch.

### Requirement: Ordered local stage records

Managed preparation SHALL write the selected-skill receipt before capturing its
baseline and SHALL refuse a new interval while the prior interval remains active.
Finalization SHALL check the original role write policy from the saved baseline,
retain effective-policy binding and judge existing current check evidence without
executing checks. Failed validation SHALL preserve the active interval.
Explicit abandonment SHALL retain a reason and confer no approval.

Source: owner request to implement item 3 and proposal.md.

#### Scenario: Prepare and finalize a worker

- **WHEN** a worker finishes within its recorded write limits
- **THEN** finalization records those limits and the actual current check verdict before permitting the next managed preparation.

#### Scenario: Invalid writes or changed policy

- **WHEN** a worker exceeds its write boundary or its effective policy changes
- **THEN** finalization refuses successful closure and preserves the original interval for correction or explicit abandonment.

#### Scenario: Expected check outcome

- **WHEN** finalization requires current passing or failing checks
- **THEN** missing, stale, interrupted or mismatched evidence cannot satisfy that expectation and no check is silently rerun.

#### Scenario: Explicit abandonment

- **WHEN** the caller abandons an active interval with a reason
- **THEN** the interval is released with an abandonment record that certifies neither acceptable writes nor completed review.

### Requirement: Opt-in and unchanged quality authority

All new handoff behavior SHALL require explicit commands. Normal task-start,
original role permissions, independent reviews, approvals, check freshness and
hooked commits SHALL retain their existing meaning. Local receipt finalization
SHALL NOT certify model execution, skill loading, review approval or task completion.
Ordinary validation SHALL invoke no paid model or pilot.

Source: owner constraints to retain opt-in and run no paid pilot.

#### Scenario: Ordinary task and help

- **WHEN** the caller starts a normal task or asks for help
- **THEN** no managed compact interval or model invocation is introduced automatically.
