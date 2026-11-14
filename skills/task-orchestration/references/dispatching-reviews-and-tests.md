# Dispatching the test engineer and code review

For the coordinator only: never give this file to a worker. The worker rules
are in the [test-engineer](contracts/test-engineer.md),
[code-review](contracts/code-review.md) and
[adversarial-tester](contracts/adversarial-tester.md) contracts.

## Dispatch a test engineer

Dispatch a fresh `test-engineer` when a substantial task adds or materially
changes behavioural tests, including helpers, fixtures, golden data, fuzz
seeds, tolerances, removal and disabling, or when the user asks. It works from
the architect's interface contract, or from your requirement packet when the
task has no design stage. A test-engineer dispatch alone does not make a small
task substantial. Formatting or documentation work needs no new tests.
Explicit user scope and no-tests instructions still apply.

Dispatch the `test_engineer` role as [hosts](hosts.md) says. Never reuse an
implementer, coordinator or source-aware reviewer, or substitute an
evidence-only specialist; when no suitable fresh worker exists, report the
blocker. Pass no implementation conversations, source excerpts, diffs, Git
history, memories, other workers' reasoning or full task records, and check
automatically loaded instructions and initial context as well as the prompt. A
separate conversation, worktree or native assignment is not a filesystem
sandbox: inspect the supplied context and the tool transcript, and report when
the host cannot supply an unexposed context.

## Prepare the test contract

