# Response-cycle fixes: paid Codex comparison

17 November 2026. Both attempts completed. Compact recorded **1,467,207**
input-plus-output tokens versus **1,655,932** ordinary: a descriptive difference
of **−188,725 (−11.4%) in this pair**, not proof of general savings. Both passed
protected acceptance, the original regression suite and patch integrity.

This follows the [frozen comparison plan](response-cycles-comparison-plan.md)
after the four [response-cycle improvements](../compact-response-cycles.md).
It evaluates the current local package, not released main, an isolated feature
or Kitsu against one agent. Compact remains opt-in.

## Actual usage

| Metric | Ordinary | Compact |
| --- | ---: | ---: |
| Total input + output | **1,655,932** | **1,467,207** |
| Input, including cached | 1,636,843 | 1,451,413 |
| Uncached input | 166,251 | 162,709 |
| Cached input, subset of input | 1,470,592 | 1,288,704 |
| Cache-write tokens | 0 | 0 |
| Output | 19,089 | 15,794 |
| Reasoning, subset of output | 3,092 | 3,721 |
| Uncached input + output | 185,340 | 178,503 |
| Solver execution seconds | 465.215 | 449.363 |
| Closed native sessions | 5 | 5 |
| Complete accounting / terminal records | Yes / yes | Yes / yes |
| Acceptance / original suite / integrity | Pass / pass / pass | Pass / pass / pass |
| Full protocol certification | Unknown | Unknown |

All ten sessions used `gpt-6.1-sol` with `high` reasoning: one coordinator and
fresh QE, developer, independent reviewer and Git workers per arm. No replacement,
additional rework worker, paid probe or extra paid reviewer ran. The per-session
totals and shared live ledger reconcile exactly to **3,123,139 captured tokens**,
leaving **6,876,861** of this experiment's 10M envelope unused. No token stop,
counter mismatch or unknown usage tail occurred.

Cached input counts once within input; reasoning counts once within output.
The total difference consists of −181,888 cached-input, −3,542 uncached-input
and −3,295 output tokens. Uncached input plus output differs by −6,837 (−3.7%).
These are token measures, not prices; a billing saving is not established.
Compact completed 15.853 seconds earlier (−3.4%); one ordinary-first pair on
a shared host cannot establish a general speed effect.

| Role | Ordinary tokens | Compact tokens | Compact minus ordinary |
| --- | ---: | ---: | ---: |
| Coordinator | 938,586 | 843,520 | −95,066 |
| Quality engineer | 238,460 | 174,797 | −63,663 |
| Developer | 182,230 | 206,217 | +23,987 |
| Independent reviewer | 179,913 | 114,432 | −65,481 |
| Git | 116,743 | 128,241 | +11,498 |
| All workers | 717,346 | 623,687 | −93,659 |

The coordinator, QE and reviewer differences are partly offset by developer
and Git growth. Retained outer native tool calls were 53 ordinary versus 54
compact: coordinator 26→25, QE 9→7, developer 7→9, reviewer 5→5 and Git 6→8.
An exec cell can contain multiple inner operations; these are not model-response
counts. The result does not establish that every role or interaction became
cheaper, or isolate which change caused the total difference.

## Quality and workflow evidence

Before any paid call, protected acceptance failed on the broken baseline and
passed on the known-correct reference; both original suites passed. Solvers
could not access that oracle, reference, prior candidates or sibling workspaces.
After each solver namespace closed, the protected offline grader ran the same
two top-level behavior criteria with nested cases and the original suite.

| Observed evidence | Ordinary | Compact |
| --- | --- | --- |
| Required QE → developer → review → Git steps | 4/4 complete | 4/4 complete |
| Intended assertion failure inspected before developer launch | Yes | Yes |
| Stored declared-check executions | 6: three failed, three passed | 4: one failed, three passed |
| Role-boundary validations | Four | Four managed finishes |
| Current gate / normal hooked commit / clean tree | Observed | Observed |
| New tests retained; original regressions and integrity | Pass | Pass |

