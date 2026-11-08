# Worker handoffs and independent review

Use this contract for delegated repository work in any project; the project's
instructions choose tools and check commands. Keep briefs, results and records
outside source. The coordinator selects the runtime agent,
resolves its model from policy, [dispatches it](global-workflow.md#dispatch-workers)
and verifies actual identity and returned work. Native commands only validate
declarations and evidence: they never launch or stop agents, authenticate
identities, isolate permissions or authorize external actions.

## Brief and dispatch

Split useful independent questions or owned components. Give each worker a
self-contained brief (exact source revision and inputs, question or owned files,
relevant decisions, constraints, permitted actions, output evidence, stopping
condition) with only the context it needs and no duplicate investigation.
Resolve its role's profile from the [host mapping](hosts.md#profiles-and-policy);
under Claude Code, every later review of a stage, after that stage's first full
review, uses the
[`review_followup` role](#follow-up-review-after-a-fix-revision). Editing workers
share the codebase and preserve others' changes; coordinate shared interfaces,
respect concurrency limits and keep dependent integration with the coordinator.
Workers return exact sources, observations, changes, checks and uncertainty, and
return evidence and questions instead of consulting advisers; reconcile overlaps
and verify conclusions independently, since agreement and confidence do not
establish behavior. Preserve required design challenges and execution
environments. The test engineer receives only the agreed contract and approved
scaffold ([boundary](contracts/test-engineer.md#author-only-from-the-contract));
the coordinator or verifier inspects implementation and owns the execution gates.
Under Claude Code the reviewer and the test engineer start without CLAUDE.md
files ([host mapping](hosts.md#worker-agent-types)): their contracts carry the
project rules they apply, and their briefs quote, word for word, any other
project constraint the work must meet, such as a check command or a naming rule.

## Assign before dispatch

When the task's checkpoints are required, attach a resolved before checkpoint
first: `cw task assign` refuses such a task until then. Set the plan's
`coordinator_id` to the actual coordinating agent (default `/root`). Prepare a
brief with a unique immutable assignment ID:

```json
{"id": "component-change", "agent_id": "/root/implementer", "role": "implementation",
 "goal": "Implement the agreed component behavior", "owned_paths": ["src/component"],
 "constraints": ["Preserve unrelated changes and the agreed interface"],
 "check_ids": ["unit"],
 "stop_condition": "Return changes, source evidence, actual checks and unresolved questions"}
```

```sh
cw task assign --task /private/task --input /private/brief.json --summary
```

The record retains the brief's task ID, exact source snapshot and SHA-256;
`--summary` prints the assignment ID, its SHA-256 and the snapshot fingerprint.
Record the assignment and dispatch the worker in the same turn, `assign` first,
with its brief, relevant input and the result contract; it needs only the
assignment ID, as `handoff` fills the assignment hash and snapshot. Successive
assignments use new IDs, distinct from runtime identity. Pass a bounded brief, not
the full conversation.

Roles are `implementation`, `evidence`, `review` and `verification`; only
implementation results can declare changes. `owned_paths` holds clean relative
files or directory prefixes (`.` covers the whole project); active implementation
assignments cannot own overlapping paths, and shared-filesystem workers preserve
others' edits. `check_ids` selects commands already in the immutable task plan.
`cw task run` retains actual execution; a worker's assertion is not a pass. A
test engineer records as its [contract](contracts/test-engineer.md#record-and-complete)
says: native `role: "implementation"`, exact test-artifact paths and `check_ids: []`.

Use exact on-disk path spelling without symlink components. Ownership checks cover
direct file aliases, not hardlinks beneath separate directory scopes. Scoped
freshness hashes every entry under each owned directory, rejects symlinks and stops
at 30,000 entries or 64 MiB; prefer exact source files when a directory also holds
caches or linked resources. `.` uses the project's eligible-source snapshot.

## Return and accept evidence

The result JSON contains:

| Field | Meaning |
| --- | --- |
| `task_id`, `assignment_id`, `assignment_sha256`, `agent_id` | Exact task, brief and worker binding |
| `snapshot` | Full current `profile.snapshot` from `cw doctor --repo /absolute/project` |
| `status`, `summary` | `completed`, `blocked` or `cancelled`, with a concrete outcome |
| `evidence` | Source references with `path`, `start_line`, `end_line` and full-file `sha256` |
| `changed_files` | Declared changes with `path` and `sha256`, or `deleted: true`; empty for read-only roles |
| `check_ids` | Actual task check records cited by the worker |
| `findings` | Each finding's `id`, `summary`, `severity` (`blocker`, `major` or `minor`; required in a review) and source `evidence`; `[]` records none |
| `uncertainties` | Remaining unknowns; `[]` records none |
| `reach_runs` | Review only: how each reach-row mode was [run](#run-every-reach-row-mode) |

The result must match actual source at submission: never invent a fingerprint or
copy the original baseline after edits. `cw task` fills omitted single-valued
fields and validates explicit ones: `task_id`, `assignment_sha256`, `agent_id` and
`snapshot` (current source) in a result; `task_id`, `coordinator_id` and
`result_sha256` (the single recorded result) in an acceptance; `sha256` in any
source reference or changed file, from its current bytes. A filled hash binds the
bytes present when the record is written, so give it explicitly when the worker
computed it and a later edit must be detected. Collections (`evidence`, `changed_files`, `check_ids`,
`findings`, `uncertainties`) are never filled: give `[]` when empty.

The coordinator independently checks meaning, scope, source and results. Its
acceptance holds `assignment_id`, `status` (`accepted`, `rejected` or `unused`),
`reason`, real current source `evidence`, `check_ids` and, for every finding,
`finding_id`, a disposition `status`, `reason` and source `evidence`: `resolved`
when addressed, `rejected` against evidence, or `deferred` when it changes no
behaviour, fails no gate and loses no rule (`cw task verify` lists it as a
limitation; never defer a defect in changed behaviour or any finding with a
concrete failure scenario, unless the defect predates the change and the change neither causes,
touches nor newly exposes it). `verify` lists each rejected `blocker` or `major`
finding as a limitation; a rejected `blocker` also needs a later accepted
independent review whose bundle lists the rejection and whose result does not
re-raise its `id`. An `accepted` disposition
acknowledges an unresolved finding and blocks completion. Reviewer agreement is not
a veto or permission. When the source stays as reviewed, write the result and
acceptance JSON and record both in one shell call:

```json
{"assignment_id": "review-1", "status": "accepted",
 "reason": "Checked each finding against the current source",
 "evidence": [{"path": "src/component/store.go", "start_line": 40, "end_line": 58}],
 "check_ids": ["unit"],
 "findings": [{"finding_id": "F1", "status": "rejected",
   "reason": "Line 54 returns the last error after the final attempt",
   "evidence": [{"path": "src/component/store.go", "start_line": 51, "end_line": 55}]}]}
```

```sh
cw task handoff --task /private/task --input /private/result.json --summary &&
cw task accept --task /private/task --input /private/acceptance.json --summary
```

When a finding needs a fix, follow the [record order](#follow-up-review-after-a-fix-revision).
After the coordinator stops or supersedes blocked or cancelled work, it can close
it as `unused` with an explicit rationale; this neither satisfies requirements nor
counts as independent review. The worker must stop changing source before its
assignment is closed; these commands do not stop the runtime process.

## Review stages

Every stage of the [flow](contracts/README.md#flow) that produces an artefact
has an independent review, run by one reviewer agent with the contract the brief
names:

| Stage | Contract | Reviews | Findings go back to | Record role |
| --- | --- | --- | --- | --- |
| Spec review | [spec-review](contracts/spec-review.md) | the OpenSpec change folder | spec writer | `evidence` |
| Design review | [design-review](contracts/design-review.md) | `design.md`, `interface-contract.md`, the ADR when one is written | architect | `evidence` |
| Test review | [test-review](contracts/test-review.md) | test matrix, requirement map, tests | test engineer | `evidence` |
| Code review | [code-review](contracts/code-review.md) | the integrated source | implementer; a finding about a test goes to a fresh test engineer | `review` |

Under Claude Code, dispatch each with the `agent-workflows:reviewer` agent type
and the stage's role, model and effort ([host mapping](hosts.md#worker-agent-types)),
and name the contract in the brief; give the `SKILL.md` path of any skill the
review should apply, such as `code-review`. Each stage's independence rules are
in its contract: a reviewer is never the author it reviews, and the test
reviewer never sees implementation. Only the code review is the task record's
`review` assignment that `cw task verify` requires; the earlier stages record as
`evidence` assignments.

**Severity.** Every finding at every stage carries one grade; a contract may say
what a grade means for its artefact, never add another:

- `blocker`: building, merging or relying on the artefact as written yields
  wrong, unsafe or unverifiable behaviour;
- `major`: the next stage cannot work correctly from the artefact as stated,
  such as an untestable requirement, an unguarded requirement or a defect in
  changed behaviour;
- `minor`: clarity or quality that changes no test and no behaviour.

**Findings loop.** The coordinator records a disposition for every finding
(below) and returns the findings it accepts to that stage's author, whose
revision answers each one; the next round reviews the revision. Before rejecting
a `blocker` or `major` finding at any stage, the coordinator consults the
available advisers ([adviser points](consultation-helper.md#adviser-points-in-the-flow)).

**Growth check.** Every time a spec or design is reviewed again after a
revision (a diff-scoped follow-up, a review scoped to a fix, a confirmation
pass, and the scoped design review of a
[contract amendment](contracts/README.md#contract-amendment)), the reviewer
checks what the revision added as well as whether each finding is resolved.
Here an accepted finding also means a user's answer to an open question listed
with the dispositions, and a question forwarded in an amendment. Every
requirement, scenario, task, option, component, interface, dependency or
behaviour that the diff adds and that no accepted finding asked for is a
finding in the category `scope creep`, citing the added text and the findings
it was checked against. The one exception is what the smallest change that
resolves an accepted finding cannot do without, judged against that finding's
"what would resolve it" (for an answer or a forwarded question, the smallest
change that applies it), not against the revision's own claim that an addition
serves it. Its severity follows the shared grading: an unrequested
addition the next stage would build or test is at least `major`. The
coordinator returns it to the author like any other finding, and an addition
that the user or the sources do require is `rejected` with that evidence.

**Review cap.** The cap applies to each stage separately: one full review; after
the fixes, at most one diff-scoped follow-up by a fresh reviewer; and a further
review only to re-bind a fix of a defect that follow-up found, scoped to that
fix. The cap never excuses a defect: fix what the last allowed review finds. A
fix that changes executable code or a gate's behaviour gets one review scoped to
it. Verify a prose-only fix yourself (diff readback, the checks it cites, the
reviewer's failure scenario rechecked against the new text), then have a fresh
reviewer re-bind with a confirmation pass that reports only a fix that fails its
finding, a defect in code or a gate, a new contradiction or lost rule in the
instructions a coordinator follows, or, for a spec or design, a `scope creep`
finding from the growth check; that pass does not count against the cap.
Report any extra review in the result. Findings that change no behaviour, fail no
gate and lose no rule are `deferred` or `rejected` rather than reviewed again; a
finding with a concrete failure scenario is never deferred unless the defect
predates the change and the change neither causes, touches nor newly exposes it.
A [contract amendment](contracts/README.md#contract-amendment) opens a cap of
the same shape for each stage it reaches. When a revision changes the approach
or touches components the last review did not cover, the follow-up reviews those components in full. The coordinator
closes each superseded round's author and reviewer assignments as `unused`, with
dispositions, before `cw task verify`.

**Fast paths.** These follow from the rules above and change none of them:

- A first review with no `blocker` or `major` finding needs no follow-up
  unless a revision follows it (a fix, or answers to open questions), and that
  revision gets only the review the cap gives it.
- `minor` findings that change no behaviour, fail no gate and lose no rule are
  `deferred` or `rejected` without another review.
- Code review can overlap check runs while source is stable; record its
  handoff after the last check run and worker record, since a later one makes
  it stale (end of [run every reach-row mode](#run-every-reach-row-mode)).
- Under Claude Code, every later review of a stage runs on the
  [`review_followup` role](#follow-up-review-after-a-fix-revision).

## Review substantial changes

New substantial tasks whose source differs from baseline require an independent
substantive code reviewer, distinct from the coordinator and implementation
workers, before final adviser context (or completion when checkpoints are not
required). Its brief uses `role: "review"`, the [code-review](contracts/code-review.md)
contract and `owned_paths: ["."]` for the full resulting source; it returns the
same handoff shape with no changes. Adviser checkpoints do not replace review,
and missing independent review cannot be presented as completed review. Small
and unchanged read-only tasks stay lightweight.

The bundle orients and never narrows: a full review still reads any source and runs
any experiment it judges necessary, such as focused tests or platform and shell
probes. Build it right before each review assignment, outside the source, so the
reviewer starts there instead of exploring (measured reviews spent about 49% of their
requests on orientation reads):

```sh
cw task bundle --task /private/task --review-id review-1 --output /private/review-1-bundle.json
```

From the record and current source it writes the requirements, prior findings with
their dispositions, each check's latest run with its log path, the files changed
since the task started (a manifest of path, mode and SHA-256, bound by
`manifest_sha256`), and one reach row per file changed since the previous reviewed
source: the latest review with a completed handoff that was not rejected, or the
task start for the first review. A row's hints give the file's shebang and up to 10
lines of docs and scripts that name it, invocations first. Fill all four facets from
source, or write `n/a: <reason>`:

| Facet | Answer from source |
| --- | --- |
| `callers` | Who calls, sources or imports the file; its entry points |
| `invocation_modes` | Every documented way it runs: piped (`curl … \| sh`), sourced, executed, flags, CI |
| `platforms` | Every operating system, shell and version it must work on |
| `consumers` | What reads the state, files, output or formats it writes |

Hints are leads, not answers: read them and the file. In a measured replay all four
reviewers missed a crash of `curl … | sh` under macOS `/bin/sh`, which is bash 3.2,
because no brief asked how the changed installer runs. Give the filled file's path
and SHA-256 (`shasum -a 256`) as the brief's `bundle`:

```json
{"id": "review-1", "agent_id": "/root/reviewer", "role": "review",
 "goal": "Review the change against the requirements", "owned_paths": ["."],
 "constraints": [], "check_ids": [],
 "stop_condition": "Return findings with source evidence and what was not reviewed",
 "bundle": {"path": "/private/review-1-bundle.json", "sha256": "<sha256 of the filled file>"}}
```

`assign` regenerates the bundle and refuses a missing row, an empty facet or a bare
`n/a`, and any other difference, including a check run or record added after
`bundle`; it keeps an immutable copy for the next bundle. Tasks started by an older
cw take no bundle. The brief points at the bundle and adds only the effort bound and
what the bundle cannot hold.

### Run every reach-row mode

The reviewer runs each changed file, in a scratch copy, in every invocation mode and
on every platform and shell its reach row names: piped as `curl … | sh`, sourced,
with each documented flag, under macOS `/bin/sh` (bash 3.2), dash or zsh, and
observes the result. Reading the code, probing a different shell, or running an
extracted fragment, a stand-in or a test that starts the file another way does not
run it that way. Where a mode cannot run on this host, the reviewer says why for that
mode, for example "no Windows host"; "looks safe" or "out of scope" is not a reason.
The result's `reach_runs` holds one entry per mode:

```json
[{"path": "install.sh", "mode": "curl | sh under macOS /bin/sh (bash 3.2.57)", "status": "ran",
  "command": "cat install.sh | /bin/sh -s -- version", "observed": "exit 0, printed the version"},
 {"path": "install.sh", "mode": "Linux dash", "status": "not_run", "reason": "no Docker or Linux host"}]
```

`cw task handoff` refuses a completed review that carries a bundle unless each row
of a file that still exists has an entry and each entry names a row; a `ran` entry
needs `command` and `observed` and no `reason`, a `not_run` entry `reason` and
neither of the others. A review assigned with a bundle from an older cw (`version`
1), which never asked for runs, needs none. Whether the entries cover every mode is
the coordinator's check before `accepted`: compare each row's `invocation_modes` and
`platforms` with its entries, and count a `ran` entry only when its command runs the
changed file itself in that mode. When a named mode has neither, do not accept.
In this order: record the review `rejected` with the missing modes as the reason (and
its findings' dispositions), so it no longer counts as reviewed source; build a new
bundle, whose rows return; assign it to the same reviewer, continued, under a new
review ID on the unchanged source; close the rejected review `unused` (verify needs
it closed, and a close after the continuation's handoff would make that stale); then
record the continuation's handoff and acceptance. That completes the same review and
does not count against the cap. In measured replays, six reviewers given a correctly filled
`install.sh` row (`curl | sh`; macOS `/bin/sh` is bash 3.2.57) probed under dash or
reasoned about the mode and none caught the crash. With this rule one of three ran
the file piped into macOS `/bin/sh` and caught it; the other two piped a fragment or
a stand-in, and after this acceptance check both ran the file and caught it.

The [review cap](#review-stages) applies to code review as to every stage.

Complete implementation handoffs, acceptance and selected checks before recording
the final review. Review and check execution can overlap while source is stable,
but a review's handoff binds every check run and worker record present when
recorded, so record it after the last check you will run; a later check run or
record makes it stale.
Source changes require a new current review within the cap, and later worker/check
mutations can also invalidate review and final adviser context: use a new
immutable review ID, rerun every check the records cite after a source change,
and refresh the other affected evidence. Implementation handoffs can
retain scoped evidence after unrelated edits.

### Follow-up review after a fix revision

Dispatch the first full review of each stage with that stage's role (`review`
for code review). Under Claude Code, dispatch every later review of a stage (the
diff-scoped follow-up, a review scoped to a fix, a confirmation pass) with the
Claude-only `review_followup` role; Codex uses the stage's role for every review.
Resolve the role from the current host policy. Record each follow-up with a new
assignment ID and its own handoff and acceptance, under its stage's record role
([review stages](#review-stages)): a spec, design or test review follow-up is an
`evidence` assignment with no bundle, so a test reviewer never receives the
implementation's changed paths. The rest of this section is about code review
follow-ups, which use task-record role `review` and `owned_paths: ["."]`. The
accepted final code review always binds the whole current source; the gate does not measure how much the
reviewer read. Its bundle is a follow-up of the previous reviewed source: its rows
cover only the files changed since, and it lists the earlier findings with their
dispositions. Its brief asks whether each finding is resolved, whether the changed
hunks are correct, and whether they hold for every caller and consumer in the rows
and when [run in every mode the rows name](#run-every-reach-row-mode); it bounds the
effort, permits reporting anything outside the diff, and asks what was not
re-reviewed.

Use a fresh reviewer: in a measured replay, the first full review missed the defect
that mattered and a fresh reviewer of the revised hunk caught it. Continue an
earlier reviewer only to re-bind a review whose source did not change, such as
after a later check run, or to [run the modes](#run-every-reach-row-mode) its review
missed; neither counts against the cap. Record the follow-up's other findings as `resolved`,
`deferred` or `rejected`. Order the records so the follow-up stays current:

1. Record the earlier review's handoff as soon as it returns, before changing
   source: a review handoff cannot be recorded as completed once the source has
   changed.
2. Fix the source, assigning any fix worker before dispatching it. Then run every
   check that a record or a fix worker's brief cites, once, before recording
   anything that cites it: a source change leaves no earlier run current.
3. Record any fix worker's handoff and acceptance. Close with an `unused`
   acceptance each earlier implementation assignment whose owned paths the fix
   changed (verify reports its scope stale otherwise), and the earlier review, with
   dispositions that cite the fixed source as `resolved` or give the evidence for
   `rejected` (an `unused` acceptance may give `[]` for `evidence` and `check_ids`).
4. Build the follow-up's bundle, assign the review, and record its handoff and
   acceptance last.

The gate checks structure, bindings, current source, actual check records and
declared identity separation; the coordinator still verifies that the reviewer
actually investigated the change and that its conclusions are sound. Declared
changes are checked against scope and source; they do not prove that undeclared
edits never occurred or establish operating-system isolation.

## Proportion

Small and unchanged read-only tasks gain no mandatory review, but delegated work
still needs complete acceptance or explicit closure as unused. Use parallel
workers for independent ownership or questions, usually one or two alongside the
coordinator; keep dependent steps sequential. Measure comparable duration, total
usage, rework and verified findings with `cw metrics`; more agents or passing
synthetic cases do not establish productivity gains.