You own requirements, source discovery, implementation inspection and the
execution environment. Prepare a bounded packet outside source, identified by
contract ID, revision, content digest and project snapshot; refresh it and the
assignment when the contract or interfaces materially change (a repository
HEAD alone does not version a dirty contract). The packet holds only the
[inputs the contract lists](contracts/test-engineer.md#inputs-you-receive),
approved dependencies and toolchain, permitted actions, the result contract
and the facts that need escalation:

- Requirements are the interface contract, or a requirement list in the same
  shape.
- Give minimal interface, type and schema extracts and approved dependency API
  references, never implementation bodies; a shared file gives an extract. For
  Go use [the extractor](#extract-go-declarations); otherwise write the public
  signatures by hand.
- Inspect helper bodies before approving scaffolding: a fixture or helper can
  disclose the implementation or bake in its answers.
- Its artefact directory is apart from the architect's and implementer's.

The implementer and the test engineer run in parallel only once the contract
and interfaces are agreed and ownership is disjoint; for colocated tests give
exact file scopes, not the containing directory. You decide whether a public
boundary or an approved test-only seam can exercise a behaviour without
needlessly widening the production API. A contract change follows the
[contract amendment](contract-amendment.md).

### Extract Go declarations

Copy declarations with the extractor, never from memory:
`cw task contract-extract --path <package dir> --symbols Parse,Client.Do
><file outside source>`. Request only what the tests need and read the extract
before sending it: doc comments, var values and const expressions can still
describe the implementation. Remove nothing by hand; drop the symbol and
extract again. Give its path and SHA-256 as the brief's `contract`. When
`cw task verify` reports a stale contract, assign a correction round with a
fresh extract over every test path the stale one owned; source changes that
left every declaration the same are a limitation only.

## Feedback and exposure

You or a source-aware verifier integrate the patch and run the repository's
required checks; raw execution logs stay with the verifier. Forward only what
the [contract](contracts/test-engineer.md#separate-authorship-from-execution)
allows, after inspecting it: compilation errors, stack traces, coverage and
mutation reports, fixture dumps and task records can carry implementation.
Report an implementation build failure as blocked verification; never forward
it. Keep the complete raw evidence apart, so sanitizing does not hide a
failure from the final review.

Diagnose a disagreement against the contract: fix the implementation when it
violates the contract; ask the test engineer for a correction when the test
does. You may apply an unchanged patch and format it mechanically; semantic
edits to assertions, setup, helpers, fixtures, expected data, seeds,
tolerances or skips go back to an unexposed test engineer, since a
source-aware rewrite loses independent authorship.

On exposure, stop or supersede the assignment and dispatch a fresh replacement
from the approved contract, without the contaminated work; keep artefacts
frozen before exposure distinguishable from later work.

## Record test authorship

Record assignment, handoff and acceptance before claiming authorship complete,
and keep execution checks in the plan, owned by you or a separate
`verification` assignment: the change is accepted on their real results. A
fresh [test review](contracts/test-review.md) follows before integration.
Record an exposed author's round `rejected`; it never counts again, even
closed `unused`. A round closed `unused` without a rejection keeps its
authorship. The brief uses `role: "implementation"`, `stage: "tests"`, exact
test-artefact `owned_paths` and `check_ids: []`, and carries the contract, its
`requirements` and the `inputs`; its constraints carry the packet identity and
the read boundary.
Returned hashes and inputs read are workflow records, not proof of who read
or wrote a file.

Report contract coverage, actual checks and their limits. Never label existing
source-aware tests as independently authored, claim that policy YAML launches
a worker, or infer a quality gain from role separation alone.

## Route code-review findings

Findings go to the implementer, except a finding about a test, which goes to a
fresh test engineer within the
[feedback limits](contracts/test-engineer.md#separate-authorship-from-execution);
rounds follow the [review cap](worker-handoffs.md#review-cap). The implementer
never receives `design.md`: forward a finding that only `design.md` supports
as its contract-terms constraint, under the finding's ID with its disposition,
never its summary or rule; or, when the contract or brief should carry the
rule, raise it as a question in contract terms through the
[contract amendment](contract-amendment.md).

## Apply a reproduction test

Apply the reviewer's reproduction test unchanged as a new file under the name
it gave (for example `quota/review_f2_test.go`). It must not be one of the
test engineer's authored files, and the fix worker's brief must not own it:
give exact file scopes that leave it out. When an earlier implementation
assignment owns its path or a directory above it, `.` included, close that
assignment `unused` in the follow-up's record order
([step 3](worker-handoffs.md#follow-up-review-after-a-fix-revision)).

The fix must turn the test green without editing it. Accept it as `resolved`
only when a recorded `cw task run` of a check that runs the test passes: that
acceptance lists the check in `check_ids`, the disposition's `evidence` cites
the test file, and its `reason` says the reviewer wrote it with source access
for that finding. The test stays as a regression test; `cw` reads it as
[`source_aware`](worker-handoffs.md#test-rules-and-test-authorship), never as
requirement coverage, so it never enters `requirement-map.md` or replaces the
test engineer's suite, and needs no test-engineer dispatch. Only the next code
reviewer may replace it. This exception covers only a code reviewer's failing
reproduction tests for its own findings and the
[adversarial tester](#adversarial)'s kept tests; any other behavioural test
still needs a fresh test engineer.

## Adversarial

Opt in to the adversarial tester at classify, before `cw task start`, when the
change touches concurrency, parsing or untrusted input, security or data
integrity, or the user asks. The plan then holds a requirement naming the
opted-in areas and a check that writes a coverage profile outside the source
tree. Dispatch a fresh `adversarial-tester`, no author or reviewer of the
change, in parallel with the code review on the same source, under an
`evidence` assignment, with the inputs its contract lists.

Record its handoff and acceptance before the code review's handoff, and both
before any source change. Reject, and do not add, a test citing a rule the
contract and design do not state. Apply each other kept test as a reproduction
test (above), at a path no implementation assignment owns; its `reason` says
the adversarial tester wrote it with source access. Only an adversarial tester
corrects a kept test that is wrong or stops building, against the pre-fix
source; otherwise drop it and reject its finding. After any later source
change, and before the follow-up review's handoff, close this assignment
`unused` with those dispositions. For a requirement the black-box suite leaves
unguarded, give a fresh test engineer that requirement ID only.
