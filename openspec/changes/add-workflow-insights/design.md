# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Diagnostic projection | Join existing captured evidence into a read-only task view | Existing token reports, check records, managed intervals, optional note reader, budget metadata | yes | Existing usage totals, runner records and prepared/finished interval storage remain authoritative; no second runner or collector |
| Optional note reader | Project bounded recognized authored tables with source references | Existing Markdown table reader | yes | Existing task Markdown parsing already ignores fenced examples; arbitrary prose remains valid |
| Budget metadata | Store optional unit-specific estimates or sourced declared limits | Existing task storage conventions | yes | Existing portable full/small task metadata conventions; check configuration and role permissions cannot represent estimates |
| CLI integration | Expose explicit diagnostics plus budget set/show/reset | Diagnostic projection, budget metadata, existing command registry | no | Existing grouped usage commands, help generation and completion |
| Guidance | Explain interpretation limits through on-demand examples | Existing docs and role texts | no | Existing source rules, reuse decisions and troubleshooting locations; no additional role or lifecycle |

## Interfaces and failure behavior

`kitsu usage show --diagnostics [--task <task>] [--json]` reads existing captured
token records, local run records and managed prepared/finished intervals. It
preserves exact known totals and subset accounting; missing, shared or
unmanaged attribution remains unknown. Original references are retained rather
than raw logs. Absent or malformed optional evidence is reported explicitly,
never as a zero count, pass, confirmed cause or verified claim.

Managed interval duration includes coordinator wait. Overlap contributes a
union of covered elapsed time; the report cannot infer worker runtime or a
causal critical path. Unfinished intervals and unmanaged work leave explicit
coverage gaps. Repeated failed/timed-out groups require the same check id,
content key and recorded command/paths policy key. No automatic retry or
change of check/review requirements follows.

The optional full-task reader recognizes `## Consequential claims` in
`proposal.md` or `design.md`, `## Decision notes` in `design.md`, and
`## Troubleshooting` in `tests.md`. The documented columns in
`docs/workflow-insights.md` are a projection convention, not a structural task
schema. Arbitrary prose remains valid. Project at most twenty open or
unclassified rows; closed/historic rows remain in original files. Omit long
cells explicitly with original references/hashes instead of truncating claims
or their negations. Missing or partial/unrecognized formats stay explicit.
Reviewer context remains neutral and original sources stay available; no
worker or reviewer packet receives an always-loaded diagnostic summary.

`kitsu usage budget set <task> --unit tokens|seconds` accepts optional `--min`,
`--max` and `--basis` for an estimate, repeatable `--assumption`, and optional
`--limit` with `--source` for a declared limit. `show` reads the task metadata;
`reset --unit` removes the selected unit. `--workflow full|small` selects the
storage on `set`; `--json` is available for all three actions. Full metadata is
`openspec/changes/<task>/usage-budget.json`; small metadata lives under
`openspec/changes/.usage/<task>/`. Invalid units, bounds, missing estimate basis
or missing limit source fail before modifying stored metadata. Missing,
malformed or conflicting task metadata is explicit rather than silently
creating a competing history.

Token actuals preserve capture completeness. Known captured exceedance is
reportable with incomplete actuals, while an under-budget claim requires
complete actuals. Managed elapsed seconds are only an observed lower bound.
Estimates and declared limits remain distinct from actuals and permissions;
neither storage nor display launches a provider call or enforces execution.

## Rules

| Rule | Checked by |
| --- | --- |
| Default usage output remains unchanged; explicit diagnostics are read-only | test |
| Exact known token totals preserve subset and unknown attribution semantics | test |
| Managed interval spans use overlap union without worker-runtime or causal claims | test |
| Optional note projection preserves original references without certifying authored claims | test, review |
| Repeated-failure groups bind check, content and recorded command/paths policy | test |
| Budget metadata is unit-specific, sourced and separate from execution authority | test |
| Incomplete actuals cannot establish an under-budget result | test |
| Existing review, approval, role limits, stages and compact opt-in remain unchanged | test, review |

The existing diagram records lifecycle transitions. This change instruments
existing records and adds optional notes, so no transition or diagram changes.
Frozen experiment artifacts remain unchanged. Validation uses local tests only.
