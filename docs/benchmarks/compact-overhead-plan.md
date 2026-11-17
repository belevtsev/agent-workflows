# Compact overhead: investigation and fix plan

17 November 2026. Three Sol/high subagents independently investigated the
coordinator, QE and native accounting in the completed
[paid pair](workflow-insights-paid-report.md). This is an offline investigation.
**All four fixes are implemented and pass offline regression checks.**
See the [handoff how-to](../compact-response-cycles.md) and
[OpenSpec verification](../../openspec/changes/reduce-compact-response-cycles/tests.md).
Implementation validation was offline. A subsequent owner-requested
[paid pair](response-cycles-paid-report.md) records the new descriptive results.
Compact remains opt-in; the frozen experiment and its reports remain unchanged.

## Why the captured total increased

Compact recorded 71 model-response usage entries versus 56 ordinary, a 26.8%
increase. Repeated accumulated input accompanies those responses. The 127
nonempty response IDs are unique across all ten sessions; per-response usage,
cumulative counters, per-session totals and the shared ledger reconcile exactly.
Each session contains one task-turn ID: response entries are not task turns,
individual tool calls or a measurement of hidden wire retries.

| Role | Ordinary tokens | Compact tokens | Difference | Recorded responses, off → on |
| --- | ---: | ---: | ---: | ---: |
| Coordinator | 929,598 | 1,183,575 | +253,977 | 27 → 33 |
| Quality engineer | 176,601 | 329,825 | +153,224 | 7 → 15 |
| Developer | 152,777 | 190,065 | +37,288 | 6 → 10 |
| Reviewer | 242,914 | 167,330 | −75,584 | 8 → 6 |
| Git | 136,329 | 113,545 | −22,784 | 8 → 7 |
| Total | 1,638,219 | 1,984,340 | +346,121 | 56 → 71 |

Coordinator and QE contribute +407,201, offset by −61,080 from the other roles
together. Mean input per response grows modestly for the coordinator
(34,047 → 35,663), but shrinks for QE (24,681 → 21,719) and the developer
(25,183 → 18,836). Prioritize the additional response sequences before
shortening necessary source or test evidence.

The arithmetic difference is +348,928 cached-input tokens, +284 uncached-input
tokens and −3,091 output tokens. Cache reads remain within input, reasoning
within output. Cached input is not new unique instruction content or a bill.
The aggregate uncached-input difference also masks role shifts: QE +32,949,
developer −31,984, coordinator +8,669, reviewer −8,747 and Git −603.

These observations locate the growth; they do not identify an isolated causal
effect, a removable token amount or a future saving. The arms differ in model
choices of operations, test organization and provider caching. One stochastic
pair cannot establish general model-quality equivalence or cost outcomes.

## Coordinator evidence

- Recorded exec cells grow 13 → 18 and native waits 6 → 10. A longer wait alone
  cannot stop early wakeups from worker messages. Distinguish final completion,
  actionable questions/blockers and nonterminal progress.
- The phase from developer completion coordination through reviewer launch
  grows from three response entries / 118,400 tokens to seven / 287,891:
  +169,491, or 66.7% of coordinator growth. Compact native trace C143–190
  shows repeated waits and nonterminal returns, diff/gate inspection, another
  wait, terminal return, separate finish, preparation and reviewer launch.
  Ordinary N130–151 combines boundary inspection with the review handoff.
  This inclusive grouping is observed activity, not a forecast of removable cost.
- Compact reads 516 unique parser lines and previews 110 lines of the QE packet.
  The preview ends before the original assignment. Workers already load the
  complete packet; the coordinator still needs the actual failing assertions
  and relevant test/source evidence for its semantic decisions.
- Four managed preparations each return approximately 2.7 KB of JSON, including
  identities, state and the native prompt. Some of this is transport overhead;
  identity, freshness and required warning information must stay available.

The current and frozen relevant instructions are byte-identical. They already
recommend selective observations, no packet reconstruction, bounded waits and
no repeated policy resolution. Repeating that advice alone is not a verified fix.

## QE evidence

Compact QE performs 15 response entries versus seven, with a smaller mean input
per entry. Its full initial packet output is also shorter than ordinary's
initial coordinator-skill/inventory output. Required role duplication is not
the demonstrated cause here.

The compact sequence adds a failed `rg` lookup followed by a working fallback,
two CLI help requests, separate formatting/check steps, an empty poll and
retained-log expansion. The unavailable optional tool is an environment fact;
the fallback was appropriate. Its availability was not carried into the packet.

Both QEs run one declared suite and add meaningful tests. Ordinary adds 36
subtests in a new 146-line file; compact adds 32 subtests in 128 appended lines.
Their edge-case mix differs and both pass protected acceptance. The compact
increase cannot be explained as broader test coverage. Preserve both the
requested behavior and its boundary/control cases.

