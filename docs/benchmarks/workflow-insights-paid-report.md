# Current compact delivery: paid Codex follow-up

**17 November 2026: both attempts completed and passed protected acceptance,
original regressions and patch integrity. Compact used 1,984,340 captured tokens
versus 1,638,219 ordinary: 346,121 more, or 21.1% in this pair.**

No quality regression was detected by the frozen checks. This result does not
demonstrate token savings or general quality equivalence. Compact remains opt-in.
Paid execution is finished; no additional attempt is planned or running.

The [approved plan](workflow-insights-comparison-plan.md) defines one real pflag
unknown-flag forwarding repair, compact off/on, Sol/high for the coordinator and
every worker, twelve minutes per attempt and a new 10M captured-token envelope.
The current local build includes behavioral verification and workflow insights.
This compares the package, not an isolated feature or Kitsu against one agent.

## Actual usage

| Metric | Ordinary | Compact |
| --- | ---: | ---: |
| Total input + output | **1,638,219** | **1,984,340** |
| Input, including cached | 1,619,893 | 1,969,105 |
| Uncached input | 199,477 | 199,761 |
| Cached input, subset of input | 1,420,416 | 1,769,344 |
| Cache-write tokens | 0 | 0 |
| Output | 18,326 | 15,235 |
| Reasoning, subset of output | 2,504 | 3,228 |
| Uncached input + output | 217,803 | 214,996 |
| Solver execution seconds | 455.986 | 388.676 |
| Completed native sessions | 5 | 5 |
| Complete accounting / terminal records | Yes / yes | Yes / yes |
| Acceptance / original suite / integrity | Pass / pass / pass | Pass / pass / pass |
| Full protocol certification | Unknown | Unknown |

All ten sessions actually used `gpt-6.1-sol` with `high` reasoning. The records
include the coordinator and all QE, developer, reviewer and Git workers; none
are omitted for failing checks, rework or overhead. No extra rework worker or
replacement attempt occurred.

| Role | Ordinary tokens | Compact tokens | Compact minus ordinary |
| --- | ---: | ---: | ---: |
| Coordinator | 929,598 | 1,183,575 | +253,977 |
| Quality engineer | 176,601 | 329,825 | +153,224 |
| Developer | 152,777 | 190,065 | +37,288 |
| Independent reviewer | 242,914 | 167,330 | −75,584 |
| Git | 136,329 | 113,545 | −22,784 |
| All workers | 708,621 | 800,765 | +92,144 |

The shared live ledger and retained per-session counters reconcile exactly to
**3,622,559 tokens**. All sessions have terminal completion; no usage tail,
counter mismatch, token stop, replacement or extra paid reviewer is missing.
**6,377,441** of the 10M envelope remained unused. No paid accounting probe ran.

Cached input counts once within input; reasoning counts once within output.
The arithmetic difference in this pair is +348,928 cache-read tokens and −2,807
uncached input-plus-output tokens. Uncached input alone rose 284 tokens, or 0.14%; the
uncached input-plus-output sum fell 1.29%. These are separate token measures,
not prices. Neither a lower nor a higher bill is established.

Compact finished 67.309 seconds sooner, a 14.8% descriptive difference. One
ordinary-first pair on a shared host does not establish a speed improvement.

## Quality and workflow evidence

Offline eligibility was rerun before paid execution: the broken baseline fails
the intended behavior, the known-correct reference passes, and both original
suites pass. A protected grader then evaluated both final candidates in a
separate offline namespace. Both passed the two top-level behavior criteria
with nested cases, unchanged original `go test ./...` and patch integrity.

| Observed evidence | Ordinary | Compact |
| --- | --- | --- |
| Required QE → developer → review → Git steps | 4/4 complete | 4/4 complete |
| Meaningful red observed before implementation | Yes | Yes |
| Actual stored check executions | 5: two failed, three passed | 4: one failed, three passed |
| Role-boundary validations | Four | Four managed finishes |
| Added test lines / deleted test lines | 146 / 0 | 128 / 0 |
| Final gate / hooked local commit / clean tree | Observed | Observed |

The ordinary coordinator inspected the actual unknown-flag failure at native
trace output line 99, time 11:43:31.747Z; its developer session began at
11:44:19.069Z. It also repeated the failing check before implementation, and that
execution and its tokens remain included. Compact expanded the retained failing
check at output line 111, time 11:51:24.159Z, before preparing the developer at
line 130, time 11:51:41.797Z. These outputs show assertion failures about parsing
`--known` after an unknown flag, not an unrelated build or infrastructure error.

Both reviewers inspected source/diff/tests and made no file changes. Both Git
workers used normal installed hooks and left a passing final gate and clean
tree. Final local task commits are `3c3b4acf9c30517fe398b4050a502090e9ce1ab0`
(ordinary) and `a569dbecd483c50cb423437610ba91a2bac80fba` (compact). Frozen check
declarations, role settings, hooks and dependency files were preserved. The
compact QE appended tests to `flag_test.go`; it did not remove original tests.

