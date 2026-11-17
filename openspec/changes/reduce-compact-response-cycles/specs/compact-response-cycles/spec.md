# compact-response-cycles

## ADDED Requirements

### Requirement: Deliver complete fitting failure evidence

Explicit compact check/verification output SHALL deliver complete chronological
failed evidence when the final serialized response fits its requested byte
budget. Required metadata/outcomes, escaping and trailing newline MUST count.
Check collection MUST remain bounded and share the allowance across checks.

Source: proposal.md; design.md; docs/benchmarks/compact-overhead-plan.md.

#### Scenario: Fit complete failed logs to the exact response budget

- **WHEN** complete failed evidence fits the response budget, including a five-chunk log formerly truncated by the fixed preview
- **THEN** its original order and facts are returned with zero omitted chunks and no unnecessary continuation.

#### Scenario: Share the budget and account for escaping

- **WHEN** multiple outcomes share one response or raw text fits but final escaped JSON does not
- **THEN** the entire response stays bounded with all required outcomes and explicit omissions or a required-metadata error.

### Requirement: Preserve bounded original-log recovery

Evidence that cannot fit, has unavailable original bytes or has expired MUST
retain existing preview/error and cursor semantics. Expansion MUST NOT execute
checks, create a pass or replace current freshness requirements. Ordinary quiet
output and quiet-success summaries SHALL remain available.

Source: proposal.md; design.md; docs/evidence.md.

#### Scenario: Expand genuine noisy evidence without rerunning checks

- **WHEN** failed evidence exceeds the budget or its original log is unavailable
- **THEN** a bounded diagnostic preview and original-log cursor or explicit missing/partial error are returned without a fabricated complete outcome.

### Requirement: Provide bounded executable QE recipes

QE saved packets SHALL snapshot current check, original-log, continuation and
typed scoped-search argv recipes. Unbounded check IDs MUST use the complete
all-declared-check default, never a partial suite. Original role/assignment and
selected skills MUST remain complete. Other roles SHALL avoid this inventory.

Source: proposal.md; design.md; internal/cli/worker_commands.go.

#### Scenario: Run current recipes and retain the complete check suite

- **WHEN** QE receives a saved packet with ordinary, noisy, log and fallback recipes
- **THEN** the recipes resolve against current CLI metadata and oversized operands select all declared checks rather than omitting checks.

### Requirement: Keep capability observations scoped

Optional-tool lookup SHALL inspect availability without executing or installing
the tool. Observations MUST bind to known host/session/runtime context including
the actual lookup cwd. Unknown/unverifiable or changed scope MUST remain
unknown and permit the typed fallback. Retained packet verification MUST NOT
refresh observations or rewrite saved role/assignment/skill bytes.

Source: proposal.md; design.md.

#### Scenario: Distinguish root and subdirectory lookup environments

- **WHEN** identical relative PATH entries resolve differently from the repository root and a subdirectory
- **THEN** the observation scopes differ and unknown scope uses a permitted fallback without claiming executable authority.

#### Scenario: Verify immutable original worker inputs

- **WHEN** saved packets are re-read after environment changes or original policy/skill content is modified
- **THEN** retained hints stay byte-identical while existing policy/skill drift and packet-tamper checks remain authoritative.

### Requirement: Preserve semantic and terminal handoff decisions

Coordinator guidance SHALL require terminal native completion and original
semantic red/review decisions before success-only sequential finish/preparation.
Nonactionable progress MAY be coalesced; actionable questions, risks, blockers,
user progress, cancellation, deadlines and budget supervision MUST remain.
No receipt SHALL certify semantic TDD, independent review or task completion.

Source: proposal.md; design.md; internal/roles/text/coordinator.md.

#### Scenario: Complete original lifecycle with lean cadence

- **WHEN** native launch projection and final returns are used in the scripted defect workflow
- **THEN** full original inputs, observed red, green, a major finding, repair, fresh review and normal hooked Git completion remain present.

#### Scenario: Retain actual failures at handoff boundaries

- **WHEN** finishing fails or the next preparation fails after a successful finish
- **THEN** preparation is skipped on failed finish and an already-finished interval is preserved without invented new work or completion.

### Requirement: Retain mandatory launch data

`task prepare --dispatch --view launch` SHALL be explicit and preserve native
prompt/hash, host/model/reasoning, fresh context/fork settings, Claude native
role, packet path/hash, dispatch/task identity, worktree and warnings. Full v2
default JSON and original full worker packets SHALL remain compatible.

Source: proposal.md; design.md; docs/compact-context.md.

#### Scenario: Use the same native contract in both launch views

- **WHEN** Codex or Claude native preparation selects launch or default full output
- **THEN** mandatory launch settings, original packet/skill contract and identities agree without requiring another response to obtain launch fields.

### Requirement: Read original full dispatch without new evidence

`task dispatch <dispatch-id>` SHALL read the original saved full native dispatch
from its worktree with a prompt bound to the immutable stage record. It MUST
validate record/packet identity and current role/skill/adviser/check policy.
It MUST NOT create locks, snapshots, baselines, checks or workers. Source edits
MAY leave archived original state intact; that state grants no current pass.
Records lacking a bound original native prompt MUST fail without invention.

Source: proposal.md; design.md.

#### Scenario: Read exact immutable dispatch without mutation

- **WHEN** a saved native dispatch is read repeatedly after permitted source edits
- **THEN** its original full response is unchanged and no execution or evidence state is created.

#### Scenario: Reject unsafe stale or unbound dispatch records

- **WHEN** a record/packet is tampered with, policy or selected skills drift, the worktree differs or an old record has no bound prompt
- **THEN** the read fails explicitly without constructing fresh evidence or silently replacing the original prompt.

### Requirement: Preserve workflow authority and opt-in behavior

Compact MUST remain opt-in. Original selected skills, behavioral coverage,
neutral independent review, write bounds, human approvals and normal hooks/gate
MUST remain. New paid pilots/probes, model downgrades and general token/cost/
quality claims SHALL NOT be introduced. Replay metrics SHALL distinguish local
calls/bytes from unknown provider tokens and model quality.

Source: proposal.md; design.md; CONTRIBUTING.md.

#### Scenario: Keep offline verification and quality authority

- **WHEN** the changes and their documentation are validated locally
- **THEN** the original lifecycle/negative controls and ordinary defaults remain with no paid execution or manufactured quality or saving conclusion.
