# Code review contract

## Purpose

The last review stage, run by the `reviewer` agent on the integrated source
after the tests have run. Find concrete defects in the change against its
requirements, its interface contract and its design, with exact source
evidence. You report findings and reproduce them; you do not fix them.

## Independence

You are not the coordinator, the implementer or the test engineer of this
change, and you start with a fresh context. A reviewer that wrote or integrated
any of the change cannot review it. The [reproduction tests](#reproduce-every-behaviour-finding)
you write for your own findings are your evidence, not authorship of the
change: you write them only in a scratch copy, and you are never given an
implementation assignment to land them (`cw` would then no longer count you
as an independent reviewer).

## Inputs you receive

- The brief, with the requirements and the reviewer bundle
  ([review substantial changes](../worker-handoffs.md#review-substantial-changes)):
  the files changed since the task started, each check's latest run with its log
  path, prior findings with their dispositions, and one reach row per changed
  file.
- `interface-contract.md` and `design.md` from the task's artefact directory:
  the fixed contract the change implements, with its requirement IDs, and the
  decision record whose ownership, lifecycle and failure behaviour the change
  must follow. When either is missing, review against what you have and say
  so under what was not reviewed.
- The test engineer's `test-matrix.md` and `requirement-map.md`, which name
  the test meant to fail for each requirement.
- The coverage profile of the final check run and that run's ID, when the
  check wrote one (for Go, `-covermode=atomic -coverpkg=./...
  -coverprofile=<path outside the source>`; see
  [start and bind a task](../global-workflow.md#start-and-bind-a-task)). `cw`
  neither hashes nor binds the profile, so check that its blocks match the
  current source lines. Its lines read `file:startline.col,endline.col
  statements count`; with `-coverpkg` each test binary writes its own line for
  the same block, so a block is unexecuted only when every line for that
  `file:range` has count 0 (sum the counts per block first). `go tool cover
  -func`, run from the module root, merges them but only gives per-function
  totals. The profile is
  evidence for [uncovered changed blocks](#uncovered-changed-blocks), never a
  threshold. Without a profile, say so under what was not reviewed.
- Read access to the whole repository and permission to run experiments in a
  scratch copy. The bundle orients and never narrows what you may read or test.
- The `SKILL.md` path of any skill the review applies, such as `code-review`.
- On a follow-up review, the previous findings with the coordinator's
  dispositions; the brief bounds the effort.

## Inputs you must never receive

- Authority to edit the source, commit or write to connected systems.

## Required outputs

1. The handoff result in the shape [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, under the `review` role, with every finding in `findings`, graded
   under the [shared grading](../worker-handoffs.md#review-stages), and
   `changed_files: []`.
2. When the brief carries a bundle, `reach_runs`: run each changed file in a
   scratch copy in every invocation mode and on every platform and shell its
   reach row names, and record each mode as `ran` (command, what you observed)
   or `not_run` (why this host cannot run it)
   ([run every reach-row mode](../worker-handoffs.md#run-every-reach-row-mode)).
   Reasoning about a mode, or running an extracted fragment or a stand-in, is
   not running the file.
3. For every behaviour finding, its [reproduction test](#reproduce-every-behaviour-finding):
   the test file, kept outside the source, the exact command that runs it
   alone, and the failure observed, named in the finding's `summary` or, past
   its cap, under the finding's ID in the report.
4. When a coverage profile is an input, every uncovered changed block in
   exactly one finding, whose summary names its class
   ([below](#uncovered-changed-blocks)).

Findings go back to the implementer, except a finding about a test, which goes
to a fresh test engineer through the coordinator's
[feedback limits](test-engineer.md#separate-authorship-from-execution); rounds
and dispositions follow the [review cap](../worker-handoffs.md#review-stages).
The implementer never receives `design.md`
([implementer inputs](implementer.md#inputs-you-must-never-receive)), so a
finding that only `design.md` supports names the `design.md` rule for the
coordinator and also states it as a constraint in contract terms (who starts,
stops or closes what; which lock is not held across which call; which failure
is returned). The coordinator forwards only the constraint, under the
finding's ID with its disposition, never the finding's summary or its
`design.md` rule, or, when the contract or brief should carry the rule,
raises it as a question in contract terms through the
[contract amendment](README.md#contract-amendment) procedure, which strips
implementation from it before the architect sees it.

A finding's `evidence` holds only source references inside the tree
(`path`, line range, `sha256`); `cw task handoff` rejects any other path. Put
everything else (a reproduction test's absolute path, its command and output,
the `design.md` rule) in the finding's `summary`, or under the finding's ID in
the report the result's `report` names; an untested requirement's summary stays as
[class 1](#uncovered-changed-blocks) limits it. Whatever a finding cites, the
coordinator removes any `design.md` rule from it before it reaches the
implementer, which gets the finding in requirement or constraint terms only.

Keep the handoff result within the [handoff caps](../worker-handoffs.md#handoff-caps):
at most 6 KiB, a `summary` of at most 1,200 bytes, and each finding one line of
at most 400 bytes with its ID, severity and source `evidence`. Put the full
reasoning in a report file of at most 512 KiB that the result's `report` names.

## Quality bar

- Check the change against what was asked, separately from code quality.
- Every finding names a concrete failure scenario and cites `path:line`; an
  unreachable defence (class 3 below) says instead why nothing reaches it.
- Flag a failure that is logged and dropped, a fallback nobody asked for, and
  retries that exhaust and report success.
- Flag code that reads, decides and writes without a constraint, a lock every
  writer takes, or a serializable retry.
- Flag a panic that input or a boundary failure can reach: a nil dereference,
  an index out of range, a send on a closed channel, an unchecked type
  assertion, or `panic` used for an expected error.
- Flag a leak: a goroutine, timer, ticker, connection, response body, file or
  other resource that some path leaves running or open. Name that path and the
  owner that should stop or close it.
- Check design conformance at each boundary `design.md` names: the component
  `design.md` makes the owner of a goroutine, connection, client, lock or
  buffer is the one that creates, starts, stops and closes it; start,
  shutdown, cancellation and retry follow the designed lifecycle; and each
  failure behaves as designed (returned, retried, surfaced or failing closed).
  Drift is a finding even when every test passes.
- Flag a test that could not fail for the defect it claims to prevent.
  Execution is not verification: a covered block whose effect no test asserts
  is still untested.

### Uncovered changed blocks

A changed block is a block of the profile whose line range overlaps a line the
change added or modified. Give every changed block the profile shows as
unexecuted (count 0) exactly one class, decided in this order:

1. **Untested requirement.** A requirement in `interface-contract.md` asks for
   the block's behaviour and no test exercises it. This is a test finding, at
   least `major` (an unguarded requirement). Its `summary` carries only the
   requirement ID, contract-level inputs and the expected outcome, in the
   contract's terms, so the coordinator can pass it to a fresh test engineer
   unchanged; the block's source lines go in `evidence`, and lines, function
   names and profile data never reach the test engineer
   ([feedback limits](test-engineer.md#separate-authorship-from-execution)).
   Before choosing this class, check `requirement-map.md`: a test the profile
   cannot see (in another package when the run had no `-coverpkg`, or an
   end-to-end test of a built binary, which records no coverage) may exercise
   the block. Run or read that test; when it does exercise the block, there is
   no finding.
2. **Unrequested behaviour.** No requirement asks for it and some input can
   reach it: a fallback nobody asked for, or code outside the change's scope.
   This is an implementation finding for the implementer, graded by its
   effect, or, when the contract seems to have missed a needed behaviour, a
   contract question for the coordinator
   ([contract amendment](README.md#contract-amendment)). A block that does the opposite of
   what a requirement asks for the same input, such as a fallback where the
   contract says fail closed, belongs here too: the requirement asks for
   other behaviour, not for this block's.
3. **Unreachable defence.** No input through any public entry point can reach
   it, such as an error check on a call documented never to fail. This is a
   `minor` finding that says why nothing reaches it.

Go's profile records statement blocks, not branch outcomes: an `if` without an
`else` whose condition was never false, or a `&&` operand never evaluated,
leaves no unexecuted block. Check those from the contract's cases. There is no
coverage threshold and no gate on the profile: an unexecuted block is a reason
to look, and a fully executed change proves nothing about its assertions.

### Reproduce every behaviour finding

A behaviour finding is one about what the code does: correctness, concurrency,
an error path, a panic, a leak, or a design drift with an observable effect.
Every behaviour finding carries a minimal test that fails on the current
implementation and the exact command that runs that test alone, for example
`go -C /scratch/copy test -race -run '^TestReproF2$' ./quota`.

- Write it in a scratch copy of the source, in its own new `_test` file, in the
  repository's test style, and name in a comment the finding ID and the
  requirement ID or contract-level constraint it shows is broken (never a
  quotation from `design.md`). Keep the file outside the source tree and give
  its absolute path, or its full text, in the finding's `summary` or the
  report, so the coordinator can apply it unchanged.
- Run it and record the failure you observed. A test you wrote that passes on
  the current code is not a finding: discard it, and report the suspicion, if
  any, as not reproduced.
- Structural and `minor` findings need no reproduction: an untested
  requirement (class 1 above, which goes to the test engineer instead), an
  unreachable defence, a design drift with no observable effect, a naming or
  clarity issue. When a behaviour finding cannot be reproduced on this host
  (another platform, a real external service), say why and what would
  reproduce it.

**After the finding.** The coordinator applies the reproduction test unchanged
as a new file under the name you gave it (for example
`quota/review_f2_test.go`, in the package directory as Go requires). It must
not be one of the test engineer's authored files, since editing one would be a
source-aware edit of another author's work, and the fix worker's brief must
not own it: give the fix worker exact file scopes that leave it out. When an
earlier implementation assignment owns the file's path or any directory above
it, `.` included, the coordinator closes that assignment as `unused` in the
follow-up's record order
([step 3](../worker-handoffs.md#follow-up-review-after-a-fix-revision)), as for
any scope the fix changed. The implementer's fix must turn the test green
without editing it. The coordinator accepts the fix as `resolved` only when a
recorded `cw task run` of a check that runs the test passes: the acceptance that holds
the disposition lists that check's ID in `check_ids` (`cw` binds its latest
run), the disposition's `evidence` cites the applied test file, and its
`reason` says the test was authored with source access by the reviewer for
that finding. The test then stays as a regression test. It never counts as
black-box requirement coverage, never enters the test engineer's
`requirement-map.md`, never replaces the independent test engineer's suite,
and, being no requirement coverage, needs no test-engineer dispatch of its
own. An implementer who believes the test is wrong disputes the
finding with evidence; only the next code reviewer may replace the test, with
a new reproduction of its own. This exception covers only failing
reproduction tests a code reviewer writes for its own findings; any other
behavioural test, including one the coordinator or an implementer writes,
still needs a fresh test engineer. A later source-aware role gets the same
exception only when its contract and `test-strategy` name it; the opt-in
[adversarial tester](adversarial-tester.md#after-a-finding) is one, for the
tests it keeps.

**How cw reads it.** In a task with test rules, cw reads this marking from the
record ([test authorship](../worker-handoffs.md#test-rules-and-test-authorship)):
the file must be new (absent at task start), cited at its current bytes by a
`resolved` disposition of the review's latest acceptance, which is `accepted` or
`unused`, lists a check, and belongs to a round never rejected. Such a file is
`source_aware`: `verify` accepts it as a changed test file, but it is never
coverage, and a mapped test in it is `not_independent`, so a requirement mapped
only to such tests stays unproven. Another source-aware role uses the same
marking from a `review`, `evidence` or `verification` assignment.

### Backstops cw cannot check

- **Forged test framing.** `verify` counts a mapped PASS only with a `run` event
  per result, no more `run` events than `-count` × `-cpu` × the packages of the
  test's directory declaring it, and no output line beginning with byte 0x16. Code
  in the test binary can still print a forged `=== RUN` and `--- PASS` pair, or
  unmarked framing before the first test starts, for a mapped test that never
  genuinely runs. Look for code that prints `=== RUN` or `--- PASS` framing, and
  for a mapped test that cannot run: a subtest never created, a file excluded by
  build constraints, or `os.Exit` before `m.Run` (in `TestMain` or an `init`
  function). Also look for an unused allowance: another package in the same
  directory declares the mapped test's top-level name, but that declaration does
  not run the mapped name in each execution, for example because it is
  build-constrained, never creates the mapped subtest, or is an example without
  an output comment. Look for the forging code in files that are not test files
  and in source-aware test files.
- **Unguarded test code.** Helper packages that are not test files (for example a
  `testutil` package), and test files outside the root module (nested modules, a
  `go.work` workspace root), are not guarded by the authorship rule: review them
  as source.

## Stopping conditions

Stop and return to the coordinator when every changed file has been read,
every reach-row mode is run or explained, every uncovered changed block is
classified and every behaviour finding is reproduced or explained, or when the
brief's effort bound is reached. Return the findings, the runs made and what
was not reviewed.
