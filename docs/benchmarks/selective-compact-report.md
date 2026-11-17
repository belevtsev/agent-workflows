# Selective compaction evaluation

This evaluation keeps compact mode opt-in. It checks whether applying compact
output to noisy checks can preserve useful evidence without imposing the same
overhead on short reads and short checks. The earlier
[24-run study](codex-compact-pilot-report.md) remains unchanged.

**Result:** the shared reserve stopped the first ordinary-output run at 3,257,291
captured tokens. Its candidate passed independent acceptance and original-suite
checks, but the Git stage and final task completion were missing. The other three
assigned runs remained unstarted. There is no completed on/off pair and no new
evidence of workflow token savings or quality equivalence.

## Offline evidence

The five local replays exercise actual CLI operations and compare the facts
recovered by each path. They count request and response bytes, calls, original
instruction reads, expansions and failed expansion attempts. They invoke no
model. Bytes are not tokens, provider cache savings or billing measurements.

| Replay | Ordinary calls / response bytes | Selective calls / response bytes | Evidence recovered |
| --- | --- | --- | --- |
| Short source | 1 / 60 | 1 / 60 | Exact short source, read natively in both paths. |
| Last search match | 1 / 9,263 | 4 / 13,716 | Match omitted from the first compact page, recovered after three expansions. |
| Long UTF-8 line | 1 / 7,231 | 4 / 9,597 | Complete long line; failed small-page attempt and retry are charged. |
| Buried failed check | 2 / 12,767 | 7 / 26,807 | Middle-of-log diagnostic plus actual failed gate verdict; six expansions. |
| Saved handoff | 6 / 5,247 | 3 / 7,330–7,332 | Canonical worker instructions and original skill source both read. |

These adversarial replays preserve their checked facts but often cost more bytes.
The handoff reduces CLI calls while increasing returned bytes. Three single-pass
benchmark repetitions are an accounting check, not a statistically supported
timing comparison. See the [raw replay receipt](selective-compact-data/replay.txt).

The versioned charset probe checks eleven behaviors, including both previously
confirmed regression families. The baseline fails the original quoted-charset
case. The reference implementation fails all three extended-parameter override
cases and is rejected as v2 ground truth. The prior compact candidate fails those
three cases; the prior ordinary candidate also fails both stray-quote cases.
These are diagnostics of retained old candidates, not newly executed model runs
or a revision of the old frozen scores. See the
[versioned oracle evidence](selective-compact-data/charset-v2.json).

## Frozen comparison protocol

Four runs are planned: `pflag-unknown-flags` and `qs-sparse-array-limit`, each
with ordinary and selective check output. Coordinators and all role workers use
`gpt-6.1-sol` with `high` reasoning. Each run has a 480-second limit. Runs execute
sequentially, completing or stopping one pair before the next pair starts.

Both arms use the same updated Kitsu executable, role policies, original task
instructions, saved worker briefs, prepare/finish bookkeeping and native source
reads. Only noisy declared-check output changes: the selective JavaScript arm
uses compact check output and expands retained logs when needed. The short Go
checks use ordinary output in both arms. This intentionally tests a no-op
selection on the short-output task rather than blanket compaction.

Local preflight returned 88,928 ordinary bytes versus 1,582 compact bytes for the
JavaScript suite, and 147 versus 1,511 for the short Go suite. These are specific
check observations; they do not predict whole-workflow token usage.

The approved overall ceiling is 4,000,000 captured tokens, counting cumulative
input including cached input, plus output, across all coordinators and causal
workers. Reasoning is already an output subset. A shared 3,250,000-token soft stop
reserves 750,000 tokens for in-flight and final capture. The monitor polls every
500 ms and stops further launches after interrupted or unknown accounting.
There are no automatic model retries or additional paid posthoc reviewers.

Discrete captured counters cannot enforce a hard provider billing limit. Missing
or interrupted tails remain unknown, never zero. Every attempted run is charged;
unstarted runs remain in the assignment table. A forced stop is incomplete
evidence and cannot count as a token saving. Prior analogous runs would exceed
this budget, so all four completions are not promised.

