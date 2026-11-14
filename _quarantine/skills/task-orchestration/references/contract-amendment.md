# Contract amendment

## Before dispatch

At the contract step of the [flow](contracts/README.md#flow), record whether
the tree holds uncommitted or untracked source; if it does, save a copy of
all of it outside the source tree.

## Procedure

Follow this when the implementer, the test engineer or the adversarial tester
returns a question by requirement ID, or a code-review finding raises one,
that the contract as written cannot answer. The contract changes only through
its author: never answer from the code. A question the contract already
answers gets the quoted requirement and no amendment.

1. **Batch.** Collect the open questions from the workers and the code review
   into one request, each as its requirement ID and the gap in contract
   terms. Strip everything that comes from a worker's work or the review's
   evidence: implementation source, tests, reproduction tests, diffs, logs,
   coverage and reasoning. Start only when the
   [round budget](worker-handoffs.md#round-budget) has room for an architect
   round and a test-engineer round.
2. **Forward to the architect, without implementation.** Continue the
   original architect with the request; when it cannot be continued, give a
   fresh architect the three artefacts and their review history. Give it,
   and the reviewer in step 4, the path of a separate checkout of the source
   the design was read from: the task's baseline revision, with any
   uncommitted design inputs applied from the copy saved before dispatch,
   never from the shared tree. Both read source only there. Without the
   record from the contract step, or the copy it says was saved, report the
   blocker. A decision only the user can make goes to the user as in the
   design stage of the flow.
3. **Change log.** The architect revises `interface-contract.md`, and
   `implementation-brief.md` or `design.md` where they follow, and answers
   each question by requirement ID in `change-log.md`.
4. **Scoped design review.** A fresh reviewer re-binds the design review,
   scoped to the changed requirement IDs, with the forwarded questions and
   the artefacts' diff, dispatched as a
   [later review](worker-handoffs.md#follow-up-review-after-a-fix-revision)
   of the stage. Its findings go back to the architect. For each stage it
   reaches, the amendment has its own
   [review cap](worker-handoffs.md#review-cap), apart from that stage's
   earlier rounds; this re-bind is its full review. Report these reviews in
   the result.
5. **Re-hash and refresh the implementer.** Fix the amended contract and hash
   `interface-contract.md` again. Once each worker hands off, record that
   handoff and close its earlier assignment `unused` with the amendment as
   the reason: `cw task assign` refuses implementation ownership that
   overlaps an assignment with no recorded handoff. The implementer's first
   refreshed assignment, under a new ID over the amended brief's owned paths,
   puts the amended declarations into source exactly as the contract states
   them, and nothing else, then hands off. Assign the rest of its work only
   after the test engineer's refreshed assignment is recorded (step 6).
6. **Re-extract and refresh the test engineer.** For Go, extract the test
   engineer's symbols again with `cw task contract-extract` and hash the new
   extract: `assign` refuses an extract that differs in any byte from what
   current source yields. Read it before sending it, against the
   [extract rules](dispatching-reviews-and-tests.md#extract-go-declarations)
   and for unexported fields of a type. Never send an extract that carries
   anything the implementer wrote beyond the contract's declarations: drop a
   symbol the tests do not need and extract again; when a needed symbol still
   carries it, report the blocker. The refreshed assignment, under a new ID,
   owns at least every test path the earlier one owned, which supersedes the
   earlier extract for `cw task verify`. Its brief re-declares in
   `requirements` every requirement it still carries, not only the changed
   IDs.

Each worker receives the amended contract (the implementer also its amended
brief), the changed requirement IDs and, for each of its questions answered
without a change, the requirement text it cites; never the change log,
`design.md` or the other worker's work. Work on requirements that the
questions and the amendment do not touch keeps moving and is kept. Tests
revised for a changed
requirement return to test review, scoped to those requirement IDs, with a
stub rebuilt from the fresh extract, under the amendment's own cap for the
test stage. With no architect, refresh the
[requirement packet](dispatching-reviews-and-tests.md#prepare-the-test-contract)
from the request and the user's answers, never from the implementation.
