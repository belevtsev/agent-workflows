# Codex compact-context pilot: real-task evidence

**The 24-run Sol/high pilot is complete. It does not justify enabling compact
mode by default or claiming a task-wide token saving.** Both conditions passed
the frozen behavioral and regression checks on all twelve tasks, but blind
review exposed defects those checks missed. Compact runs also completed less
of the required workflow within the eight-minute limit. Forced stops left no
task pair with complete usage on both sides, so the token comparison is
inconclusive.

The user selected this smaller replacement after interrupting the original
Sol/max phase. Both phases are retained separately and are not pooled.
The [case catalog](codex-compact-high-pilot-cases.json) contains exact prompts, source
and reference revisions, archive hashes, commands, eligibility and adaptations.

## What the comparison measures

Both conditions use a Codex coordinator and fresh quality-engineer, developer,
reviewer and Git workers. All sessions use **`gpt-6.1-sol`, reasoning `high`**.
The difference is Kitsu's observation and handoff interface:

| Condition | Evidence and handoffs | Checks |
| --- | --- | --- |
| Compact off (`normal`) | Ordinary native reads/searches/Git; separate role, model and skill queries | Ordinary `kitsu check run` |
| Compact on (`compact`) | `kitsu task brief`, bounded `context collect/show`, expandable original evidence | `kitsu check run --compact` |

The task contract, role limits, tests-before-code rule, independent review,
finding dispositions, Git hooks and required final commit stay the same.
Compact mode may fall back to a permitted raw read when a necessary fact is
unavailable; the worker must explain the fallback. Neither condition enables
advisers or changes models. Review context excludes the author's opinion.

This is a comparison of **Kitsu's compact interfaces**. Codex's native tool
batching, Code Mode and automatic conversation compaction are host behavior
left unchanged in both conditions. It does not compare a single agent with
a team or demonstrate that arbitrary semantic summaries preserve quality.

## Frozen design and environment

| Parameter | Value |
| --- | --- |
| Real tasks | 12 historical repairs across seven repositories |
| Repetitions | One per task and condition |
| Measured solver identities | 24; 12 task pairs |
| Model and reasoning | Sol/high for coordinator and every worker |
| Topology | Kitsu small path, four fresh roles even for a tiny fix |
| Randomization | Seed `20261116`; shuffled tasks and randomized condition order within each pair |
| Concurrency | Six fixed controllers; each pair's conditions run sequentially on its assigned controller |
| Run limit | 480 seconds per solver attempt, identical in both conditions |
| CPU settings | `GOMAXPROCS=2`, Go build parallelism two; Node suites use one test job |
| Codex CLI | `0.169.0` |
| Language tools | Go `1.27.4`, Node `24.11.1`, GCC/G++ `15.2.0` |
| Host | Linux `7.0.0-43-generic`, AMD Ryzen 9 3900X, 60 GiB RAM |
| Git | `2.53.0` |
| Source state | Kitsu `1069e4bd1501197f61631841c3a39e83fda0ed29` plus the retained uncommitted compact-engine/harness patch |
| Frozen manifest SHA256 | `36e0158fe0add361e4b2c4d378f2347a2d7d3205af176d17c8481a195ae984ba` |
| Kitsu executable SHA256 | `9c529970789862bfd04306410b7786287ef88162a92705ab932f1d71ff1d24b3` |
| Frozen harness executable SHA256 | `11d131d8f4eb84dfb0fa8632f20e5c72a20f0805cf2e0fb01b6ada64112b5091` |

Every run receives a new single-commit repository, a fresh Codex home and
the same approved check declarations and role settings. Personal plugins,
apps, MCP servers, web search and global user configuration are disabled.
The public Kitsu task skill is loaded explicitly rather than relying on a
personal plugin installation. Hooks are installed before solver execution.

All assigned identities remain in the dataset. The harness claims each once;
timeouts, failed attempts and incomplete accounting are not silently retried
or replaced. A missing usage record remains unknown, not a zero-token attempt.

