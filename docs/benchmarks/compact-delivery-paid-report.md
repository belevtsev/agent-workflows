# Revised compact delivery: actual Codex comparison

**17 November 2026: both attempts completed and passed frozen acceptance,
original regressions and patch integrity. Revised compact used 1,872,131 tokens
versus 1,982,129 ordinary: 109,998 fewer, or 5.5% in this pair.**

Coordinator usage fell 21.9%, while worker usage rose 21.0%. Uncached input
increased 18.7%, so this is not evidence of a lower bill. No quality regression
was detected by the frozen checks; one pair cannot prove quality equivalence.
Full protocol certification remains unknown because native dispatch bodies are
encrypted. Compact remains opt-in. Paid execution is finished.

The [frozen plan](compact-delivery-comparison-plan.md) identifies the modified
binary, source patch, real pflag task, protected oracle and 10M approved budget.
Both attempts use Sol/high for the coordinator and every fresh worker, twelve
minutes per attempt, ordinary first and revised compact second. The binary is
based on main `e5d8c17` plus the uncommitted compact-delivery changes.

## Actual usage and outcomes

| Metric | Ordinary | Revised compact |
| --- | ---: | ---: |
| Total input + output | **1,982,129** | **1,872,131** |
| Input, including cached | 1,963,299 | 1,856,596 |
| Uncached input | 178,467 | 211,796 |
| Cached input, subset of input | 1,784,832 | 1,644,800 |
| Cache-write tokens | 0 | 0 |
| Output | 18,830 | 15,535 |
| Reasoning, subset of output | 3,019 | 3,017 |
| Solver execution seconds | 511.696 | 484.434 |
| Actual completed sessions | 5 | 5 |
| Complete accounting / terminal records | Yes / yes | Yes / yes |
| Independent acceptance / original suite / integrity | Pass / pass / pass | Pass / pass / pass |
| Full protocol certification | Unknown | Unknown |

| Actual session role | Ordinary tokens | Revised compact tokens |
| --- | ---: | ---: |
| Coordinator | 1,228,556 | 959,984 |
| Quality engineer | 292,774 | 331,169 |
| Developer | 170,872 | 190,798 |
| Reviewer | 195,936 | 176,759 |
| Git | 93,991 | 213,421 |
| All workers | 753,573 | 912,147 |

The shared ledger reconciles exactly to **3,854,260 tokens**, with no missing
counters, interrupted tails, token stops, replacement attempts, additional
rework workers or extra paid judge. **6,145,740** of the approved 10M remained
unused. No new accounting probe was run. Cached input and reasoning must not
be added a second time. The 27.262-second elapsed difference is exploratory
shared-host timing, not a demonstrated speed benefit.

## Quality and workflow audit

The private grader evaluated two top-level acceptance criteria with nested
unknown-flag behavior cases, ran the unchanged original `go test ./...` suite,
and checked patch integrity in an offline namespace. Both candidates passed.
The eligibility baseline fails the intended behavior and the reference passes,
with original suites passing on both; these were rechecked before paid execution.

Both traces show a meaningful unknown-flag failure inspected before the developer
started, then green evidence, a fresh source/diff/test reviewer and four role-write
validations. Both Git workers made a normal hooked commit, with current passing
gate evidence and a clean final tree. The original tests were preserved: ordinary
added 149 lines in `unknown_flag_forwarding_test.go`; compact added 136 lines in
`unknown_flags_test.go`, with no test deletions. Approved check declarations,
role configuration, go.mod and go.sum remained unchanged.

| Local artifact | Ordinary | Revised compact |
| --- | --- | --- |
| Final local task commit | `c5decb4c80c8255b2d9ea71afd620c014dfa5311` | `77799381bc217f775edf14587591722b12617e00` |
| Boundary checks | Four unmanaged role checks, including one repeated developer check | Four managed finishes |
| Expected check sequence | Meaningful red, then green | QE fail; code/review/Git pass |
| State snapshots / deltas | Not used | Not needed; zero retained snapshots |
| Retained structured worker returns | Not used | Not used; zero rejected ingestions observed |

All four compact packets match their stored hashes and contain their complete
original Codex role body exactly once. Their assignments include the complete
original behavioral contract. Each worker read its original saved packet; no
explicit worker call rereads the coordinator task skill or compact guide in the
retained traces. Selected extra skills are empty in this case, so skill-heavy
workflows remain untested. No managed interval remains active. Provided credential
copies were removed and the temporary model-service tunnel was shut down.

The ordinary coordinator inspected the regression failure at retained trace
line 124 before the developer launch. Compact inspected the original failure
log at line 93, finished QE with expected fail at line 108, and prepared the
developer at line 115. These are actual returned diagnostics, not merely a
worker claim or the existence of a failed check receipt.

Saved packets and primary command results support these observations but do
not prove equality to encrypted actual native assignment messages or completely
attest reviewer-input neutrality. Both reviewers inspected source/tests and ran
checks; no additional separate behavioral probe should be inferred. The export
therefore keeps certified `savings_ratio` null. The 5.5% above is the descriptive
difference in actual captured tokens, with frozen quality checks passing.

## Remaining overhead and recommendation

