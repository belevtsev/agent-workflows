# Design

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Coordinator projection | Bounded referenced task, interval, evidence, policy and prerequisite sections | Existing task reports, evidence judge, roles/skills, adviser checkpoints and Git state | yes | Ordinary views remain compatible; full raw records are unnecessary in each observation |
| Snapshot baseline | Bind a complete stable projection to task/worktree/schema and retain it privately | Existing contained stage ledger, exclusive lock and atomic writes | yes | Context artifacts require identical source on load, so cannot serve historical delta bases |
| Section delta | Replace only changed sections after loading a valid full baseline | Coordinator projection, Snapshot baseline | yes | Existing content keys and JSON serialization provide deterministic comparisons |

## Rules

| Rule | Checked by |
| --- | --- |
| Ordinary task views stay compatible and compact views are explicit | test |
| Snapshot sections are complete or the response budget is refused | test |
| Source, raw task history, effective external skills and time-dependent evidence remain observable | test |
| Canonical finding references remain distinct from untrusted worker claims | test |
| Applying every delta reconstructs the next complete projection | test |
| Unknown, mismatched, expired, unsafe and incomplete bases cannot produce a delta | test |
| Private bounded snapshot retention never evicts managed intervals or receipts | test |
| A view never runs a model/check/adviser, validates writes or certifies completion | test, review |

## Consistency and authority

Collect the same complete projection twice under the existing worktree interval
lock. Check Git/HEAD, raw tracked sources and raw task-history keys at observation
boundaries. Include effective role instructions/model/reasoning, selections and
original skill hashes for both hosts, current evidence verdicts and gate/index
state. Reject changing observations rather than saving a mixed baseline.

Snapshots are content-bound, versioned and worktree/task-local, limited to 64
entries, 64 MiB and 24 hours. Output budgets include the complete envelope and
refuse rather than silently omit mandatory state. Returned sections replace
entire prior sections; absent sections are unchanged, explicit empty states clear
facts. Full baselines are retained behind delta responses, allowing safe chains.

Projection scope stays local and factual. Full tasks expose the first unfinished
recorded stage, not every future semantic obligation. Small history remains
unknown. Active handbacks retain counts/severity/reference and untrusted scope;
closed returns are not a finding-disposition database. Never pass author prose
as independent review context. Hashes are bookkeeping, not filesystem attestation.

Offline correctness and serialized byte reductions do not establish provider
token savings or real-task quality equivalence. No host worker or paid experiment
is started by these changes.