The clearest deterministic defect is the fixed four-chunk log preview:

| Captured CLI evidence | UTF-8 bytes |
| --- | ---: |
| Original request budget, default | 16,384 |
| Raw complete rendered log, five chunks | 4,861 |
| Complete serialized expansion, required metadata included | 6,556 |
| Complete result with explicit `preview:false` | 6,572 |
| Actual four-chunk preview, one omitted chunk | 5,616 |

The complete observed result fits the original request budget by 9,828 bytes.
The preview still omits a chunk and returns tail/first fragments; QE correctly
expands it after another help request. The complete result's additional 940
bytes would preserve the original evidence in one CLI response. These are byte
measurements, not provider-token costs or a promised number of avoided responses.

Native QE references: C58–62 check help, C64 check start, C70 empty poll,
C77–79 preview, C83–86 expansion help and C88–91 full original-log expansion.
The C64 request has no `--max-bytes`; C88 requests 18,000 for expansion. The
actual C91 serialized result already fits 16,384. Host tool-wrapper bytes are
separate from the CLI response budget.

## Explanations ruled out or left unknown

- Both arms use one coordinator and four Sol/high workers, without rework
  workers, adviser calls or replacement attempts.
- Both final trees contain five empty skill-selection receipts. Their count
  cannot explain the difference. Current-content checks after receipt changes
  remain necessary under the existing evidence identity.
- Optional consequential/decision/troubleshooting notes and `usage budget`
  metadata were not selected in this task.
- No structured `task return` ingestion occurs in this pair; managed finishes
  supply the interval validations. Do not invent an ingestion-repair cost.
- Both frozen quality checks pass. Saved packet fidelity does not expose
  encrypted native assignment equality or fully attest reviewer-input neutrality.

## Accepted implementation plan

The implementation follows these four items. OpenSpec, regression tests,
README, how-to guidance and generated CLI help describe the resulting behavior;
the original investigation and paid measurements above remain unchanged.

### 1. Return complete failed evidence when it fits

Change the fixed preview decisions in `internal/cli/compact_checks.go:147` and
`internal/contextpack/page.go:137`. Read a bounded candidate from the retained
original log and test the complete final serialization against the requested
budget, including escaping and all required metadata. Do not load or retain an
unbounded log to make this decision.

When it fits, return chronological complete evidence, correct total/omission
counts and no continuation requirement. Otherwise retain diagnostic preview,
explicit omissions and the existing original-log cursor/fallback. Ordinary
output for quiet checks remains valid. Preserve exit status, check identity,
freshness, cancellation/timeout, expiry and missing-evidence semantics.

Acceptance: fitting one/four/five/six-chunk failures arrive complete; long logs
still need expansion. Test exact serialized budget N and N−1, multiple checks
sharing one response budget, UTF-8/invalid encoding and required metadata.
A 10,000-byte all-quote synthetic log in the observed envelope serializes to
21,391 bytes and must fall back under 16,384 despite its smaller raw text.
One check executes once; expansion executes no check. A receipt alone never
establishes meaningful red.

### 2. Give QE executable command recipes

Use existing declared-check and CLI metadata to include exact check/expansion
recipes and a bounded set of observed optional-tool capabilities in the saved
worker packet. Relevant entry points are `worker_dispatch.go`, `compact_brief.go`,
`worker_protocol_dispatch.go`, QE role guidance and the compact reference.
Preserve original role, assignment, restrictions and selected-skill bytes.
Do not paste the coordinator guide or a universal tool inventory into every role.

Scope observed capabilities to the actual host/session/tool environment.
Unknown stays unknown; a relevant environment change or execution error requires
revalidation and an available fallback. Do not install missing tools or interpret
a failed lookup as an empty search result. Keep all commands within permissions.

Allow dependent format/check steps to share a host exec call, short-circuiting
on failure. Use bounded execution waits with cancellation and deadline handling.
Complete check evidence needs no expansion. Required progress/questions remain
available; completion should use the native final return without a duplicate
worker-to-coordinator completion message when the final return suffices.

Acceptance: generated recipes parse against current CLI metadata; a known
missing `rg` uses a working fallback without repeating a failed discovery.
Unknown availability retains one permitted detection path. Scoped source
inspection, original skills and all requested edge/control cases remain.

### 3. Make coordinator sequencing explicit and economical

Add executable host recipes and replay expectations to existing cadence guidance
in `internal/roles/text/coordinator.md` and `skills/task/references/compact.md`.
Do not inspect a worker packet preview when only its dispatch identity is needed.
Use targeted failure/test/source reads for actual coordinator decisions.

