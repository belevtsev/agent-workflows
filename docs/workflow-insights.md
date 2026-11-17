# Workflow insights

Use optional diagnostics when you need to explain a task's captured usage,
managed intervals or repeated check failures. Add source-linked notes only for
uncertainty or tradeoffs that could change a consequential decision. These
tools add no stage, role, required table or universal checklist. Ordinary prose
remains acceptable, compact remains opt-in, and existing review, approval and
gate rules still apply.

## Inspect existing evidence

```sh
kitsu usage show --diagnostics
kitsu usage show --diagnostics --task fix-login
kitsu usage show --diagnostics --task fix-login --json
```

The report joins existing [token records](tokens.md), [check records](evidence.md)
and managed `task prepare`/`task finish` intervals. It is read-only: it runs no
check, starts no worker and contacts no model provider. Original record and
source references let you inspect the evidence; the report does not copy raw
logs. Missing records, unmanaged work and shared sessions remain unknown or
unattributed. A missing count never becomes zero.

Large captured consumers and stage shares are investigation candidates. Open
their original references before attributing a bottleneck or changing the
workflow; a large share alone supplies neither a cause nor a saving estimate.

Known captured token totals retain their exact values and completeness. Cache
counts are components of input accounting; reasoning, when reported, is a
subset of output. Do not add either subset to the total again. Diagnostics do
not allocate shared coordinator tokens to tasks or infer tokens from bytes,
calls, duration or requested model names.

The uncached counter is captured uncached input plus output, excluding cache
read/write counts. It is a token count, not a monetary charge.

An interval measures the span from managed preparation to finish, including
coordinator wait and bookkeeping. It is not actual worker runtime. Intervals
that overlap contribute their union to elapsed coverage, rather than having
their durations added twice. Unfinished intervals and work outside managed
intervals leave gaps. Overlap, stage totals and repeated rounds do not establish
a causal critical path or a reason for delay.

## Keep consequential uncertainty near its source

In an existing `proposal.md` or `design.md`, an optional `## Consequential claims`
table can make an important assumption explicit:

```markdown
## Consequential claims

| Claim | Kind | Source | Consequence if wrong | Revisit when | Status |
| --- | --- | --- | --- | --- | --- |
| Check reuse depends on declared paths | observed | docs/evidence.md, opened section "The same content" | A narrower rerun could miss changed dependencies | The recorded paths policy changes | open |
| The retry defect is limited to the login path | inferred | internal/login/retry.go:42, opened during this task | Other callers may retain the defect | A failing case names another caller | open |
```

The second row is an illustrative project path: replace it with a source you
actually opened. `Kind` is `observed`, `inferred` or `unknown`; it describes the
author's claim about its support. The source, consequence if wrong, revisit
trigger and status explain why the note matters. Even `observed` is an authored
claim, not machine-certified truth. Keep the original sources available and
let an independent reviewer inspect them with neutral context.

An optional `## Decision notes` table in `design.md` can record a consequential
tradeoff that the building-block reuse table does not already explain:

```markdown
## Decision notes

| Choice | Alternatives | Reason | Source | Revisit when |
| --- | --- | --- | --- | --- |
| Keep declared checks as the runner | Add a second runner | Preserve existing content and policy identity | docs/evidence.md, opened "A recorded passing run" | The existing runner cannot express a required check |
```

Short prose with the same information is also useful. Diagnostics project up to
20 open or unclassified rows from recognized optional tables and retain original
file references and hashes. Closed rows stay in the original files. Missing,
partial, oversized or unrecognized notes stay explicit; omitted text is not
interpreted as confirmation, agreement or absence of uncertainty. Notes do not
resolve open questions or replace independent reviews and human approvals.

## Investigate difficult failures

For a failure that needs more than a straightforward regression fix, keep only
useful hypotheses and experiments in an optional `## Troubleshooting` table in
the existing `tests.md`:

```markdown
## Troubleshooting

| ID | Check | Content key | Policy key | Hypothesis | Prediction | Run | Observation | Status |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| retry-1 | test | <content_key from the run> | <recorded_definition_key from diagnostics> | The retry timer survives cancellation | The cancellation regression fails before another request starts | <run_id> | Extra request assertion failed; source: kitsu/runs/test/<content_key>/<run_id>.json | open |
```

`test` is the existing check declared in this repository's `kitsu.toml`; use
your own declared check id in another project. The run reference uses the
existing store under the common Git directory. Copy actual content/policy keys
and the latest `last_run` id from your local diagnostic, replacing the
illustrative placeholders above. The `Run` cell matches that id exactly; keep
the original record reference alongside the observation. Open that record and its
retained log to distinguish an observation from a proposed explanation. An
observation or `confirmed` status may reference additional evidence while
remaining the author's claim. Keep historic and resolved rows when useful;
the bounded diagnostic view may omit rows, and the original file remains
available. Do not paste raw logs.

Diagnostics flag repeated failed or timed-out runs only within the same check,
content key and recorded command/paths policy key, keeping those two outcomes
separate. A changed command, dependency policy or content starts another group.
This `recorded_definition_key` binds the
recorded check id, command arguments and dependency paths; it is not a complete
task-policy hash. Repetition is a reason to examine the
hypothesis and prediction; it neither confirms a cause nor authorizes another
retry. Missing or unrecognized note evidence is unknown. The report does not
retry automatically, suppress checks or relax reviews.

When a later recorded run passes, the report retains the historical recurrence
and shows the newer outcome. An earlier failure group does not establish that
the check is still failing.

## Record an estimate or a sourced limit

Budget metadata is optional and separate from `kitsu.toml` and role permissions.
Use tokens and seconds as separate units:

```sh
kitsu usage budget set fix-login --unit tokens --min 100000 --max 250000 \
  --basis "Prior completed login fix, captured totals only" \
  --assumption "One implementation round" --assumption "Existing tests reproduce the defect" \
  --limit 300000 --source "Owner's task limit in the request of 17 November 2026"
kitsu usage budget set fix-timeout --unit seconds --min 120 --max 480 \
  --basis "Prior managed prepare-to-finish spans" --workflow small
kitsu usage budget show fix-login --json
kitsu usage budget reset fix-timeout --unit seconds
```

The example values and cited owner request are illustrative; substitute actual
sources and assumptions from your task. On `set`, use `--workflow full` for a
change folder or `--workflow small` for a lightweight task; use one workflow
consistently for the task. Full-task metadata lives in
`openspec/changes/<task>/usage-budget.json`, small-task metadata under
`openspec/changes/.usage/<task>/`. `show` reads both units; `reset --unit`
removes that unit's entry. An estimate requires `--min`, `--max` and `--basis`
together, with `0 <= min <= max` and optional assumptions. A declared limit
requires `--limit` and `--source` together with a nonnegative value. A declared
source is an authored reference, not proof of authorization. Recording a limit
never authorizes provider calls or paid work.

The diagnostic compares estimates and limits with available actuals while
preserving completeness. Token-budget actuals combine captured host usage with
separately measured adviser usage; unmeasured adviser calls leave actuals
incomplete. Captured tokens may establish a known exceedance even
with missing usage; incomplete actuals cannot establish that the task is under
budget. Managed interval seconds are only an observed lower bound on task
elapsed time, with the interval limitations above. Seconds and tokens are not
converted into each other, prices or model choices.

This metadata is not a hard cap, cancellation mechanism or runtime/payment
guarantee. Existing host controls and the user's authorization govern execution.
The feature adds no paid pilot or probe and establishes no general quality,
token-saving or billing result; historical [comparison evidence](benchmarks.md)
retains its original scope.