Ordinary's coordinator inspected the actual unknown-flag failure at native
output line 115, 13:20:15.490Z; its developer began at 13:20:50.115Z. Compact
inspected the original failure log at output line 100, 13:28:22.815Z, before
its developer began at 13:28:47.053Z. The assertions concern known flags being
consumed after an unknown and shorthand clusters continuing past the stopping
point, covering both `Parse` and `ParseAll`; they are not infrastructure errors.
The test sets and iteration choices differ between arms, so passing these
checks is not proof of identical coverage or general quality equivalence.

Both independent reviewers inspected source/diff/tests and reported no findings.
The local task commits are `c1b8c0ebd8a7d4897574a022b204759ee6535919`
(ordinary) and `43057a4396884cd39c04fdacd4212de845101dec` (compact), produced
by the Git workers through installed hooks. Four compact finish receipts validate
write bounds: QE expects failing evidence; developer, reviewer and Git expect
passing evidence. No managed interval remains unfinished. An expected-red receipt
does not establish a green pass or certify semantic TDD.

## Which improvements were actually used

| Improvement | Observed compact behavior | Limit |
| --- | --- | --- |
| Complete fitting failed logs | QE used ordinary declared-check output; the coordinator read the retained original failure log. | The fit-aware compact check path was not selected; its evidence remains the offline regression tests. |
| Bounded QE recipes and tool observations | Original QE packet contained the five recipes and `rg` marked missing. QE used three typed searches, combined formatting/check execution on success and made no `rg` attempt or help call. | Other roles still attempted optional tools or requested help; availability guidance does not guarantee model compliance. |
| Conditional coordinator handoffs | All three intermediate transitions group finish then prepare, with preparation guarded by the finish exit code. | This demonstrates selected sequencing, not a causal count of avoided model responses or suppression of all routine messages. |
| Lean native launch | All four preparations returned `view:launch`; workers read the original saved packets. | The archival full-dispatch command was not selected in this paid run; both-host identity/recovery remains covered offline. |

All four saved packet SHA-256 values match their recorded identities, and their
stored original role instructions match the frozen source bytes. This fixture
has no custom selected skills; its empty receipts do not prove skill loading.
Encrypted native assignment bodies prevent full attestation of exact dispatch
equality and reviewer-input neutrality. Full protocol status, certified
`savings_ratio` and provider cost therefore remain unknown/null.

Posthoc `usage show --diagnostics` ran offline with the model command and API
tunnel removed. Kitsu's task CLI hook-token counters remain **unknown** in both
isolated workspaces. They neither replace nor invalidate the separately captured
native coordinator/worker counters; unknown must not be reported as zero.

## Evidence and interpretation

The production binary is built from a complete frozen source snapshot, including
non-ignored untracked files, based on `e5d8c17db6ff98aca59c1d7ae9b1590aa3d2cc7d`
plus local changes. Production SHA-256:
`6538c4e4e6c7a54e4544bb5f3ea76dc23bac81a58216fa9157e56cf0bb93324f`.
Frozen manifest SHA-256:
`14ec2fb98045ed6b5b926554d41ef7d1d01cc61e0ffd0dd92a30a627a154b55a`.

The [summary](response-cycles-paid-data/summary.json),
[runs](response-cycles-paid-data/runs.csv),
[sessions](response-cycles-paid-data/sessions.csv),
[workflow observations](response-cycles-paid-data/workflow.json) and
[hash index](response-cycles-paid-data/sha256.json) retain sanitized results.
Original private traces, frozen source, prompts, grader evidence and credentials
are not exported. The accounting runner and native runtime hashes match the
previously validated foundation; no new paid accounting probe was needed.

The [previous pair](workflow-insights-paid-report.md) recorded compact at +21.1%;
this follow-up records −11.4% with the same protected acceptance checks passing.
This is an encouraging descriptive result, not proof that the four fixes alone
reversed the overhead. One historical task, one attempt per arm, ordinary-first
ordering, cache behavior and different model/test choices limit the conclusion.
Keep compact opt-in. Broader offline coverage and targeted discovery guidance
remain useful; another paid experiment needs its own task and limits.