Preserve user-facing progress updates, real worker questions, discovered risks,
blockers and terminal results. Coalesce nonactionable worker-to-coordinator
progress; do not silence messages that require a decision. Bounded waits and
independent budget/deadline supervision remain.

After the worker is terminal and required semantic decisions have been made,
sequentially batch existing `task finish` then `task prepare` in one host call.
The next operation runs only if the previous operation succeeds. The original
red must have been inspected before developer preparation; review findings and
dispositions must be handled before the next role. A successful receipt is not
semantic approval. If preparation fails after a successful finish, retain that
finished interval and report the partial transition honestly.

No new all-in-one stage-advancement command, model spawner, wait daemon or host
rollout watcher is needed. This is a protocol/example change; offline replay
does not establish how many responses a future model will choose.

### 4. Add an opt-in lean native launch view

After the response-sequence changes, add a launch-oriented presentation in Go
at `worker_protocol_dispatch.go`, `worker_stage.go` and `worker_commands.go`.
Preserve existing default versioned JSON contracts and provide an accessible
complete immutable response reference. The exact view/flag name is an
implementation choice, not a new compact mode.

The launch view must retain the exact native prompt and prompt hash, model and
reasoning, fresh-context settings, task/dispatch/packet identities and needed
warnings or explicit warning references. Claude's native role must remain.
Complete original worker packets and selected skills stay unchanged. Full state,
policy and evidence metadata remain available at their original authority.
Do not require another model response to recover a field every launch needs.

Acceptance: both-host roundtrips preserve exact native prompt/role/assignment/
skill loading contracts; full and lean views have identical identities and
authority. Undersized budgets, stale policy, tampering and changed skills fail
closed before partial selection/dispatch writes. Repeated full-reference reads
create no new execution, baseline or evidence.

## Offline gate before any future paid decision

Reuse the existing lifecycle harness; do not create a second orchestration
framework or count an always-green delivery microreplay as an end-to-end repair.

- Extend `TestCompactChecksPreserveOutcomesLogsAndEvidence` and
  `TestCheckPreviewsRetainTailAndExpandFromOriginalLog` for the fitting-log
  boundary while retaining huge-log tail recovery and failure/expiry controls.
- Extend `TestCompactDeliveryOfflineReplay` with the observed QE discovery/
  preview/expansion shape and coordinator notification/finish/prepare shape.
  Count every observation, failure, help call, expansion, return, retry and
  required original-skill read in the existing transport ledger.
- Pair each microreplay with `TestCoordinatorReplayLifecycle`, including actual
  red → green, a real major finding, repair, fresh independent review and
  the Git worker's normal hooked commit with a clean final tree.
- Reuse interrupted/failure endpoints: early QE success without meaningful red,
  stale green after a relevant edit, hidden unauthorized writes, removed
  role/assignment/skill facts, unresolved findings, unavailable host and a Git
  preparation with no actual dispatch/completion. The replay must not label
  those endpoints complete. Do not invent CLI certification of semantic TDD.
- Reuse worker return, packet hash and policy/source/skill freshness matrices.
  Returns stay optional untrusted claims. An expected-red check is evidence;
  it must not cause automatic dropping of a genuine finding or fake approval.
- Run relevant local Go/repository checks. Keep component/provider/model paths
  disabled. Report replay calls and serialized UTF-8 bytes as proxy measures,
  not actual provider tokens, prices or model-quality results.

Do not broadly exclude `.skills` from evidence or skip checks after a policy/
source change. Receipt-identity changes would need a separate reviewed design
and mutation controls. Do not shrink test coverage, remove required selected
skills, downgrade models or reasoning, merge author and reviewer context,
discard findings, bypass hooks or infer an under-budget pass from missing usage.

## Evidence and current validation

The [sanitized analysis](compact-overhead-analysis.json) binds the numbers to
the frozen manifest, production binary, public usage summary and every native
trace hash. Private subagent notes retain detailed line references and decoded
outputs; raw prompts and private host paths are not exported.

For coordinator references N/C denote the ordinary/compact coordinator native
rollouts identified by hashes in the analysis. For the QE sequence they denote
the ordinary/compact QE rollouts. Lines are one-based native JSONL positions,
not source lines or host stdout positions.

Investigation status: accounting and trace audit complete; all four fixes are
implemented. Required local integration checks pass and their results are
recorded in the [OpenSpec verification](../../openspec/changes/reduce-compact-response-cycles/tests.md).
Regressions cover fitting and oversized evidence, actual lookup-directory scope,
both native hosts, immutable original dispatches and the existing repair/review/
hooked-Git lifecycle with conditional handoff failures. Local calls and bytes
remain proxy measures; no provider saving or model-quality improvement has been
established generally for the new implementation. The subsequent
[paid pair](response-cycles-paid-report.md) is separate from this investigation;
it includes no new paid accounting probe or extra paid reviewer.
