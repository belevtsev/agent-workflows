# Codex compact-context pilot

Read the [completed report](benchmarks/codex-compact-pilot-report.md) for outcomes,
confirmed additional defects, usage and recommendations. The captured solver
counts are lower bounds; no pair has complete accounting in both conditions.

## Separate selective canary

The new [selective evaluation report](benchmarks/selective-compact-report.md) records offline
replay, v2 charset reference rejection and the status of a separate four-run canary. It does
not replace the completed study or its frozen reports, catalogs, grades, binaries or assets.
Replay counts all expansions and retries; some workloads use more bytes. The old charset
reference fails the expanded v2 contract and is rejected as v2 ground truth, rather than
silently accepting a defective reference or changing historical grades.

The canary plans two tasks with one attempt per arm: four total Sol/high runs, 480 seconds
each, one controller and no retry. All coordinators and descendants use `gpt-6.1-sol`/`high`.
Both arms use native short reads and scoped searches, original full worker instructions and
the same managed prepare/finish bookkeeping. Only noisy declared-check output differs:
ordinary logs versus bounded check results with retained-log expansion. This measures that
component; it does not test every selective retrieval choice at once. The short Go checks
use ordinary output in both arms, making that task a no-op observation selection.

The frozen profile is `selective-canary-v1`. Across all four planned cells, captured input
(including cached input) plus output is capped at 4,000,000 tokens. Stop at 3,250,000, leaving
a 750,000-token reserve for delayed usage and shutdown; require sufficient reserve before
another launch. Count every unique coordinator and descendant session once, including
failed and interrupted work. Reasoning is an output subset and is not added again. Missing
counters, incomplete tails and live/offline disagreements block further launches and remain
unknown in reporting. Polling captured counters cannot impose a hard provider billing cap.

Freeze this experiment under a new output directory. Report every planned cell, including
unstarted, timed-out or incomplete cells, with its status and any captured lower bound.
Do not infer a zero-token result, successful workflow or savings from an unstarted cell or
missing final usage. The separate report is the source of live outcomes; this procedure
alone makes no claim that the canary has run or demonstrated a benefit.

The 17 November execution stopped its first ordinary-output run at 3,257,291
captured tokens. Three cells stayed unstarted under the interrupted-tail stop
policy. The retained candidate passed independent tests, but no complete workflow
or on/off pair exists. See the separate report for the accounting and audit.

## Completed 24-run study

This experiment compares Kitsu compact observations and handoffs with ordinary
observations, using the same Codex small-task workflow in both conditions. It
does not compare a single agent with a team, or replace Codex's own compaction.

The completed design used 12 real historical bug fixes, one repetition per
condition, and two conditions: **24 solver runs**, with an eight-minute limit.
Every coordinator and worker uses `gpt-6.1-sol` with reasoning effort `high`.
The user replaced the original 72-run Sol/max plan with a smaller budget;
its 13 started attempts remain a separate partial dataset. Each task starts from a fresh
single-commit repository and a fresh Codex home. Advisers, personal plugins,
MCP servers and web search are disabled. The four required worker roles are
quality engineer, developer, reviewer and Git.

Before measured execution, freeze the task prompts, source trees, model and
host versions, toolchains, dependency manifests, role configuration, commands,
grading tests, calibration evidence and randomized plan. Order is randomized
within repetition, with a randomized condition order for each task pair.
Failed and timed-out attempts keep their assigned identity and spent tokens;
the harness does not silently retry them. Calibration is charged separately.

The experiment runs in Linux namespaces. The solver can read its selected
repository, selected dependency bundle and public tool instructions. Other
tasks, reference fixes, grading files and host personal files are absent.
Network access is restricted to an opaque CONNECT tunnel for the model service.
The grader runs after solver termination in a separate namespace with no
network. This also prevents a task's later source snapshot from revealing an
earlier task's fix.

Each grading test must first fail on the broken implementation for the
specified behavior and pass on the reference implementation. Existing relevant
test suites must pass on both; any adaptation required by an intentional
behavior change is documented before the experiment. Candidate tests cannot
rewrite these independent checks. Functional acceptance, regressions, patch
integrity, review findings, protocol compliance and infrastructure status are
reported separately.

Token accounting uses the final cumulative usage of every unique coordinator
and descendant worker session, with causal parent links and model/effort
verification. Standard-output usage repeats session usage and is not added a
second time. Input includes cached input; output includes reasoning output.
Unavailable categories and missing sessions remain unknown, never zero.
Token counts are not a monetary bill.