The coordination reduction is encouraging for this workload, but 158,574 extra
worker tokens offset much of it. The Git worker rose from 93,991 to 213,421;
its visible work included broad parser reads, discovery and reading all skill
receipts. Those requests count, though this pair cannot attribute the entire
increase to them. The ordinary coordinator also repeated a developer role check;
its overhead is included rather than replaced by an idealized baseline.

Selective observations are still imperfect. QE chose compact output for its
failing check and expanded the retained diagnostic; other workers used ordinary
check output. The compact reviewer requested a broad whole-file read, received
native envelope truncation, and made further scoped source/test reads. No
repository collection was necessary. Smaller instructions did not automatically
eliminate every unnecessary operation or establish complete source coverage.

Keep compact opt-in. The next useful work is offline: tighten Git-role evidence
delivery, avoid broad review reads, and calibrate critical-fact preservation and
verifiers against known misses and correct controls. Preserve source references,
complete original contracts, selected skills, independent review and current
gate authority. No further paid attempt is included or launched.

## Method and accounting scope

Compare ordinary unmanaged observations/handoffs with the revised opt-in package:
managed original worker packets, selective observations, minimal optional returns
and state views only when needed. Common cleanup keeps coordinator supervision
out of repeated assignments in both arms. The task's complete original behavior,
roles, selected skills, write bounds and final Git requirements remain intact.
This does not isolate the effect of a single compact feature.

Counters include the instrumented coordinator and all unique descendants,
including failures and rework. Cached input remains part of total input;
reasoning remains part of output. No new probe runs: exact unchanged CLI/runtime/
runner files allow reuse of the previously completed accounting foundation.
Prior probe usage belongs to the earlier study, not this one. Interactive study
preparation, research and posthoc reporting are outside these solver counters.
No provider bill is inferred.

The solver receives no reference solution, hidden oracle, previous candidates,
sibling workspaces, web or personal tools. Grading uses a separate offline
namespace after solver termination. Eligibility was rerun before execution:
baseline behavior fails, reference behavior passes, and both original suites pass.
Acceptance, original regressions, patch integrity, actual workflow completion,
observable protocol and accounting completeness are reported separately.

Exact native assignment messages are encrypted. Original saved packets, hashes,
source reads, commands and final artifacts can be inspected; exact dispatch
equality and reviewer-input neutrality remain unknown where the host provides
no direct evidence. Passing tests alone do not prove general quality equivalence.

## Verification article assessment

A read-only Sol/high research subagent assessed Addy Osmani's
[verification article](https://claude.dev/blog/how-the-claude-code-team-thinks-about-verification/).
Its useful mechanisms are protected executable checks, evidence bound to the
exact candidate, cheap checks first and calibration of the verifier itself.
Kitsu already separates worker claims from observed facts and binds checks to
current content. The strongest follow-ups are offline property/differential
tests of critical-fact preservation and calibration against historical missed
defects plus known-correct controls. These need no additional paid review layer.
The article supplies no evidence of Kitsu token savings and changes no oracle
or quality obligation in this experiment.

## Evidence and local verification

- [Run counters](compact-delivery-paid-data/runs.csv).
- [Every measured coordinator/worker](compact-delivery-paid-data/sessions.csv),
  including causal parent, role, model, effort, terminal status and token categories.
- [Frozen summary](compact-delivery-paid-data/summary.json), including binary/input
  hashes, approved limits, separate unknowns and ledger reconciliation.
- [Offline artifact audit](compact-delivery-paid-data/workflow.json), including
  unchanged controls, additive tests, original packet hashes, finishes and cleanup.
- [SHA-256 index](compact-delivery-paid-data/sha256.json) for the public exports.

Raw transcripts, candidates, grader logs, authorization, source patch and native
accounting remain private under `delivery-paired-20261117`. Public exports omit
credentials, raw prompts and private host paths. Hashes identify retained files;
they do not make provider behavior deterministically reproducible.

Preparation used an immutable previously validated runner and a newly frozen
production binary. Posthoc analysis used a separate executable and never
replaced measured binaries or inputs. Offline checks verify terminal session
tails, model/effort, all four causal workers, token-category sums and artifact
identity. Linux build, vet and the full race-enabled suite passed, as did
golangci-lint 2.15.0 and govulncheck 1.9.0 for Linux, Darwin and Windows targets,
generated help, documentation and all OpenSpec record checks. All four export
hashes verify. The [verification record](../../openspec/changes/evaluate-focused-delivery/tests.md)
describes their scope; ordinary tests invoke no paid entries. No CI or native
macOS/Windows runtime result is claimed.

## Limits

One public historical task and one attempt per arm cannot establish a general
quality or cost trend. Prior exposure, realized ordinary-first order, provider
cache variation and shared-host timing remain confounds. Comparisons with the
earlier paired outcome are descriptive, not causal attribution to the change.
Claude, mixed role-model defaults, skill-heavy workflows and large noisy
repository tasks were not measured.
Both arms use Kitsu's coordinated workflow with the four required roles; this
does not compare Kitsu against a single coding agent or a lighter small-task path.
The earlier actual +9.8% result and the separate 57-to-48 scripted call replay
remain unchanged; neither substitutes for this experiment's actual results.
