# workflow-insights

## ADDED Requirements

### Requirement: Inspect captured workflow evidence

`kitsu usage show --diagnostics [--task <task>] [--json]` SHALL read existing
captured token, check and managed-interval records with original references.
Ordinary usage output MUST remain unchanged. Diagnostic collection MUST NOT
run checks, launch workers, contact model providers or copy raw logs. Exact
known totals, cache components and reasoning subsets SHALL retain their
accounting semantics; missing records, shared sessions and unmanaged work
MUST remain unknown or unattributed rather than become zero or inferred usage.

Source: proposal.md; docs/tokens.md; docs/evidence.md; docs/compact-context.md.

#### Scenario: Preserve ordinary usage output

- **WHEN** usage is requested without `--diagnostics`
- **THEN** the established human and JSON usage report shapes remain unchanged.

#### Scenario: Retain exact captured accounting

- **WHEN** diagnostic records include captured input, cache, output and reasoning counts
- **THEN** exact known totals are preserved and subsets are not added to the total twice.

#### Scenario: Keep missing shared and unmanaged attribution unknown

- **WHEN** records are absent, shared between tasks or outside managed attribution
- **THEN** diagnostics retain the known captured lower bound plus explicit unknown or unattributed coverage without inventing zero usage.

### Requirement: Report managed interval coverage

Diagnostics SHALL describe preparation-to-finish managed interval spans,
including coordinator wait, with overlap counted as elapsed coverage union.
Unfinished or missing intervals MUST leave explicit coverage gaps. The report
MUST NOT identify these spans as actual worker runtime, a causal critical path
or proof of why a task was delayed.

Source: proposal.md; docs/compact-context.md; design.md.

#### Scenario: Union overlapping prepared-to-finished spans

- **WHEN** two recorded spans cover seconds 0 through 10 and 5 through 15
- **THEN** elapsed coverage is 15 seconds rather than their 20-second duration sum.

#### Scenario: Preserve gaps and timing interpretation

- **WHEN** a task includes unfinished or unmanaged work and coordinator wait
- **THEN** diagnostic timing remains partial managed-span evidence with explicit gaps and no worker-runtime or causal claim.

### Requirement: Project optional consequential notes

Existing full-task proposal/design files MAY contain consequential claims
with Kind (`observed`, `inferred`, `unknown`), opened Source, Consequence if
wrong, Revisit when and Status, plus optional design decisions with Choice,
Alternatives, Reason, Source and Revisit when. Diagnostics SHALL project only
bounded recognized tables with original references and explicit missing,
partial, unrecognized or omitted content. Arbitrary prose MUST remain valid.
These notes MUST remain authored claims, not certified truth, resolved open
questions or substitutes for original sources and neutral independent review.
No full-task folder SHALL be invented for small-task notes.

Source: proposal.md; docs/workflow.md; internal/roles/text/spec-writer.md;
internal/roles/text/architect.md.

#### Scenario: Project consequential claims and decisions

- **WHEN** an existing full task has recognized consequential claim and decision tables
- **THEN** diagnostics display a bounded projection with original references while preserving their status as authored claims.

#### Scenario: Preserve prose partial formats and bounded omissions

- **WHEN** optional notes are prose, missing, partially recognized, closed, numerous or oversized
- **THEN** structural task validation remains unchanged and diagnostics explicitly describe unavailable or omitted content with original references rather than certify the notes or truncate a claim's meaning.

### Requirement: Investigate repeated check failures

Existing `tests.md` MAY hold optional troubleshooting experiments with ID,
Check, Content key, Policy key, Hypothesis, Prediction, Run, Observation and
Status. Diagnostics SHALL identify repeated failed or timed-out runs only
within the same check, content key and recorded command/paths policy key.
Notes and new evidence references MUST remain unconfirmed authored claims;
missing or unrecognized evidence SHALL remain unknown. Historic or resolved
rows MUST remain in the original file even when omitted from bounded display.
Diagnostics MUST NOT infer confirmed causes, retry automatically, suppress
checks or relax reviews.