The report includes each task/repetition pair, successes and failures,
all-run usage, usage among paired verified successes, cached and uncached
input, output, reasoning, coordinator/worker attribution, and quality losses.
Concurrency is bounded and recorded. Shared-host elapsed times are exploratory.

Twelve repeated tasks are twelve task clusters. Even a pilot with no observed
loss cannot establish universal quality preservation. Public historical fixes
may occur in model training data, and the two Kitsu cases introduce self-evaluation
bias. These limitations constrain any recommendation to enable compact mode.

The opt-in, test-only harness is in [`internal/pilot`](../internal/pilot).
Ordinary `go test` does not spend model tokens. Its explicit entry point is:

```sh
go test ./internal/pilot -run '^TestCodexPilot$' -count=1 -timeout=0 -args \
  -pilot-action=freeze \
  -pilot-manifest=/absolute/private/input.json \
  -pilot-output=/absolute/private/results
```

After reviewing the frozen calibration and plan, use `-pilot-action=run` with
`-pilot-run=<assigned-id>`, `next`, or `all`. That explicit action spends model
tokens. A controller takes only its assigned cells, and paired arms stay on
the same controller. A claimed attempt cannot be silently rerun.

The manifest supports reasoning `high` or `max`, and one or three repetitions;
the calibration must match the selected model and effort. Set `repeats: 1`,
`effort: "high"`, `timeout_seconds: 480` for the completed replacement profile.
The twelve source snapshots must be **outside the result directory**. Freezing
rejects intersecting source/result paths and records source, prompt and asset
hashes. The [replacement catalog](benchmarks/codex-compact-high-pilot-cases.json)
and [original catalog](benchmarks/codex-compact-pilot-cases.json) identify exact
upstream revisions, prompts, dependencies and eligibility evidence. They are
provenance exports; a runnable private manifest needs local toolchain, solver,
grader, bridge and authorization paths.

Rollout directories must be clean paths relative to the fresh solver home;
portable slash paths and native Windows separators are accepted. Private grading
resolves directory aliases before comparing the attested repository identity.
Aliases do not permit a different repository, a candidate outside the trusted
root or a mismatch in baseline/candidate trees.

Build the solver executable once before freezing and retain its SHA256. Do not
replace it or its inputs while controllers are running. Protocol analysis and
blind-bundle preparation are separate posthoc helpers; build them under a
different path. In particular, frozen substring markers do not establish that
a compact command actually executed. Audit returned evidence and actual calls.

To recompute the sanitized analysis offline after execution:

```sh
go test ./internal/pilot -run '^TestPilotAnalysis$' -count=1 -timeout=0 -args \
  -pilot-output=/absolute/private/results \
  -pilot-analysis-output=/absolute/new-analysis
```

This reads immutable assignments, retained results and independent protocol
reviews. It makes no model calls and does not need the temporary credential
copies used during execution. `-pilot-action=report` is the older execution-time
summary; its full frozen-input validation requires those inputs still present.
Do not recreate authentication merely to analyze completed measurements.

The public [analysis JSON](benchmarks/codex-compact-pilot-data/analysis.json)
and CSV files contain runs, unique sessions, pairs, arms, cases and roles.
`functional_status` describes the **frozen tests**, not comprehensive code quality.
`protocol_status` separates verified, unknown and violated workflows.
`accounting_complete=false` keeps observed counts but excludes the pair from
complete-token ratios. A forced stop always leaves final in-flight usage unknown.
Input includes cache reads; output includes reasoning, so neither subset is
added again. An undefined comparison is not a zero-token result.

The [quality export](benchmarks/codex-compact-pilot-data/quality.json) contains
all 24 independent reviewer results, the arm mapping opened after review,
and deterministic posthoc confirmations. Three reviews timed out. The
[protocol export](benchmarks/codex-compact-pilot-data/protocol.json) records
observable mechanics and hash bindings; it does not certify encrypted briefs.

Keep raw traces, frozen assets, candidates, grading logs, cancellation records
and hashes private. Remove the experiment's temporary authorization copies
when model work ends, preserving the user's original credentials. Publish
sanitized measurements rather than a solver home. A new live run requires fresh
authorization and a new frozen output directory; it must not overwrite this study.