## The real tasks

| Task | Upstream | Language | Requested behavior |
| --- | --- | --- | --- |
| `pflag-unknown-flags` | [spf13/pflag](https://github.com/spf13/pflag) | Go | Stop non-interspersed parsing at the first forwarded unknown flag; preserve long, short, clustered and ParseAll behavior |
| `chi-quoted-charset` | [go-chi/chi](https://github.com/go-chi/chi) | Go | Accept valid quoted charset parameters without confusing quoted semicolons or other parameter values; retain legacy behavior |
| `semver-prerelease-groups` | [Masterminds/semver](https://github.com/Masterminds/semver) | Go | Apply prerelease opt-in within the appropriate AND group and keep Check/Validate consistent |
| `chi-self-mount` | [go-chi/chi](https://github.com/go-chi/chi) | Go | Reject self-mounts and shared-tree aliases immediately while preserving valid distinct routers |
| `qs-sparse-array-limit` | [ljharb/qs](https://github.com/ljharb/qs) | JavaScript | Count defined sparse-array elements toward the limit while retaining overflow, append and compaction semantics |
| `qs-empty-array-keys` | [ljharb/qs](https://github.com/ljharb/qs) | JavaScript | Encode empty-array keys consistently with formatter/encoder options and preserve round trips |
| `minimatch-split-budget` | [isaacs/minimatch](https://github.com/isaacs/minimatch) | TypeScript | Raise RangeError on total preprocessing-cell exhaustion without changing the separate brace pattern-count cap |
| `minimatch-escaped-patterns` | [isaacs/minimatch](https://github.com/isaacs/minimatch) | TypeScript | Keep escaped leading markers and braces literal in normal and Windows-style modes |
| `fmt-long-double-precision` | [fmtlib/fmt](https://github.com/fmtlib/fmt) | C++ | Format extended-precision values below powers of ten correctly in scientific, fixed and general notation |
| `fmt-null-printf-precision` | [fmtlib/fmt](https://github.com/fmtlib/fmt) | C++ | Handle null C-strings with printf precision safely while retaining null, padding and valid-string conventions |
| `kitsu-path-completion` | [belevtsev/kitsu](https://github.com/belevtsev/kitsu) | Go | Complete home-relative paths, spaces and directory-only options with native separators |
| `kitsu-bash-login` | [belevtsev/kitsu](https://github.com/belevtsev/kitsu) | Bash, Go tests | Make fresh interactive and login Bash sessions find an installed command, preserving startup-file precedence and opt-outs |

The catalog pins each broken revision and reference repair. Test scope is
deliberate: complete Go suites for pflag, chi and semver; existing parser,
stringifier and utility suites for qs; complete configured TAP suites for
minimatch; fmt's relevant upstream formatting/printf suites; and Kitsu's
existing completion or installer scenario suites. Passing does not mean every
upstream CI platform or optional suite was exercised.

## Eligibility, corrections and isolation

Before measured runs, independently authored behavioral checks were run on
both broken and reference source. All 12 broken versions fail acceptance for
the intended behavior; all 12 reference repairs pass acceptance and frozen
regression checks in the actual isolated Go grader. All 24 integrated grader
checks have successful infrastructure status. The 12 public solver environments
also pass their approved baseline checks.

Three original-suite adaptations were necessary and frozen in advance:

1. Semver's original test calls a private comparator whose signature changes
   in the reference repair. A signature-neutral invocation preserves the
   original input/assertion rows; separate public-API acceptance checks behavior.
2. The minimatch cell-budget repair intentionally replaces silent truncation
   with RangeError. Contradictory original over-budget assertions are adapted;
   below-budget matching and the separate brace cap remain covered.
3. The Windows leading-`!` escape representation changes a `hasMagic` expectation.
   That narrow expectation is adapted while independent literal/non-negation
   acceptance remains required.

These adaptations affect the private grader, not an unseen post-result change
to the scoring rule. A broken implementation can therefore fail an adapted
new-behavior regression as well as acceptance. Existing original base-suite
eligibility was established separately before that adaptation.

Prefreeze curation also rejected a gjson overflow case whose reference repair
fails on the pinned toolchain. An unsupported Bash shell-fallback requirement
was removed from the Kitsu completion prompt after the independent check showed
that the actual upstream fix covers filesystem completion. The minimatch prompt
was clarified to distinguish total-cell exhaustion from brace-count truncation.
The case catalog retains these corrections and the rejected candidate.

Solvers run inside filesystem, PID and network namespaces. Only their selected
source tree, selected read-only dependency bundle, toolchains and public Kitsu
instructions are mounted. Other tasks, reference patches, grader assets and
personal host files are absent. This matters because some later upstream
snapshots already contain a different selected task's repair.

A Go CONNECT bridge permits only exact model-service destinations; direct
external networking and GitHub fetches are unavailable. Codex's unrestricted
inner tool mode operates inside this external boundary. TLS remains end to
end. Dependencies were audited to remove newer copies of the target libraries
that could reveal fixes. Runtime directories are read-only and version/receipt
pinned; the manifest does not represent a complete per-file hash of every
runtime directory.

After solver termination, a symlink-safe raw copy reconstructs the candidate
in a fresh trusted Git database. Host capture does not execute the solver's
Git filters or diff drivers. Offline regressions verify this and preservation
of frozen files covered by ignore rules. The private grader requires a bound
trusted-candidate attestation before reading Git metadata, restores frozen test
and build controls, and executes in a separate namespace with no network.
Candidate tests cannot replace the independent oracle.

## Quality and accounting definitions

**Frozen-test functional success** requires eligible inputs, successful grader infrastructure,
passing independent acceptance, passing frozen regressions and valid patch
capture. Acceptance criteria retain individual results. A model's success
claim, its own passing tests or a local commit alone is insufficient. This is
a bounded oracle: subsequent blind review and confirmation probes can reveal
additional defects without rewriting the frozen score.

**Workflow compliance** is assessed separately: correct/fresh roles, a genuine
regression before production edits, role write checks, independent review,
authorized finding dispositions, current check evidence and the gated local
commit. Assignment text is archived before native spawning. When native logs
encrypt message contents, archived text cannot alone prove the exact payload
received; unresolved observations remain explicit limitations. Text mentioning
a compact command is not proof that the command executed.

**Blind code review** uses a fresh reviewer given the visible contract, public
baseline and substantive code/test diff under an opaque identifier. Condition,
tokens, author narratives, grading outcomes and workflow receipts are withheld.
Bundle preparation and protocol auditing are distinct from the blind reviewer.
Review findings have severity, a concrete source location and behavioral
evidence. Model review is additional evidence, not an infallible oracle.

Token totals come from every unique coordinator/descendant session's actual
Codex rollout usage. Causal parent links identify ownership; turn contexts
verify actual model and reasoning. Duplicate aliases and standard-output
summaries are not counted again. The report distinguishes:

- `input_total`: provider input, including cached input;
- `input_uncached`: input after subtracting cache reads and writes;
- `cache_read` and `cache_write`: input subsets;
- `output`: all generated output, including reasoning;
- `reasoning_output_tokens`: an output subset, not another addition;
- `total`: `input_total + output`.

The native four-role calibration demonstrates why coordinator-only accounting
is insufficient. The five live calibration groups used **1,686,680 tokens**
across 12 actual sessions, including the failed missing-companion probe. They
are excluded from solver identities and reported separately. A shorter Sol/high
parent/child check validates the replacement model profile; it does not repeat
or recharge the earlier workflow calibration. Research,
preparation and implementation conversation usage is not a measured solver
result. Independent blind-review usage is also a separate category.

These are actual token measurements, not a monetary charge. Provider billing
or subscription allowance consumption is unavailable; no dollar saving is
inferred from token ratios.

## Results

All 24 assigned attempts have retained candidates, independent grading and
protocol audits. The solver batch ran from **20:13:58 to 20:46:54 UTC on
16 November 2026**, about 33 minutes of wall time. Independent reviews and
offline validation followed. There were no silent retries.

| Outcome | Compact off | Compact on |
| --- | ---: | ---: |
| Assigned and captured candidates | 12 | 12 |
| Frozen independent acceptance passes | 12 | 12 |
| Frozen regression passes | 12 | 12 |
| Grader infrastructure and capture integrity passes | 12 | 12 |
| Host processes that completed before cutoff | 8 | 0 |
| Host processes stopped at eight minutes | 4 | 12 |
| Observable required workflow mechanics complete | 9 | 1 |
| Full protocol verified / unknown / violated | 0 / 9 / 3 | 0 / 1 / 11 |
| Runs with complete token accounting | 8 | 0 |
| Candidates with a deterministically confirmed additional defect | 1 | 1 |

The last row is a lower bound on defects found, not a certificate that the other
22 candidates are defect-free. Two major findings from blind review affect
the same charset task; follow-up probes show that one applies to both patches.

### Workflow completion and fidelity

The observable mechanics check requires the requested actual model/effort,
four fresh worker stages, a genuine behavioral red before production edits,
all required role write checks, current final check evidence and an observed
hooked local commit with a clean final tree. It completed in **9/12 normal**
and **1/12 compact** attempts. A coordinator can time out after the Git worker
has already completed these mechanics, so this differs from host-process exit.
The eight-minute limit is part of this comparison; a patch that passes tests
but has not finished review or the gate is not a completed Kitsu task.

All **108 captured actual solver sessions** have the requested Sol/high profile.
Full protocol certification remains unknown even for the ten mechanically
complete runs: encrypted native assignment payloads cannot be independently
matched to the saved briefs, and review neutrality/disposition evidence has
limits. Three reported native/saved brief deviations are retained in the audit.
Some normal coordinators moved skill receipts into `.git`, another documented
departure from the requested receipt location. None of these observations is
silently promoted to full compliance.

The frozen substring-based compact-uptake marker is not used as an execution
count: it can match command names inside a saved assignment. The independent
audit instead inspects executed foreground commands and returned structured
evidence. Its positive uptake counts are conservative observations, not a
complete frequency estimate. All 24 actual frozen graders exited successfully;
the longest took 31.56 seconds. No grader rescore was necessary.

### Blind review found what the frozen tests missed

After all solver results were captured, **24 fresh, independent Sol/high
reviewers** received opaque, read-only bundles. Each had a 120-second limit,
no subagents, and no access to condition, token counts, author narratives,
workflow receipts or private grading results. **21 returned valid reviews**:
19 reported no findings and two each reported one major finding. Three timed
out and remain unknown; they were not retried. The review manifest hash is
`297ad4a04bd20c4df4e4c19f07928a9c2f8135cfb3e01a9015a7c24d3cd382a8`.

A separate agent confirmed the concrete inputs against the pristine baseline
and both opaque charset candidates using isolated, offline Go probes. It did
not receive the private arm mapping. Arms were revealed only after all reviews
and confirmation probes had finished. The [review and adjudication export](codex-compact-pilot-data/quality.json)
retains IDs, hashes, outcomes and the mapping; the probes are retained alongside it.

| Additional defect in `chi-quoted-charset` | Baseline | Compact off | Compact on |
| --- | --- | --- | --- |
| A stray quote in an unquoted unrelated parameter hides a later explicit charset | Preserved legacy decisions | Confirmed regression | Preserved decisions on these inputs |
| Extended MIME parameters override the explicit ordinary `charset` parameter | Explicit ordinary charset governs | Confirmed regression | Confirmed regression |

Concrete examples use a nonempty POST body and a success handler returning
204. With only UTF-8 allowed,
`text/plain; charset=latin-1; charset*=UTF-8''utf-8` returns **415 on baseline
and 204 on both candidates**. The continuation form
`text/plain; charset=latin-1; charset*0=utf-; charset*1=8` does the same.
The converse `text/plain; charset=utf-8; charset*=UTF-8''latin-1` changes
**204 to 415** in both. The visible contract explicitly preserves disallowed
charset and legacy behavior; it does not authorize these overrides.

For the other defect,
`text/plain; note=foo"bar; charset=utf-8` changes **204 to 415** only in the
normal candidate. With UTF-8 and an absent charset permitted,
`text/plain; note=foo"bar; charset=latin-1` changes **415 to 204** only there.
The compact candidate preserves baseline decisions for these two inputs.

These checks were devised after review. They are labeled **posthoc evidence**,
not added retrospectively to the frozen pass rate. They demonstrate why
**24/24 test passes cannot establish quality preservation**. There is no
confirmed compact-only additional defect in this sample, but both arms have
a demonstrated defect and three reviews are unresolved. Counting "no finding"
as proof of correctness would be unjustified.

### Actual captured tokens

| Captured category | Compact off | Compact on |
| --- | ---: | ---: |
| Input including cache reads | 30,524,482 | 30,713,341 |
| Uncached input | 2,229,570 | 1,855,997 |
| Cache reads, included in input | 28,294,912 | 28,857,344 |
| Cache writes observed | 0 | 0 |
| Output including reasoning | 210,023 | 233,725 |
| **Total observed input + output** | **30,734,505** | **30,947,066** |
| Reasoning subset of output | 37,124 known subtotal; one run unknown | 35,100 |

The solver total is **at least 61,681,571 tokens**. These are actual captured
counters, not an estimate from characters or a provider bill. Most observed
input was cached. Reasoning is already included in output and is not added
again. A captured cache-write zero means no writes were reported in those
events, not proof of complete accounting for an interrupted session.

Every compact attempt was forcibly stopped, so its final in-flight request can
have unrecorded usage. Four normal attempts have the same problem. The
analysis therefore marks **zero complete pairs** and suppresses paired token
ratios and their confidence intervals. Comparing the two lower-bound totals,
or dividing them by test passes, would not establish an actual saving or
increase in complete task cost. Tokens per fully verified success are
undefined because no attempt has full protocol certification.

| Task | Normal captured tokens | Compact captured tokens | Normal / compact host status |
| --- | ---: | ---: | --- |
| pflag unknown flags | 2,007,427 | 2,186,426 | completed / cutoff |
| chi quoted charset | 2,087,482 | 2,540,010 | cutoff / cutoff |
| semver prerelease groups | 2,559,595 | 2,753,965 | completed / cutoff |
| chi self mount | 2,398,168 | 2,369,072 | cutoff / cutoff |
| qs sparse array limit | 2,975,314 | 3,020,885 | completed / cutoff |
| qs empty array keys | 2,705,971 | 2,443,733 | completed / cutoff |
| minimatch split budget | 2,748,778 | 3,581,746 | completed / cutoff |
| minimatch escaped patterns | 3,049,160 | 2,705,521 | cutoff / cutoff |
| fmt long double precision | 2,344,988 | 1,863,857 | completed / cutoff |
| fmt null printf precision | 2,363,704 | 2,030,747 | completed / cutoff |
| Kitsu path completion | 2,111,632 | 2,860,854 | completed / cutoff |
| Kitsu Bash login | 3,382,286 | 2,590,250 | cutoff / cutoff |

All rows pass the frozen tests in both arms. Every cutoff count is a lower
bound; completed normal rows have complete accounting. The machine-readable
[runs](codex-compact-pilot-data/runs.csv), [pairs](codex-compact-pilot-data/pairs.csv),
[threads](codex-compact-pilot-data/threads.csv) and
[analysis](codex-compact-pilot-data/analysis.json) preserve individual outcomes
and unknown reasons.

### Where observed usage went

| Role | Normal observed tokens | Compact observed tokens |
| --- | ---: | ---: |
| Coordinator | 20,145,443 | 19,250,246 |
| Quality engineer | 3,764,780 | 5,707,543 |
| Developer | 3,302,293 | 4,145,804 |
| Reviewer | 2,557,851 | 1,626,352 |
| Git worker | 964,138 | 217,121 |

The coordinator accounts for about **64% of all captured solver tokens**.
Compact test/development stages recorded more tokens; later review/Git stages
recorded fewer while frequently remaining unfinished. That downstream
difference is not evidence of efficiency. No lookup or architecture worker
was assigned in this small-task topology. The study holds all roles to one
model/effort to isolate the interface; it does not evaluate Kitsu's recommended
mixed-model role configuration.

### Other study usage, kept separate

| Phase | Captured tokens | Accounting |
| --- | ---: | --- |
| Replacement Sol/high solver batch, 24 attempts | 61,681,571 | Lower bound; 16 cutoffs |
| Interrupted Sol/max solver phase, 13 attempts | 41,398,261 | Lower bound; cutoffs/cancellations |
| Earlier live foundation calibration, 12 sessions | 1,686,680 | Separate; includes a failed probe |
| Replacement Sol/high parent/child calibration, two sessions | 44,993 | Complete |
| Independent blind reviewers, 24 attempts | 5,281,751 | Lower bound; three cutoffs |
| **Recorded subtotal across these categories** | **110,093,256** | **Lower bound, not a bill** |

Implementation and research conversation usage is outside this subtotal and
was not measured as part of solver execution. Independent tests, protocol
audits, cancellation recovery and confirmation probes are offline processes;
they make no model calls. The reviewer category has 924,851 uncached input,
4,314,624 cache reads and 42,276 output tokens, including 16,510 reasoning.

### Recommendation and next experiments

Keep compact mode **opt-in**. This pilot exposes a completion problem for the
tested uniform Sol/high, four-worker workflow under an eight-minute cap, and
does not establish a task-wide token saving or general quality equivalence.
It also shows that frozen tests need adversarial review and independent probes.

Priorities for a subsequent, separately authorized experiment are:

1. Move deterministic coordinator bookkeeping into Go. Prepare role settings,
   skill references, check identities and receipts once; test whether avoiding
   repeated discovery and retyped briefs offsets the added interface overhead.
2. Apply compact collection selectively to noisy searches and check logs. Do
   not assume short source files or complete worker instructions benefit from
   another metadata/paging layer. Preserve original evidence and raw fallback.
3. Measure the existing mixed-model role profile separately. Choosing a smaller
   model for lookup or Git is a hypothesis until completed tasks and quality
   are compared; this pilot deliberately held those choices fixed.
4. Record a graceful turn-boundary stop and reconcile all descendant usage
   before closing a run. Retain a hard ceiling as a safety limit, but do not
   treat an interrupted final counter as complete task accounting.
5. Add both confirmed charset failure families to the next frozen oracle,
   validate baseline/reference eligibility again, and include unfamiliar/private
   feature work. Reuse this pilot as diagnostic evidence, not a corrected trial.

These are suggestions, not measured benefits or changes to production policy.

## Superseded Sol/max phase

The original 72-run plan was stopped at the user's request for a smaller budget,
without pooling its outcomes or token consumption. It started 13 attempts: two host processes completed, five
reached the 20-minute limit, and six were cancelled by the operator. The 59
unstarted identities remain unstarted, rather than being assigned failures.
Cancelled candidates were captured and graded without further model calls.

Twelve of the 13 candidate patches pass independent acceptance; all 13 pass
the frozen original regression suites. These are code results, not evidence
that the required workflow finished. The captured usage is **at least
41,398,261 tokens**; only one attempt has fully complete accounting. Most
captured input was cache reads. Cutoffs can hide final in-flight usage, so the
phase does not establish total actual spend or a compact-mode saving.

The [original catalog](codex-compact-pilot-cases.json), frozen original plan,
traces, cancellation records and outcomes remain available. The new phase
starts from fresh source and homes, with Sol/high, one pair per task and an
eight-minute limit. No old attempt is silently retried or merged into it.

## What this pilot can establish

The completed study exposes concrete failure mechanisms and records bounded
quality evidence and actual captured consumption. Incomplete pairs prevent
a complete-task token comparison.
It cannot prove that compaction never loses quality. Twelve repeated tasks
remain twelve task clusters across only seven repositories. Public historical
source may occur in training data; two Kitsu repairs add self-evaluation bias.
Long feature development, new interfaces, dependent follow-ups, other models,
Claude Code and native macOS/Windows execution are outside this pilot.

The analysis resamples twelve task clusters and separately seven repository
clusters. On the frozen binary test outcome, every pair is a tie, so the
empirical quality-delta bootstrap is degenerate at zero; this says nothing
about defects the oracle misses. The zero frozen compact-only losses among
twelve tasks yield an illustrative one-sided 95% exact upper bound of **22.1%**
under an independent-binomial assumption. These convenience-selected tasks
do not establish that assumption for general developer work. Blind-review
unknowns and confirmed additional defects preclude treating that bound as
a general code-quality guarantee. Paired token intervals remain unavailable.

Provider cache warmth is observed, not fully controlled. Randomized condition
order reduces systematic ordering effects but cannot remove shared-service
cache variation. Six concurrent controllers share hardware; elapsed times are
exploratory and must not be promoted as clean speed measurements. Failed and
truncated trajectories cannot be presented as token-saving successes.

## Reproduction and retained evidence

### Harness and repository verification

The final implementation passed `go build ./...`, `go vet ./...`, the complete
Linux `go test -race -count=1 ./...` suite, and `golangci-lint run ./...` plus
`govulncheck ./...` for Linux, Darwin and Windows. Repocheck and `kitsu change
check` also passed. Native Darwin/Windows runtime execution was unavailable;
cross-target static scans are not native test runs. Ordinary harness tests use
fake local processes and make no paid model calls.

Retained validation receipt directories under the replacement state are:

| Evidence | Receipt directory within `validation/` |
| --- | --- |
| Build and vet | `20261116T205431Z-core-2632172735` |
| Three-target lint and vulnerability scans | `20261116T205630Z-core-2814207726` |
| Full final Linux race suite | `20261116T210013Z-core-602627045` |
| Repocheck, separate validation executable and record check | `20261116T210045Z-docs-1946280261` |

Earlier validation failures remain retained. They exposed a flaky compact-log
fixture that assumed ordering between stdout and stderr, and `/tmp` quota
exhaustion. The fixture now emits its routine output and final diagnostic on
one stream; its failing-preview assertions passed 30 race repetitions. Full
validation subsequently passed with disk-backed temporary storage. A separate
posthoc archive-padding deadlock fix has bounded-drain regression coverage.
These repairs did not replace frozen solver binaries, inputs or grades.

### Reproducing and inspecting the study

The [harness guide](../codex-compact-pilot.md) documents explicit invocation.
Ordinary tests make no paid calls. The private output retains the frozen
manifest/plan, frozen harness source, source patch, public source snapshots,
non-credential assets with hashes, raw parent/worker traces, candidate patches,
independent test logs, protocol audits and summaries. Authentication is excluded
from the non-credential asset archive and public exports. After all model work,
66 temporary experiment authorization copies were removed; the user's original
authorization was preserved. Frozen manifests and solver executable hashes
were rechecked unchanged. The earlier phase's protocol audits were copied from
temporary storage into its durable result directory.

The result location for the replacement execution is
`~/.local/state/kitsu/pilots/codex-compact-high-20261116`. The original partial
phase remains at `~/.local/state/kitsu/pilots/codex-compact-20261116`. Private machine paths
and authentication inputs are not portable configuration; a reproduction needs
local paths and fresh authorization. Public catalog revisions and hashes
identify task inputs; sanitized result exports support independent analysis.