Source: proposal.md; docs/evidence.md; design.md.

#### Scenario: Group repeated failures with matching identities

- **WHEN** failed or timed-out attempts share check id, content key and recorded command/paths policy key
- **THEN** diagnostics identify their repeated group with original attempt references.

#### Scenario: Split changed content and recorded policy

- **WHEN** a check attempt changes content, command or recorded dependency paths
- **THEN** it is not grouped with failures on the earlier content or policy identity.

#### Scenario: Preserve unconfirmed and missing troubleshooting evidence

- **WHEN** troubleshooting notes propose a cause, reference additional evidence or cannot be recognized
- **THEN** diagnostics retain claims or explicit unknowns without confirming a cause, copying logs or triggering another run.

### Requirement: Record estimates and sourced declared limits

`kitsu usage budget set <task> --unit tokens|seconds` SHALL record optional
estimate bounds with basis and repeatable assumptions, optional declared
limits with explicit source, workflow selection and JSON output. Metadata
MUST remain separate from `kitsu.toml` and role permissions, using
`usage-budget.json` in the full task or `.usage/<task>/` for small tasks.
Invalid units, inconsistent bounds, estimates without basis and limits
without source MUST fail without changing stored metadata. Show SHALL read
stored metadata; reset SHALL remove the selected unit. Recording metadata
MUST NOT authorize provider calls or enforce a hard cap or runtime/payment
guarantee.

Source: proposal.md; design.md; docs/workflow-insights.md.

#### Scenario: Store separate full and small unit budgets

- **WHEN** token and second metadata is set for an explicitly selected full or small task
- **THEN** each unit retains its estimate, assumptions and sourced limit in the appropriate optional task metadata without changing check policy or permissions.

#### Scenario: Refuse invalid or unsourced metadata

- **WHEN** a set request has an invalid unit, inconsistent bounds, missing estimate basis or unsourced limit
- **THEN** it fails without modifying prior budget metadata or authorizing any provider call.

#### Scenario: Show and reset one unit

- **WHEN** budget metadata contains both units and one unit is reset
- **THEN** show retains the other unit unchanged and reports the reset unit as absent.

### Requirement: Compare budgets with incomplete actuals

Diagnostics SHALL distinguish estimate ranges, sourced declared limits and
captured actuals. Token completeness MUST retain existing unknown attribution.
Known captured counts MAY establish exceedance despite incomplete actuals;
incomplete actuals MUST NOT establish an under-budget result. Managed interval
seconds SHALL be only an observed lower bound. No bytes, calls or seconds
SHALL be converted into tokens, prices or model downgrades.

Source: proposal.md; docs/tokens.md; design.md.

#### Scenario: Keep incomplete actuals from claiming under budget

- **WHEN** known captured counts are below a declared limit but actual usage is incomplete
- **THEN** the budget result remains incomplete and cannot claim the task is under budget.

#### Scenario: Report known captured exceedance

- **WHEN** known captured counts exceed a declared limit despite missing usage
- **THEN** diagnostics may report the known exceedance while retaining actual completeness and managed-time lower-bound qualifications.

### Requirement: Preserve existing workflow authority

Guidance and CLI help SHALL document concrete optional diagnostic, note and
budget examples with source references. No new stage, role, mandatory task
table, paid pilot/probe or always-loaded worker/reviewer diagnostic summary
SHALL be introduced. Compact MUST remain opt-in. Existing role write limits,
original selected skills, neutral independent review, human approvals and Git
gate authority MUST remain unchanged. Frozen experiments SHALL retain their
original scope without a general quality, saving or billing claim.

Source: proposal.md; docs/workflow.md; CONTRIBUTING.md.

#### Scenario: Retain existing workflow and source authority

- **WHEN** an ordinary or compact full/small task uses diagnostics, optional notes or budget metadata
- **THEN** existing lifecycle, approval, review, role limits and gate checks remain authoritative and no provider execution follows from recording metadata.