Reference solutions and private graders are outside each solver's isolated
workspace. Original suites and independent acceptance/regression checks run
offline after each attempt. Passing tests, completing workflow steps, satisfying
the observation assignment and complete token accounting are reported separately.
Only a completed, audited pair with comparable quality can support a token ratio.
Two tasks cannot establish general quality equivalence or universal savings.

## Live results

The source and inputs were frozen on 17 November 2026 before model execution.
Source base: `e42556d9882a6067a538a33be53870106e4ab2c7`, with the exact new source
bytes and uncommitted diff retained separately. An initial offline build failed
on the temporary filesystem's quota; its logs were retained and the build was
repeated with disk-backed temporary storage. That attempt invoked no model.

| Frozen artifact | SHA-256 |
| --- | --- |
| Kitsu executable | `5b3ffd0192793379b7321017415d66aa680f94a5a26c379b8a1d444a4a902cd6` |
| Pilot executable | `4aad090b90f962a37ab9e2b9507e6a62e9882c1a55ba8e413bd83356e7fd4669` |
| Source index | `8f5f1e2118e77f206fcce79040e4deaaf5fb0cc28f3ab5e6cd0e9255c3b33067` |
| Manifest | `9353db0a442853e04c15c29b0b0e63a4c8f737bba93e890bd2e16ce24836da55` |
| Assignment plan | `10189904e0e714ea70c44ccedf97872ab6a46e468967753597f3857b220edc2e` |

The seeded order was JavaScript ordinary, JavaScript selective, Go selective,
Go ordinary. The experiment's token totals exclude implementation conversations
and unrelated historical runs.

After this freeze, integration review added a test-only CLI regression file for
26 malformed request cases and typed-command recovery. Production code and all
experimental inputs remain the frozen bytes above. Reporting edits also occur
outside those retained experimental snapshots.

| Assigned run, in order | Execution | Captured tokens | Functional result | Workflow |
| --- | --- | --- | --- | --- |
| `qs-sparse-array-limit-r1-normal` | Reserve stop after 421.019 s | 3,257,291; interrupted tail unknown | 18/18 acceptance criteria, original suites and patch-integrity checks passed | QE, developer and reviewer intervals finished; Git worker and final commit absent. |
| `qs-sparse-array-limit-r1-compact` | Unstarted: stop policy | Unmeasured | Not run | Not run |
| `pflag-unknown-flags-r1-compact` | Unstarted: stop policy | Unmeasured | Not run | Not run |
| `pflag-unknown-flags-r1-normal` | Unstarted: stop policy | Unmeasured | Not run | Not run |

The measured invocation ran from 05:25:21 to 05:32:22 UTC on 17 November 2026
(07:25–07:32 in Kyiv). The final live ledger and retained rollout accounting
agreed on the captured total. The threshold-crossing update exceeded the soft
stop by 7,291 tokens, leaving 742,709 below the approved captured ceiling.
Cancellation makes the final in-flight usage tail unknown; the reserve is not
evidence that the tail was zero. No later model run or retry was launched.

| Captured category | Tokens |
| --- | ---: |
| Uncached input | 184,923 |
| Cached input | 3,055,616 |
| Cache-write input | 0 |
| Output | 16,752 |
| **Total input plus output** | **3,257,291** |
| Reasoning, already included in output | 3,380 |

| Actual session | Captured input plus output |
| --- | ---: |
| Coordinator | 1,856,554 |
| Quality engineer | 763,962 |
| Developer | 283,876 |
| Reviewer | 352,899 |

These are observed cumulative provider counters, with repeated cumulative events
deduplicated across the four causal sessions. They are a lower bound after
interruption, not a complete-task total or monetary bill. The coordinator
accounts for about 57% of the captured total. Most captured tokens are cached
input; no provider cache price or monetary saving is inferred.

