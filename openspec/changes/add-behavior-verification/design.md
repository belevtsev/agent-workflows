# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Check metadata | Parse and validate optional `when`, `expect`, `verify`; retain independent `paths` semantics | Existing TOML and check-add paths | no | Existing declared commands and pattern validation |
| Change planner | Compare resolved revision with complete working tree; prioritize applicable checks and report remaining checks/gaps | Existing Git snapshot and declared checks | yes | Existing local diff/state machinery; no test discovery |
| Attempt coordinator | Pin first explicit base, require finished managed intervals, run targets then publish summary before final checks | Planner, existing check runner and evidence lookup | yes | Existing sequential runner, freshness and local logs; no duplicate runner/service lifecycle |
| Shared attempt records | Validate immutable sanitized source-bound historical summaries on full/small paths | Existing task storage validation | yes | Existing portable receipt conventions; raw logs remain private |
| Readiness projection | Derive current required verification from applicable policy, matching actual records and available logs | Attempt metadata and existing local evidence | no | Existing full-task stage/brief/compact state; small stage remains unknown |
| Role guidance | Explain original selected skills, actual app assertions/cleanup and coordinator-only summaries | Existing role source and generated host files | no | Existing skill resolution/loading; no new role or skill execution |

## Execution and identity

`check plan [--from <rev>] [--json]` defaults to HEAD and includes staged,
unstaged, untracked, added and deleted paths. Patterns change priority only;
every required gate check remains visible. Uncovered paths are gaps, never
evidence that no test is necessary. `paths` remains the dependency boundary
for reuse and cannot be repurposed as changed-path scheduling.
Compare regular-file working bytes with normalized snapshot blobs too, so
filters or CRLF cannot hide a trigger. Since no historical raw baseline exists,
unchanged normalized originals may conservatively remain scheduled. Raw-source
read failures must fail closed; dependency-based evidence semantics stay intact.

`task verify <task> [--from <rev>] [--workflow full|small] [--json] [--compact]`
requires the first explicit base, then reuses its resolved pinned revision.
Omitted workflow selects full when the task folder exists, otherwise small.
Active managed intervals must finish first. Targeted failure stops and leaves
remaining checks pending. Targeted success publishes historical sanitized
attempt data before final required checks, so checks cover tracked summary
content. Reuse is based on matching actual records, never the base. No tracked
writes follow final checks; unchanged successful repetition adds no summary/run.
Reuse requires matching private observations of raw filesystem bytes and actual
deciding records. First local verification runs applicable required checks even
with older Git-only evidence. Changes to raw source (including hidden/normalized
edits), verification policy, selected original sources or deciding records
require behavioral re-observation. Both hosts' selected sources participate in
the fingerprint; role execution and host choice remain unchanged.

Full summaries are under `openspec/changes/<task>/verification/`; small summaries
under `openspec/changes/.verification/<task>/`. Immutable `attempts/<sha256>.json`
bind source and policy; `binding.json` selects the current attempt.
Current readiness is recalculated using local evidence and log
availability. Clone-without-logs is unavailable. Structural `task check` can
validate shared history without local logs. Full done requires applicable
verification; small stage remains unknown with separate status. Metadata-free
repositories retain existing task behavior.

## Rules

| Rule | Checked by |
| --- | --- |
| Changed-path scheduling never removes full required checks or changes `paths` reuse | test, review |
| First base is explicit and pinned; untracked/deleted paths remain visible | test |
| Managed workers cannot publish summaries or verify during active intervals | test, review |
| Failure leaves remaining work pending; success writes history before final checks | test |
| Historical claims never substitute for actual current local evidence/logs | test |
| Structural task validation requires no private logs | test |
| Opt-in compact presentation preserves lifecycle, authority and evidence | test |
| Independent review, approvals, gate and original skill instructions remain authoritative | test, review |

Only affected archify JSON sources and their browser-exported SVGs are updated.
Historical calibration assets and paid results remain unchanged. Validation
does not claim exhaustive program correctness or general token/billing benefit.
