# Compact worker delivery

## ADDED Requirements

### Requirement: Complete focused worker delivery

Saved worker delivery SHALL preserve original assignment bytes, original Codex
role text or Claude native role, and selected original skills. Guidance SHALL
keep coordinator supervision out of repeated worker instructions, except when
an explicit task reference or selected skill requires the original guide.
Default task starts MUST remain unchanged; compact MUST remain opt-in.

Source: proposal.md.

#### Scenario: Original worker inputs remain complete

- **WHEN** Codex or Claude receives a prepared assignment
- **THEN** original task/role/skill obligations and write limits remain, without requiring duplicate coordinator-guide reads.

### Requirement: Select observations without manufacturing evidence

Worker and coordinator guidance SHALL prefer ordinary quiet checks and native
short reads, reserving compact operations for noisy observations. Coordinator
state views SHALL wait for task history and additional state needs. Minimal
worker returns SHALL require no invented optional claims or extra checks.
Existing return validation and finish authority MUST remain intact.

Source: proposal.md.

#### Scenario: Quiet observations and existing history

- **WHEN** output is short or no task history exists yet
- **THEN** guidance avoids unnecessary compact wrappers and premature state probes, while required checks still execute.

#### Scenario: Return claims match their evidence

- **WHEN** a return claims an unselected support guide or a runner outcome as a gate state
- **THEN** it is rejected without saving a return or releasing the active interval; a valid minimal return remains available.

### Requirement: Honest offline verification

Offline verification SHALL count every scripted request/response, packet/skill
read, check, handback, finish and rejected request. It SHALL compare critical
facts and retain unknown provider usage/model quality. It MUST neither invoke
a paid model nor present bytes or scripted success as measured token savings.

Source: proposal.md and owner restriction on paid comparisons.

#### Scenario: Complete transport accounting

- **WHEN** repeated-guide and focused delivery are replayed
- **THEN** all four workers and checks remain accounted with equal critical facts, and any reduction is labeled as scripted transport only.