The private grader rechecked case eligibility: the baseline fails the independent
acceptance contract, the reference passes it, and both pass the original suites.
The retained candidate passed all eighteen independent acceptance criteria and
the complete original parse/stringify/utils suites. Patch-integrity checks found
no protected-test or check-policy changes. This supports the candidate's tested
behavior; it does not certify the unfinished coordinated task or general quality.

The frozen automatic command-text scan also flagged three apparent compact calls
in the ordinary arm. Independent inspection found these were flag mentions in
policy text or allowed JSON metadata, not executed compact checks. This initial
scan is retained; the independent audit records the distinction. Actual workflow
completion remains false because the Git stage and commit are absent.

The detailed audit observed a real red regression run (48 failures among 1,768
tests) before the developer started, followed by 1,768 live tests passing. All
three workers directly read and hashed their canonical saved packets; their
managed finishes passed the applicable write-bound and check-evidence checks.
The review assignment contained neutral facts and the original role, and the
reviewer ran separate behavior probes and checks. Native assignment messages
remain encrypted, so exact equality of those payloads to the saved dispatch
messages cannot be independently proved. The observed packet reads do not remove
that limitation or certify full protocol compliance.

The [frozen policy](selective-compact-data/canary-policy.json) and
[assignment plan](selective-compact-data/canary-plan.json) retain the budget,
model, arm and content-hash bindings. Detailed exports and independent trace
audit accompany the final evidence below. The
[outcome record](selective-compact-data/canary-outcome.json) includes grading and
cleanup status; [run rows](selective-compact-data/canary-runs.csv) retain every
planned identity and [session rows](selective-compact-data/canary-threads.csv)
retain the captured token components. Model processes and the model tunnel were
stopped, copied credentials removed, and the original credential hash preserved.
The [independent trace audit](selective-compact-data/canary-audit.json) records
verified local facts and remaining unknowns; its
[evidence index](selective-compact-data/canary-audit-evidence.json) binds the
retained records. All fourteen public canary files passed their
[SHA-256 checks](selective-compact-data/canary-sha256.json).

## What this supports

Selective routing is implemented and locally regression-tested. Ordinary short
reads retain their direct path. Noisy checks can expose much less output while
retaining expandable evidence, but complete adversarial recovery can exceed
ordinary output. Full original role instructions and skills remain available.

The approved budget could not fund even one complete workflow on the first
selected task, so it could not fund this four-run comparison. The result cannot
be used as a compact-mode failure rate: no compact model run started. It also
cannot be used as evidence that the new handoffs save whole-workflow tokens.

Keep compact mode opt-in. Use the offline replays to test recovery and accounting
before spending on another experiment. A future paid comparison needs a budget
that can plausibly cover both complete arms, or a separately declared narrower
workflow. Changing models, dropping reviews or using incomplete runs as savings
would answer a different question. No further paid experiment is authorized or
launched by this change.

## Local verification

Build, vet, the full Linux race suite and the pinned lint/vulnerability tools
passed. Lint and vulnerability scans covered Linux, macOS and Windows targets;
native macOS/Windows runtime tests were not run here. Markdown links, generated
command help, documentation index, changelog and OpenSpec rules are checked by
the repository tests and final declared gate.

These focused commands invoke only local fixtures and fake processes:

```sh
GOWORK=off go test ./internal/cli -run 'TestSelectiveCompact|TestCompactSelectiveReplay|TestCompactCollectorRequestErrors' -count=1
GOWORK=off go test ./internal/pilot -run 'TestCanary|TestCharsetRegressionV2' -count=1
GOWORK=off go test ./internal/cli -run '^$' -bench '^BenchmarkCompactSelectiveReplay$' -benchtime=1x -count=3 -benchmem
```

The OpenSpec change record maps the regressions, validation receipts and bounded
execution outcome. Reproducing the historical source probes requires the retained
separate source inputs; the public probe, contract and raw observations are
included above. Ordinary tests never start the paid Codex pilot.