All four saved compact packets match their recorded SHA-256 values and contain
their complete recorded original role body exactly once. Each managed interval
finished with validated write bounds; QE expected failing evidence, followed by
passing developer, reviewer and Git evidence. No interval remains unfinished.
Saved packets and observable reads do not establish equality to encrypted native
assignment messages or fully attest reviewer-input neutrality. Protocol remains
unknown, separate from the passing executable checks. The sanitized export keeps
certified `savings_ratio` and provider cost null.

## What the new diagnostics showed

After both solvers stopped, the frozen production CLI ran
`kitsu usage show --diagnostics --task pflag-unknown-flags --json` in each original
isolated workspace. These were offline CLI reads, with the model command and API
tunnel removed. They are outside measured solver time and tokens.

There are no Kitsu token-hook records in these isolated workspaces. The CLI
correctly reports captured host usage as **unknown**, while the harness retains
complete native accounting separately. This pilot therefore verifies native
accounting, not automatic end-to-end token ingestion by the CLI. Missing hook
counters were not filled with invented usage or reported as measured zero.

The compact task view finds four explicitly associated check runs and four
closed managed spans, with an observed union of **310.177 seconds** and no
unfinished or damaged interval. That is preparation-to-finish coverage including
coordination and waiting, not actual worker execution or complete task duration.
The ordinary task view has no managed spans or explicit task/check associations;
its five original raw check records remain available in the independent audit.
The diagnostic does not assign them to the task from timestamps.

Optional consequential notes, decision notes, troubleshooting notes and
`usage budget` metadata were not selected in this small-path repair. The approved
spending envelope was enforced by the separate experiment runner. This case
does not measure the benefit of those optional features or a difficult repeated
failure investigation.

## Where to improve next

The compact coordinator grew 27.3% and workers together grew 13.0%. QE accounts
for the largest worker increase. Review and Git differences in this pair are
−75,584 and −22,784 tokens respectively.
Retained code-mode cells rose from 13 to 18 for the coordinator and 5 to 12 for
QE. A cell can contain several tool calls; these are not individual tool or model
request counts and do not prove the cause of the token increase.

Visible compact coordinator work includes broad parser reads and a preview of
the QE packet. Managed preparation also adds skill-selection receipts that can
invalidate current check evidence; both arms properly refresh required evidence
before committing. These are concrete places to inspect offline, without
removing original role instructions, necessary checks or independent review.

Keep compact opt-in. Use this captured pair for replay tests of unnecessary
coordinator reads and repeated instruction delivery. Verify native token-hook
setup separately before treating a task-scoped CLI view as the complete usage
ledger. Broader or skill-heavy cases need their own design and approval; none
are launched by this report.

The earlier [5.5% decrease](compact-delivery-paid-report.md) remains unchanged.
The opposite direction here reinforces the limits of one task and one attempt
per arm; differences between studies are descriptive, not causal attribution to
the new workflow features. Prior exposure, fixed ordinary-first order, provider
caching and shared-host timing remain confounds.

## Reproducible evidence and limits

- [Actual run totals](workflow-insights-paid-data/runs.csv).
- [Every coordinator and worker](workflow-insights-paid-data/sessions.csv), with
  actual model/effort, parent, terminal status and token categories.
- [Frozen summary](workflow-insights-paid-data/summary.json), with approved
  limits, source/binary/input hashes and ledger reconciliation.
- [Workflow observations](workflow-insights-paid-data/workflow.json), including
  red ordering, stored executions, commits, packet hashes and diagnostic limits.
- [SHA-256 index](workflow-insights-paid-data/sha256.json).

The frozen binary SHA-256 is
`2c2a12aec955535dc88c16ff209fcccbbb92a5c0cb0636e1790796cbd946e642`.
Its source index SHA-256 is
`b7925f86edbdca51d7f9c7922aaec44ea8e45f387df8a2896d34265c61165fe4`.
The complete 722-file frozen snapshot includes non-ignored untracked packages;
the binary was built from that snapshot, based on main `e5d8c17` plus local edits.
It is not a measurement of released main. The immutable runner and exact native
runtime match the previously completed actual parent/child accounting foundation;
no fictitious new probe usage is added.

Raw transcripts, candidates, protected grading evidence, source snapshot and
offline diagnostic outputs remain private under `insights-paired-20261117`.
Copied credentials were removed and the model tunnel shut down. Public exports
omit credentials, raw prompts and private host paths. Study preparation and
posthoc reporting are outside the solver counters. One public historical task
cannot establish general quality, cost or token-saving results.

Local build/vet, race-enabled code and scenario tests, documentation checks and
three-target lint/vulnerability analysis passed. Runtime tests ran on Linux;
Darwin and Windows checks were static target analysis. Source-capture and
diagnostic-replay regressions run without a model. Ordinary test commands skip
all explicit paid entries. No CI or native macOS/Windows runtime result is claimed.
