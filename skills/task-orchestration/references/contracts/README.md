# Role contracts

Each file is the whole assignment for one worker role or review stage. A worker
reads only the contract the brief names, so each role's output is independent
evidence.

| Role | Agent | Contract | Reads | Writes |
| --- | --- | --- | --- | --- |
| Spec writer | `spec-writer` | [spec-writer.md](spec-writer.md) | sources, repository, review findings | OpenSpec change folder, `change-log.md` |
| Spec review | `reviewer` | [spec-review.md](spec-review.md) | change folder, sources, repository | `spec-review.md` |
| Architect | `architect` | [architect.md](architect.md) | request or reviewed change folder, repository | `design.md`, `interface-contract.md`, `implementation-brief.md` |
| Design review | `reviewer` | [design-review.md](design-review.md) | the architect's artefacts, spec, sources, repository | `design-review.md` |
| Implementer | `implementer` | [implementer.md](implementer.md) | brief, interface contract, repository | the change, `self-review.md` |
| Test engineer | `test-engineer` | [test-engineer.md](test-engineer.md) | interface contract, interface extract | tests, `test-matrix.md`, `requirement-map.md` |
| Test review | `reviewer` | [test-review.md](test-review.md) | contract, extract, tests, stub | `test-review.md` |
| Code review | `reviewer` | [code-review.md](code-review.md) | resulting source, reviewer bundle, interface contract, `design.md`, test matrix and requirement map, coverage profile | findings, a failing test per behaviour finding |
| GitHub worker | `github-worker` | [github-worker.md](../github-worker.md) | verified facts, carried authorization | commit, push, pull request |

## Flow

