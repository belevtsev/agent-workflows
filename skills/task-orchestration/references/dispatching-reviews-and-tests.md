# Dispatching the test engineer and code review

For the coordinator only: never give this file to a worker. The worker rules
are in the [test-engineer](contracts/test-engineer.md) and
[code-review](contracts/code-review.md) contracts.

Contents: [dispatch a test engineer](#dispatch-a-test-engineer),
[prepare the test contract](#prepare-the-test-contract),
[extract Go declarations](#extract-go-declarations),
[feedback and exposure](#feedback-and-exposure),
[record test authorship](#record-test-authorship),
[route code-review findings](#route-code-review-findings),
[apply a reproduction test](#apply-a-reproduction-test).

## Dispatch a test engineer

Dispatch a fresh `test-engineer` when a substantial task adds or materially
changes behavioural tests, including helpers, fixtures, golden data, fuzz
seeds, tolerances, removal and disabling, or when the user asks. In the
[full flow](contracts/README.md#flow) it works from the architect's interface
contract; when a substantial task skips the design stage, from your
requirement packet. For a small task the coder writes the tests and the
reviewer checks each against the Code Review Rules; a test-engineer dispatch
alone does not make a small task substantial. Tests the
[adversarial tester](contracts/adversarial-tester.md) keeps are source-access
regression tests and need no dispatch. Formatting or documentation work needs
no new tests. Explicit user scope and no-tests instructions still apply.

Resolve the `test_engineer` role on the current host, keep configured
overrides, pass model and effort explicitly and verify the effective settings
([host mapping](hosts.md)). Codex uses `fork_turns: "none"`; Claude Code a
fresh Agent invocation of `agent-workflows:test-engineer`, which has no Skill,
Agent or Workflow tool, so list the `SKILL.md` path of any skill to apply
among the approved inputs. Never reuse an implementer, coordinator or
source-aware reviewer, or substitute an evidence-only specialist; when no
suitable fresh worker exists, report the blocker. Pass no implementation
conversations, source excerpts, diffs, Git history, memories, other workers'
reasoning or full task records, and check automatically loaded instructions
and initial context as well as the prompt. A separate conversation, worktree
or native assignment is not a filesystem sandbox: inspect the supplied context
and the tool transcript, and report when the host cannot supply an unexposed
context.

## Prepare the test contract

You own requirements, source discovery, implementation inspection and the
execution environment. Prepare a bounded packet outside source, identified by
contract ID, revision, content digest and project snapshot; refresh it and the
assignment when the contract or interfaces materially change (a repository
HEAD alone does not version a dirty contract). The packet holds only:

- Numbered requirements in the shape the
  [contract](contracts/test-engineer.md#inputs-you-receive) lists: the
  interface contract, or a requirement list in the same shape. Agreed
  requirements stay apart from observed behaviour and open questions.
- Minimal interface, type and schema extracts and approved dependency API
  references, never implementation bodies; a shared file gives an extract. For Go use
  [the extractor](#extract-go-declarations); otherwise write the public
  signatures by hand.
- Approved scaffolding, dependencies, toolchain and runner instructions, and
  skill paths, such as `test-strategy` with its
  [test design](../../test-strategy/references/test-design.md),
  [property testing](../../test-strategy/references/property-testing.md) and
  [end-to-end](../../test-strategy/references/e2e.md) references. Inspect
  helper bodies first: a fixture or helper can disclose the implementation or
  bake in its answers.
- For end-to-end tests, how to run the real binary in a temporary environment.
- Allowed input files, exclusive test-artefact output paths, permitted
  actions, the result contract, facts that need escalation, and an artefact
  directory outside the source tree, apart from the architect's and
  implementer's.

The implementer and the test engineer run in parallel only once the contract
and interfaces are agreed and ownership is disjoint; for colocated tests give
exact file scopes, not the containing directory. You decide whether a public
boundary or an approved test-only seam can exercise a behaviour without
needlessly widening the production API. A contract change follows the
[contract amendment](contracts/README.md#contract-amendment): its author (the
architect, or you for a requirement packet, from the request and the user's
answers) revises it without implementation.

### Extract Go declarations

Copy declarations with the extractor, not from memory (wrong signatures cost
author rounds):

```sh
cw task contract-extract --path internal/pkg --symbols Parse,Options,Client.Do >/private/packet/contract-r1.json
shasum -a 256 /private/packet/contract-r1.json
```

`cw task contract-extract --help` states what it prints. Request only what the
tests need and read the extract before sending it: doc comments, var values
and const expressions can still describe the implementation. Remove nothing by
hand; drop the symbol and extract again, because `assign` compares the file.
Give its path and SHA-256 as the brief's `contract`, saved outside source.
`cw task assign` refuses an extract that differs from what current source
yields, so extract again after a source edit. When `cw task verify` reports a
stale contract, assign a correction round with a fresh extract over every test
path the stale one owned; source changes that left every declaration the same
are a limitation only.

## Feedback and exposure

You or a source-aware verifier integrate the patch and run the repository's
required checks; raw execution logs stay with the verifier.
Forward only what the
[contract](contracts/test-engineer.md#separate-authorship-from-execution)
allows, after inspecting it: compilation errors, stack traces, coverage and
mutation reports, fixture dumps and task records can carry implementation. An
implementation build failure is reported as blocked verification, not
forwarded. Keep the complete raw evidence apart, so sanitizing does not hide a
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
authorship. The brief carries the contract, its `requirements` and the
`inputs`; its constraints carry the packet identity and the read boundary.
Returned hashes and inputs read are workflow records, not proof of who read
or wrote a file.

Substantial work keeps its decision checkpoints, when required, and a distinct
source-aware code reviewer: blind authorship does not replace review. Report
contract coverage, actual checks and their limits. Do not label existing
source-aware tests as independently authored, claim that policy YAML launches
a worker, or infer a quality gain from role separation alone. Host smokes are
synthetic acceptance evidence; keep live model calls out of CI.

## Route code-review findings

Findings go to the implementer, except a finding about a test, which goes to a
fresh test engineer within the
[feedback limits](contracts/test-engineer.md#separate-authorship-from-execution);
rounds follow the [review cap](worker-handoffs.md#review-stages). The
implementer never receives `design.md`: forward a finding that only
`design.md` supports as its contract-terms constraint, under the finding's ID
with its disposition, never its summary or rule; or, when the contract or
brief should carry the rule, raise it as a question in contract terms through
the [contract amendment](contracts/README.md#contract-amendment).

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
`source_aware`
([test authorship](worker-handoffs.md#test-rules-and-test-authorship)), never
as requirement coverage, so it never enters `requirement-map.md` or replaces
the test engineer's suite, and needs no test-engineer dispatch. Only the next
code reviewer may replace it. This exception covers only a code reviewer's
failing reproduction tests for its own findings and roles whose contract and
`test-strategy` name it, such as the
[adversarial tester](contracts/adversarial-tester.md#after-a-finding); any
other behavioural test still needs a fresh test engineer.
