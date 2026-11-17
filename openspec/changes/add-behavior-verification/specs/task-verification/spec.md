# Task behavioral verification

## ADDED Requirements

### Requirement: Declare optional behavioral intent

Checks SHALL optionally accept repeated changed-path `when` patterns, an
`expect` behavioral description and `verify = true` for applicable required task
verification. `check add` SHALL expose `--when`, `--expect` and `--verify`.
Scheduling MUST remain separate from the `paths` dependency/evidence reuse
promise. Missing verification metadata MUST preserve existing task behavior.

Source: proposal.md and owner's approved plan.

#### Scenario: Preserve metadata-free behavior

- **WHEN** a repository declares checks without verification metadata
- **THEN** existing check and task behavior remains unchanged.

#### Scenario: Separate scheduling from dependencies

- **WHEN** a check declares `when`, `expect`, `verify` and `paths`
- **THEN** changed paths determine priority/applicability while `paths` alone retains its dependency/reuse meaning.

### Requirement: Plan complete working-tree changes

`check plan [--from <rev>] [--json]` SHALL default to HEAD and compare the base
with the complete working tree. It SHALL report priority reasons, remaining
full gate checks and uncovered areas. No mapping SHALL remove a required check.
Regular-file original-byte differences from normalized snapshot blobs SHALL
also trigger scheduling; without a historical raw baseline this MAY
conservatively prioritize unchanged normalized originals. Raw-source read
failures MUST fail closed without changing `paths` evidence semantics.

Source: proposal.md and design.md.

#### Scenario: Plan staged unstaged untracked and deleted paths

- **WHEN** staged, unstaged, untracked or deleted paths differ from the chosen base
- **THEN** the plan accounts for all of them and identifies mapped priorities and uncovered paths.

#### Scenario: Keep unmapped full checks

- **WHEN** changed paths match no behavioral mapping
- **THEN** the plan reports coverage gaps and retains every remaining required gate check.

#### Scenario: Schedule original bytes hidden by normalization

- **WHEN** regular-file original bytes differ from Git-normalized snapshot blobs through a clean filter or CRLF conversion
- **THEN** applicable mappings remain scheduled, including in SHA256-object repositories, without changing evidence dependencies.

#### Scenario: Refuse unreadable original source

- **WHEN** the planner cannot safely read an original source file
- **THEN** planning fails closed instead of treating unavailable original bytes as unchanged.

### Requirement: Coordinate source-bound attempts

`task verify` SHALL support task, base, full/small workflow, JSON and opt-in
compact presentation. Its first attempt SHALL require an explicit base and
later attempts SHALL reuse the pinned resolved revision. Active managed worker
intervals MUST finish before verification. Omitted workflow SHALL select full
when that task folder exists, otherwise small. Only coordinator commands SHALL
write shared summaries. Declared commands SHALL run targeted checks first;
targeted failure SHALL stop and leave remaining checks pending. On targeted
success, a sanitized immutable historical summary SHALL be published before
final full required checks, with no tracked writes afterwards. An unchanged
successful repeat SHALL add neither history nor runs.

Source: proposal.md and design.md.

#### Scenario: Require and pin the first base

- **WHEN** the first attempt lacks `--from`, or a later attempt omits it
- **THEN** the first is refused and the later uses the original resolved base.

#### Scenario: Refuse active managed intervals

- **WHEN** verification is requested with an active managed worker interval
- **THEN** it is refused until that interval finishes, without publishing history.

#### Scenario: Stop on targeted failure

- **WHEN** a targeted declared command fails
- **THEN** verification stops and remaining full checks stay pending.

#### Scenario: Publish before final checks

- **WHEN** targeted checks succeed
- **THEN** the source-bound summary is published before final required checks and no tracked write follows those checks.

#### Scenario: Reuse unchanged successful verification

- **WHEN** successful verification repeats without relevant change
- **THEN** no new shared summary or local check run is created.

### Requirement: Derive current readiness from local evidence

Full and small shared summaries SHALL be immutable sanitized historical
source-bound claims under `verification/` and `.verification/<task>/`.
Raw logs SHALL remain local. Current readiness MUST require matching actual
local records and available logs and MUST NOT infer passes from the base or
historical summary alone. Missing required verification SHALL block full done;
small stage SHALL remain unknown with a separate verification status.
Reuse SHALL require matching private observations of actual filesystem bytes
and current deciding check records. First local verification SHALL run
applicable required checks even with older Git-only evidence. Changed raw
source, policy, selected original skill sources or deciding evidence SHALL
require fresh behavioral observation. Both hosts' selected sources SHALL
participate without changing role execution or host choice.

Source: proposal.md and design.md.

#### Scenario: Reject missing stale or unavailable local evidence

- **WHEN** evidence is missing, stale, mismatched or lacks local logs, including a clone holding shared summaries only
- **THEN** readiness is unavailable or incomplete and never a passing result.

#### Scenario: Establish the first local behavioral observation

- **WHEN** older passing check records exist but no matching private verification observation binds the actual filesystem bytes
- **THEN** task verification executes applicable required behavioral checks before establishing readiness.

#### Scenario: Reobserve changed raw source or deciding evidence

- **WHEN** raw source bytes (including hidden or Git-normalized edits), policy, selected skill sources or deciding check records change
- **THEN** prior readiness becomes stale and fresh behavioral observation is required.

### Requirement: Keep structural validation portable

`task check` SHALL validate full/small summary structure without requiring
private local logs or claiming current readiness.

Source: proposal.md and design.md.

#### Scenario: Validate history without private logs

- **WHEN** a clone holds structurally valid shared summary history without raw logs
- **THEN** structural validation succeeds while current verification remains unavailable.

### Requirement: Preserve workflow and skill authority

Compact SHALL remain opt-in presentation of the same verification lifecycle.
Independent review, human approvals, role write limits and Git gate authority
MUST remain unchanged. Existing selected skills SHALL use their original
directories/instructions for prerequisites, actual app startup, assertions and
cleanup through existing project tools. Selection MUST NOT imply loading or
compliance; Kitsu MUST NOT automatically discover/execute scripts or manage
services. Documentation SHALL include actual CLI help paths and project-command
examples for Go, C++ and JavaScript without installing tools. Historical
calibration and paid evidence MUST retain their original scope and claims.

Source: proposal.md and owner's approved plan.

#### Scenario: Preserve full small and compact authority

- **WHEN** ordinary or compact verification runs on a full or small task
- **THEN** it uses the same declared commands and evidence rules without replacing review, approval, role limits or gate requirements.