Every stage that produces an artefact has a review loop: the reviewer's findings
go back to that stage's author, who revises and answers each one, and the
[review cap](../worker-handoffs.md#review-stages) bounds the rounds of each
stage separately. After the task's before checkpoint, when its checkpoints are
required, advisers join only at the
[adviser points](../consultation-helper.md#adviser-points-in-the-flow) (a to d
below) and only when available; without them every step runs unchanged.

1. **Spec writer ⇄ reviewer (spec).** The spec writer turns the source
   documents into an OpenSpec change folder; the reviewer grades findings
   against the sources under [spec review](spec-review.md). While that review
   runs, the coordinator asks the user the spec's open questions instead of
   waiting for it. When advisers are bound, an open question the spec leaves
   to a choice waits for them at point a, after the review, and then goes to
   the user with their input. The answers reach the spec writer with the
   findings, and the reviewer only in the follow-up review, listed with the
   coordinator's dispositions, and through the folder's diff
   ([spec review inputs](spec-review.md#inputs-you-receive)); a revision that
   answers them gets that follow-up even when the review found no `blocker`
   or `major`.
2. **Architect ⇄ reviewer (design).** The architect writes the three artefacts
   from the reviewed change folder. When it chooses between real options, the
   coordinator consults advisers before the design review (point b). The
   reviewer, never the architect, checks the decision record and the interface
   contract under [design review](design-review.md). While that review runs,
   the coordinator asks the user the architect's open questions; the answers
   reach the architect with the findings and the reviewer only in the
   follow-up review, listed with the dispositions, and through the artefacts'
   diff
   ([design review inputs](design-review.md#inputs-you-receive)), which a
   revision answering them always gets.
3. **Contract.** The coordinator fixes `interface-contract.md`, extracts the
   public interface and hashes both before dispatch. For a later
   [contract amendment](#contract-amendment), it records whether the tree held
   uncommitted or untracked source and, if it did, saves a copy of all of it
   outside the source tree.
4. **Implementer ∥ test engineer, isolated**: same contract, disjoint owned
   paths, fresh contexts. The implementer never sees `design.md`; the test
   engineer never sees the implementation. The spec's scenarios reach the test
   engineer as requirements through the interface contract
   ([architect](architect.md#inputs-you-receive)). A question either worker
   returns by requirement ID that needs the contract changed follows the
   [contract amendment](#contract-amendment) procedure.
5. **Test engineer ⇄ reviewer (tests)**, before integration. A reviewer that is
   not the test engineer and sees no implementation checks the matrix and tests
   against the contract under [test review](test-review.md). Test review needs
   no implementation, so it starts as soon as the test engineer's handoff is
   recorded and overlaps the implementer's work; its loop finishes before
   integration. Red-first runs against the stub; mutant results, which are
   optional and need the implementation, reach it only if the coordinator ran
   them before the loop closes.
6. **Integrate and run.** Apply both results unchanged and run the tests. Fix
   the implementation when it violates a requirement; return a test that
   contradicts the contract to an unexposed test engineer.
7. **Reviewer (code) ⇄ implementer.** A further reviewer reviews the integrated
   source against the requirements, the interface contract and `design.md`,
   with the bundle and the final check's coverage profile, under
   [code review](code-review.md); the implementer fixes accepted findings, and
   a finding about a test goes to a fresh test engineer by requirement ID,
   contract-level inputs and expected outcome only. A finding that only
   `design.md` supports reaches the implementer as a constraint in contract
   terms; a rule the contract should carry goes through the
   [contract amendment](#contract-amendment).
   Before rejecting a `blocker` or `major` finding at this or any earlier stage,
   the coordinator consults advisers (point c).
8. **Final checkpoint** (point d, when checkpoints are required), then
   **`cw task verify`** checks that every assignment, handoff, acceptance, check
   run and review is recorded and current.
9. **Commit** through the GitHub worker with the user's authorization.

The skip rules are unchanged. A small task stays lightweight and runs none of
these stages, the spec and design stages included: the coder writes the tests
and the reviewer checks each against the Code Review Rules. A substantial task
skips step 1 when it already has a reviewed spec (an OpenSpec change that passed
review, or current specs that cover the request), or when the request is precise
enough to be its own spec: every behaviour, input, output and failure stated
with its values, and nothing left to settle. The architect then works from that
spec, or from the request when there is none, and the design review still runs.

Artefacts and records stay outside the source tree; the change folder is the
one exception, under `openspec/changes/`. Model and effort come from the
policy roles `spec_writer`, `spec_review`, `architect`, `design_review`,
`implementer`, `test_engineer`, `test_review`, `review` and `review_followup`
([hosts](../hosts.md)).

## Contract amendment

The interface contract changes only through its author. When the implementer
or the test engineer returns a question by requirement ID, or a code-review
finding raises one, that the contract as written cannot answer (an ambiguous or conflicting requirement, missing
expected behaviour, unclear error semantics, a needed change to the public
interface or a missing public seam), the coordinator does not answer it from
the code. A question the contract already answers gets the quoted requirement
and no amendment.

1. **Batch.** Collect the open questions from both workers and the code
   review into one request, each as its requirement ID and the gap in contract
   terms. Strip anything a question carries from either worker's work or the
   review's evidence: implementation source, tests, reproduction tests, diffs,
   logs, coverage and reasoning. Workers keep working on the requirements the
   questions do not touch.
2. **Forward to the architect, without implementation.** Continue the original
   architect with that request; when it cannot be continued, give a fresh
   architect the three artefacts and their review history. The coordinator
   gives it a separate checkout of the source the design was read from (the
   task's baseline revision, with any uncommitted design inputs applied from
   the copy saved before dispatch, never from the shared tree; without the
   record from step 3 of the [flow](#flow), or without the copy it says was
   saved, the coordinator reports the blocker) and its path, and tells it to
   read source only there, never in the shared working tree with the workers'
   edits, at paths it used before; the same holds for the re-bind reviewer in
   step 4. So code cannot shape the contract or reach the test engineer through
   it. A decision only the user can make goes to the user as in step 2 of the
   [flow](#flow).
3. **Change log.** The architect revises `interface-contract.md`, and
   `implementation-brief.md` or `design.md` where they follow, and writes
   `change-log.md` answering each question by requirement ID: what changed,
   or why nothing did ([architect](architect.md#required-outputs)).
4. **Scoped design review.** A fresh reviewer re-binds the design review,
   scoped to the changed requirement IDs, with the forwarded questions, the
   artefacts' diff and the same checkout
   ([design review inputs](design-review.md#inputs-you-receive)), dispatched
   as a later review of the stage
   ([`review_followup`](../worker-handoffs.md#follow-up-review-after-a-fix-revision)
   under Claude Code). Its findings go back to the architect. Each amendment
   gets, for each stage it reaches, a [review cap](../worker-handoffs.md#review-stages)
   of the same shape apart from that stage's earlier rounds: here this re-bind
   counts as the full review, then at most one diff-scoped follow-up after the
   fixes and one review scoped to a fix of a defect that follow-up found.
   Report these reviews in the result.
5. **Re-hash and refresh the implementer.** The coordinator fixes the
   amended contract and hashes `interface-contract.md` again. For each
   worker, once it hands off, the coordinator records that handoff and closes
   the earlier assignment `unused` with the amendment as the reason;
   `cw task assign` refuses implementation ownership that overlaps an
   assignment with no recorded handoff. The implementer's first refreshed
   assignment, under a new ID over the amended brief's owned paths, puts the
   amended declarations into source exactly as the contract states them, and
   nothing else, then hands off. Its further assignment, for the rest of its
   work, waits until the test engineer's refreshed assignment is recorded
   (step 6), so no implementer edit lands between the extract and `assign`.
6. **Re-extract and refresh the test engineer.** For Go, the coordinator then
   extracts the test engineer's symbols again with `cw task contract-extract`
   and hashes the new extract; `assign` refuses an extract that differs in
   any byte from what current source yields. It reads the extract before
   sending it, against the [extract rules](test-engineer.md#extract-go-declarations)
   and for unexported fields of a type: an extract that carries anything the
   implementer wrote beyond the contract's declarations is not sent. Drop a
   symbol the tests do not need and extract again, as those rules say; when a
   needed symbol still carries it, report the blocker. The refreshed
   test-engineer assignment, under a new ID, owns at least every test path
   its earlier one owned, which supersedes the earlier extract for
   `cw task verify`.

Each worker receives the amended contract (the implementer also its amended
brief), the changed requirement IDs and, for each of its questions the
architect answered without a change, the requirement text it cites; never the
change log, `design.md` or the other worker's work. Cases and code for
requirements the amendment does not change keep moving and keep their work.
Tests revised for a changed requirement return to
[test review](test-review.md), scoped to those requirement IDs, with a stub
rebuilt from the fresh extract, under the amendment's own cap for the test
stage. A task whose test engineer works from the coordinator's requirement
packet, with no architect, refreshes the packet from the request and the user's answers, never
from the implementation ([prepare the contract](test-engineer.md#prepare-the-contract-before-dispatch)).
