# Worker handoffs and independent review

Contents: [brief](#brief-and-dispatch) · [assign](#assign-before-dispatch) ·
[accept](#return-and-accept-evidence) · [tests](#test-rules-and-test-authorship)
· [reviews](#review-stages) · [code review](#review-substantial-changes)

The coordinator picks each worker, resolves its model from policy,
[dispatches it](global-workflow.md#dispatch-workers) and verifies its identity
and work. `cw task` commands only validate records
([checks](../../../docs/operations.md#worker-records)): none launches or stops
agents, authenticates identities, isolates permissions or authorizes external
actions. Keep briefs, results and records outside source. Small and read-only
tasks need no review, but delegated work still needs acceptance or closure as
`unused`.

## Brief and dispatch

Split independent questions or owned components, usually one or two workers;
keep dependent steps sequential. A brief is bounded and self-contained, never
the conversation: source revision, inputs, question or owned files, decisions,
constraints, permitted actions, evidence and stop condition. Editing workers
preserve others' changes; the coordinator owns shared interfaces, concurrency
and dependent integration. Workers return evidence and questions, never adviser
consults. The test engineer gets only the contract and scaffold
([boundary](contracts/test-engineer.md#author-only-from-the-contract)); the
coordinator owns the execution gates. Under Claude Code the reviewer and test
engineer start without CLAUDE.md ([hosts](hosts.md#worker-agent-types)): quote
any project constraint their contract lacks word for word.

## Assign before dispatch

Set the plan's `coordinator_id` when it is not `/root`. Write the brief
([example and members](../../../docs/operations.md#assignment-briefs)) with a
new, unique `id`, run `cw task assign --task <task-id> --input <brief>` and
dispatch the worker in the same turn, `assign` first; the worker needs only the
assignment ID. Under a profile a brief names its `stage`, and a later author
brief what it `fixes` or the assignment it `repeats`. Each stage starts a fresh
worker from `cw task brief --task <task-id> --stage <stage>`, never a continued
worker or the pasted record. Use exact on-disk paths without symlinks, and
exact files where a directory holds caches.

## Return and accept evidence

A result takes the shape `cw task schema handoff` prints and matches the source
at submission: never invent a fingerprint or copy the baseline after edits.
Give a hash the worker computed explicitly, so a later edit shows; give `[]`
for an empty collection.

The gate checks structure, bindings and declared identities; the coordinator
checks meaning, scope, source, results and the reviewer's reasoning, then accepts
the result as `accepted`, `rejected` or `unused`, with a disposition for every
finding: `resolved` when addressed, `rejected` against evidence, or `deferred`
only when it changes no behaviour, fails no gate and loses no rule. Never defer
a defect in changed behaviour or a finding with a concrete failure scenario,
unless the defect predates the change and the change neither causes, touches
nor newly exposes it. An `accepted` disposition leaves the finding open.
Reviewer agreement is no veto or permission. While the source stays as
reviewed, record both in one shell call:

```sh
cw task handoff --task <task-id> --input /private/result.json &&
cw task accept --task <task-id> --input /private/acceptance.json
```

Close stopped or superseded work as `unused` with a reason; that satisfies no
requirement and is no review. The worker stops changing source first.

### Handoff caps

Each role contract states the result's size caps, and `cw task handoff`
refuses a result over them ([limits](../../../docs/operations.md#handoff-caps)):
the reasoning goes in the report file that `report` names.

## Test rules and test authorship

`cw task verify` judges test authorship from the record
([rules](../../../docs/operations.md#test-authorship)). When `assign` refuses a
contract because no plan check earns per-test credit, stop and ask the user.

## Review stages

Each [flow](contracts/README.md#flow) stage producing an artefact gets one
independent reviewer:

| Stage | Contract | Findings go back to | Record role |
| --- | --- | --- | --- |
| Spec review | [spec-review](contracts/spec-review.md) | spec writer | `evidence` |
| Design review | [design-review](contracts/design-review.md) | architect | `evidence` |
| Test review | [test-review](contracts/test-review.md) | test engineer | `evidence` |
| Code review | [code-review](contracts/code-review.md) | implementer; a test finding to a fresh test engineer | `review` |

Dispatch per [hosts](hosts.md#worker-agent-types); name the contract (its
Independence section binds the reviewer) and any skill's `SKILL.md` path.

**Severity**, one per finding; a contract may define a grade for its artefact,
never add one. `blocker`: relying on the artefact as written yields wrong,
unsafe or unverifiable behaviour. `major`: the next stage cannot work correctly
from it, such as an untestable or unguarded requirement or a defect in changed
behaviour. `minor`: clarity that changes no test and no behaviour.

**Findings loop.** Return accepted findings to the author; the next round
reviews the revision. Before rejecting a `blocker` or `major`, consult the
available advisers ([points](consultation-helper.md#adviser-points-in-the-flow)).

**Growth check.** When a spec or design is reviewed after a revision or an
[amendment](contracts/README.md#contract-amendment), anything the diff adds
(requirement, scenario, task, option, component, interface, dependency or
behaviour) that no accepted finding, user answer or forwarded question asked
for is a `scope creep` finding citing it, unless the smallest fix of an
accepted finding needs it, judged by that finding, not the revision's claim. One the next stage would build or test is at least
`major`; one the user or sources require is `rejected` with that evidence.

### Review cap

Per stage, and again for each stage an amendment reaches:

1. One full review.
2. After fixes, one follow-up by a fresh reviewer, limited to the diff, but in
   full for any component the earlier review missed or a new approach touched.
3. One more review only for a fix to a defect the follow-up found, limited to
   that fix.

The cap never excuses a defect: fix what the last review finds. Check a
prose-only fix yourself (diff, cited checks, failure scenario); a fresh
reviewer's confirmation pass, outside the cap, then reports only a fix that
fails its finding, a code or gate defect, a contradiction, a lost rule or scope
creep. Report any extra review. Before `cw task verify`, close each superseded
author and reviewer assignment `unused`, with dispositions.

### Round budget

1. `cw task assign` refuses a round past the [cap](../../../docs/operations.md#round-caps) or a third [fix round](../../../docs/operations.md#fix-rounds) on an open finding.
2. Each brief with `fixes` is a round; `repeats` only replaces an exposed worker or a round `verify` requires.
3. At the cap, open `blocker` and `major` findings stay `accepted`; nothing is published until each is resolved or rejected.
4. Ask the owner with each finding, its evidence and the round it needs; pass only their words to `--owner-approval`; never edit the artefact yourself.

**Cost.** Where no hook samples usage, run `cw task usage` before each `assign`
and `run` ([budget stop](../../../docs/operations.md#budget-stop)); at the stop,
ask the user its question and pass only their words.

## Review substantial changes

A substantial task whose source changed needs an accepted code review of the
whole current source by neither the coordinator nor an implementer, before
final adviser context or completion. Its brief owns `.` and names the paths of
`interface-contract.md`, `design.md`, `test-matrix.md`, `requirement-map.md`
and the final coverage profile with its run ID, when they exist. Adviser
checkpoints never replace it. Handle behaviour-finding tests as the
[contract](contracts/code-review.md#reproduce-every-behaviour-finding) says.

Right before each review, write the
[bundle](../../../docs/operations.md#review-bundle) outside source with
`cw task bundle`, fill each reach row's `callers`, `invocation_modes`,
`platforms` and `consumers` from source (read the file too), and
give its path and SHA-256 as the brief's `bundle`; the brief adds only the
effort bound and what the bundle cannot hold. It orients, never narrows.
Record implementation handoffs, acceptances and checks first; record the
review after the last check run. After a source change, review again within
the cap and rerun every cited check.

### Run every reach-row mode

The reviewer runs each changed file in every mode and platform its row names
([contract](contracts/code-review.md#required-outputs)). Before accepting,
count a `ran` entry only when it runs the changed file itself in that mode;
"looks safe" is no `not_run` reason. When a mode has neither: record the review
`rejected` naming the missing modes; build a new bundle; assign the same
reviewer, continued, a new ID on unchanged source; close the rejected review
`unused`; record the continuation, which is the same review, outside the cap.

### Follow-up review after a fix revision

Under Claude Code, each review of a stage after its first uses the
`review_followup` role. Spec, design and test follow-ups take no bundle. A code
follow-up asks whether each finding is resolved, whether the changed hunks hold
for every caller, consumer and mode in the rows, and what was not re-reviewed;
it may report beyond the diff. Continue a reviewer only to re-bind unchanged
source or run missed modes. Order:

1. Record the earlier review's handoff before changing source.
2. Apply the reproduction tests, then fix the source, assigning any fix worker
   first; run every cited check once.
3. Record fix handoffs and acceptances; close `unused` each earlier
   implementation assignment whose paths changed and the earlier review, with
   dispositions citing the fixed source.
4. Build the follow-up's bundle, assign it, record it last.
