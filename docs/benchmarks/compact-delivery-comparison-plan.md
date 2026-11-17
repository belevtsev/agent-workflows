# Revised compact delivery: prepared paid comparison

Status: both approved attempts completed; paid execution is finished. The
[completed report](compact-delivery-paid-report.md) records actual usage,
quality and limits. No new paid accounting probe was included.

## Frozen implementation

Use main base `e5d8c17db6ff98aca59c1d7ae9b1590aa3d2cc7d` plus the current
compact-delivery edits. This is a modified local build, not released main.

| Identity | SHA-256 |
| --- | --- |
| Production binary | `242ba61daaa1280cf5da77e594e5cbe3ad464c7d5524eefdc4526d8ed876e398` |
| Production/guide source patch | `d1b6bcc4b4afb28f88156ed146e30c24683d44f814dff211d226a3f329bca8d2` |

The private `delivery-paired-20261117` directory retains the binary, source
patch, task snapshot, prompts, manifest, seeded plan and original grading inputs.
The unchanged immutable runner retains its strict `main-paired-v1` topology and
budget validation; `experiment.json` separately identifies this revised build.
No historical study or frozen binary is overwritten.

## Design and approved limits

- One real external task: the unchanged historical pflag unknown-flag forwarding
  repair. One compact-off attempt and one revised compact-on attempt, sequentially.
- Frozen order: ordinary first, compact second. Both use the same new binary,
  original behavioral contract and required quality obligations. Common delivery
  cleanup applies to both arms; only the compact arm uses managed handoffs and
  selective observations. This tests the package, not a single feature.
- Coordinator and all fresh QE, developer, independent reviewer and Git workers
  use `gpt-6.1-sol` with `high` reasoning and no inherited coordinator turns.
- Twelve minutes per attempt; no replacements, automatic additional attempts,
  extra paid judge or task expansion.
- Approved new captured-token ceiling: **10,000,000 total**, including cached
  input and every unique coordinator/descendant. Soft stop at **9,000,000**;
  retain **1,000,000** for delayed accounting/shutdown. Require 5.4M of remaining
  total capacity, including that reserve, before another launch. Discrete counters
  cannot guarantee a hard bill.
- Reuse the already completed actual native parent/child accounting preflight
  because exact Codex CLI/runtime/runner files are unchanged and checked offline.
  No paid probe is necessary; prior probe usage remains with the earlier study,
  never counted as a fictitious new call. Current session counters are fresh.
- Stop later launches if usage is missing, inconsistent or has an interrupted
  unknown tail. Count all failed/reworked workers and stopped attempts honestly.

The earlier 10M approval named the completed `b263394` experiment. This is a new
build and experiment; the owner approved its separate spending envelope before
the first model call.
The earlier pair captured about 4M tokens; that is context, not a forecast or
a guarantee that both new attempts will fit the proposed limits.

## Verification before and after execution

Offline eligibility has already been rerun: the broken baseline fails the
unchanged hidden behavior oracle, the reference passes, and both original suites
pass. The solver cannot see the reference, hidden oracle, earlier candidates,
personal host files or sibling workspaces. Dependencies remain offline.

Require all four workers, inspected meaningful red before implementation,
green evidence, neutral independent review with no unresolved blocker/major
findings, validated role writes, a final hooked local Git commit and a clean
tree. Do not skip these requirements to meet a token/time target.

Report frozen acceptance, original-suite regressions, patch integrity, workflow
completion, observable protocol and accounting separately. Retain each unique
root/descendant's actual cumulative input/output, cached subsets and reasoning
subsets; require final session completion. Provider costs are not inferred from
bytes or uncached input alone. Exact encrypted native dispatch equality and
reviewer-input neutrality remain unknown where the host does not expose them.

The [verification article](https://claude.dev/blog/how-the-claude-code-team-thinks-about-verification/)
supports protected executable checks, evidence tied to the candidate and verifier
calibration. Kitsu already applies current-content gate evidence and strict
untrusted claims. This experiment adds no paid review layer or gate bypass;
broader offline property testing and evaluator calibration remain separate
possible improvements. One pair cannot establish general quality equivalence.

## Offline preparation

`TestFocusedPrepareOffline` requires an explicit new private root, builds Go and
copies only approved inputs. Ordinary tests skip it. It invokes no model.
Preparation, baseline/reference eligibility, source freezing, pilot regressions,
vet and pinned lint passed locally. Paid execution remains a separate explicit
action after confirmation of this plan's budget, which the owner provided.
