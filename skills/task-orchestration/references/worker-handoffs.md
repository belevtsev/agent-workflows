# Worker handoffs and independent review

You pick each worker, dispatch it as [hosts](hosts.md) says and verify its
identity and work. `cw task` only validates records: it never launches or stops
an agent, authenticates an identity, isolates permissions or authorizes an
external action. Keep briefs, results and records outside source.

## Brief and dispatch

Split independent questions or owned components, usually one or two workers;
keep dependent steps sequential. A brief is bounded and self-contained, never
the conversation: source revision, inputs, question or owned files, decisions,
constraints, permitted actions, evidence and stop condition. Editing workers
preserve others' changes; you own shared interfaces, concurrency and dependent
integration. Workers return evidence and questions, never adviser consults.

## Assign before dispatch

Set the plan's `coordinator_id` when it is not `/root`. Write the brief in the
shape `cw task schema assign` prints, with a new, unique `id`, run
`cw task assign --task <task-id> --input <brief>` and dispatch the worker in
the same turn, `assign` first; the worker needs only the assignment ID. Under
a profile a brief names its `stage`, and a later author brief what it `fixes`
or the assignment it `repeats`. Each stage starts a fresh worker from
`cw task brief --task <task-id> --stage <stage>`, never a continued worker or
the pasted record. Use exact on-disk paths without symlinks, and exact files
where a directory holds caches.

## Return and accept evidence

A result takes the shape `cw task schema handoff` prints. Give a hash the
worker computed explicitly, so a later edit shows.

The gate checks structure, bindings and declared identities; you check meaning,
scope, source, results and the reviewer's reasoning, then accept the result as
`accepted`, `rejected` or `unused`, with a disposition for every finding:
`resolved` when addressed, `rejected` against evidence, or `deferred` only when
it changes no behaviour, fails no gate and loses no rule. Never defer a defect
in changed behaviour or a finding with a concrete failure scenario, unless the
defect predates the change and the change neither causes, touches nor newly
exposes it. An `accepted` disposition leaves the finding open. Reviewer
agreement is no veto or permission. While the source stays as reviewed, record
both in one shell call: `cw task handoff --task <task-id> --input <result> &&
cw task accept --task <task-id> --input <acceptance>`.

Close stopped or superseded work as `unused` with a reason; that satisfies no
requirement and is no review. The worker stops changing source first.

### Handoff caps

Each contract states the cap on its result, and `cw task handoff` refuses a
larger one. Read the worker's reasoning in the report file the result's
`report` names; never ask for it inline.

## Test rules and test authorship

Test rules are the record's `test_rules`: in a `substantial` task,
`cw task verify` judges each changed Go test file (`_test.go`, or under
`testdata`) from the record, and `cw task authorship --task <task-id>` prints
each status with its reason. A file is `independent` when an unrejected
test-engineer round (a brief with a `contract`, an agent with no other role)
recorded its current bytes; `source_aware` when it is a new file that a
`resolved` finding disposition cites at its current bytes in an acceptance
listing a check, which makes it a reproduction test, never requirement
coverage; otherwise `lost` or `unowned`, which fails `verify`. A pre-task test
file that anyone edits needs a test-engineer round recording
its new bytes. When `assign` refuses a contract because no plan check earns
per-test credit, stop and ask the user.

## Review stages

Each [flow](contracts/README.md#flow) stage producing an artefact gets one
independent reviewer. The spec, design and test reviews record as `evidence`
assignments and the code review as `review`. Name the contract in the brief
(its Independence section binds the reviewer) and any skill's `SKILL.md` path.

**Severity.** Every finding carries `blocker`, `major` or `minor`, as its
contract defines them.

**Findings loop.** Return accepted findings to the stage's author
([code-review routing](dispatching-reviews-and-tests.md#route-code-review-findings));
the next round reviews the revision. Before rejecting a `blocker` or `major`, consult the
available advisers ([points](adviser-providers.md#adviser-points-in-the-flow)).

**Growth check.** The spec and design reviewers report as `scope creep` what a
revision or [amendment](contract-amendment.md) adds unasked. A finding the
user or sources require is `rejected` with that evidence.

### Review cap

Per stage, and again for each stage an amendment reaches:

1. One full review.
2. After fixes, one follow-up by a fresh reviewer, limited to the diff, but in
   full for any component the earlier review missed or a new approach touched.
3. One more review only for a fix to a defect the follow-up found, limited to
   that fix.

The cap never excuses a defect: fix what the last allowed review finds. A fix
to code or a gate gets one review limited to it. Check a prose-only fix
yourself (diff, cited checks, failure scenario); a fresh reviewer's
confirmation pass, outside the cap, then reports only a fix that fails its
finding, a code or gate defect, a contradiction, a lost rule or scope creep.
Report any extra review. Before `cw task verify`, close each superseded author
and reviewer assignment `unused`, with dispositions.

### Round budget

1. `cw task assign` refuses a round past the profile's cap or a third fix
   round on an open finding.
2. Each brief with `fixes` is a round; `repeats` only replaces an exposed
   worker or a round `verify` requires.
3. At the cap, open `blocker` and `major` findings stay `accepted`; nothing is
   published until each is resolved or rejected.
4. Ask the owner with each finding, its evidence and the round it needs; pass
   only their words to `--owner-approval`; never edit the artefact yourself.

**Cost.** Where no hook samples usage, run `cw task usage` before each `assign`
and `run`; at the budget stop, ask the user its question and pass only their
words.

## Review substantial changes

A substantial task whose source changed needs an accepted code review of the
whole current source by neither the coordinator nor an implementer, before
final adviser context or completion. Its brief owns `.` and names the paths of
`interface-contract.md`, `design.md`, `test-matrix.md`, `requirement-map.md`
and the final coverage profile with its run ID, when they exist. Adviser
checkpoints never replace it. [Apply behaviour-finding tests](dispatching-reviews-and-tests.md#apply-a-reproduction-test) unchanged.

Right before each review, write the bundle outside source with
`cw task bundle`, fill each reach row's `callers`, `invocation_modes`,
`platforms` and `consumers` from source (read the file too), and give its path
and SHA-256 as the brief's `bundle`; the brief adds only the effort bound and
what the bundle cannot hold. It orients, never narrows. Record implementation
handoffs, acceptances and checks first; record the review after the last check
run. After a source change, review again within the cap and rerun every cited
check.

### Run every reach-row mode

The reviewer runs each changed file in every mode and platform its row names.
Before accepting, count a `ran` entry only when it runs the changed file itself
in that mode, and a `not_run` entry only when it says why this host cannot run
that mode: "looks safe" or "out of scope" is no reason. When a mode has
neither: record the review `rejected` naming the missing modes; build a new
bundle; assign the same reviewer, continued, a new ID on unchanged source;
close the rejected review `unused`; record the continuation, which is the same
review, outside the cap.

### Follow-up review after a fix revision

Under Claude Code, each review of a stage after its first uses the
`review_followup` role. Spec, design and test follow-ups take no bundle. A code
follow-up asks whether each finding is resolved, whether the changed hunks hold
for every caller, consumer and mode in the rows, and what was not re-reviewed;
it may report beyond the diff. Continue a reviewer only to re-bind unchanged
source or run missed modes; neither counts against the cap. Order:

1. Record the earlier review's handoff before changing source.
2. Apply the reproduction tests, then fix the source, assigning any fix worker
   first; run every cited check once.
3. Record fix handoffs and acceptances; close `unused` each earlier
   implementation assignment whose paths changed and the earlier review, with
   dispositions citing the fixed source.
4. Build the follow-up's bundle, assign it, record it last.
