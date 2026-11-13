# Contract amendment

## Before dispatch

At the contract step of the [flow](contracts/README.md#flow), the coordinator
records whether the tree held uncommitted or untracked source and, if it did,
saves a copy of all of it outside the source tree.

## Procedure

The interface contract changes only through its author. When the implementer,
the test engineer or the [adversarial tester](contracts/adversarial-tester.md) returns a
question by requirement ID, or a code-review
finding raises one, that the contract as written cannot answer (an ambiguous or conflicting requirement, missing
expected behaviour, unclear error semantics, a needed change to the public
interface or a missing public seam), the coordinator does not answer it from
the code. A question the contract already answers gets the quoted requirement
and no amendment.

1. **Batch.** Collect the open questions from the workers and the code
   review into one request, each as its requirement ID and the gap in contract
   terms. Strip anything a question carries from any worker's work or the
   review's evidence: implementation source, tests, reproduction tests, diffs,
   logs, coverage and reasoning. Workers keep working on the requirements the
   questions do not touch. An amendment needs an architect round and a
   test-engineer round, so it starts only when both
   [budgets](worker-handoffs.md#review-stages) have room.
2. **Forward to the architect, without implementation.** Continue the original
   architect with that request; when it cannot be continued, give a fresh
   architect the three artefacts and their review history. The coordinator
   gives it a separate checkout of the source the design was read from (the
   task's baseline revision, with any uncommitted design inputs applied from
   the copy saved before dispatch, never from the shared tree; without the
   record from the contract step of the [flow](contracts/README.md#flow), or without the copy it says was
   saved, the coordinator reports the blocker) and its path, and tells it to
   read source only there, never in the shared working tree with the workers'
   edits, at paths it used before; the same holds for the re-bind reviewer in
   step 4. So code cannot shape the contract or reach the test engineer through
   it. A decision only the user can make goes to the user as in the design stage
   of the [flow](contracts/README.md#flow).
3. **Change log.** The architect revises `interface-contract.md`, and
   `implementation-brief.md` or `design.md` where they follow, and writes
   `change-log.md` answering each question by requirement ID: what changed,
   or why nothing did ([architect](contracts/architect.md#required-outputs)).
4. **Scoped design review.** A fresh reviewer re-binds the design review,
   scoped to the changed requirement IDs, with the forwarded questions, the
   artefacts' diff and the same checkout
   ([design review inputs](contracts/design-review.md#inputs-you-receive)), dispatched
   as a later review of the stage
   ([`review_followup`](worker-handoffs.md#follow-up-review-after-a-fix-revision)
   under Claude Code). Its findings go back to the architect. Each amendment
   gets, for each stage it reaches, a [review cap](worker-handoffs.md#review-stages)
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
   sending it, against the [extract rules](contracts/test-engineer.md#extract-go-declarations)
   and for unexported fields of a type: an extract that carries anything the
   implementer wrote beyond the contract's declarations is not sent. Drop a
   symbol the tests do not need and extract again, as those rules say; when a
   needed symbol still carries it, report the blocker. The refreshed
   test-engineer assignment, under a new ID, owns at least every test path
   its earlier one owned, which supersedes the earlier extract for
   `cw task verify`. Its brief re-declares in `requirements` every requirement
   it still carries, not only the changed IDs.

Each worker receives the amended contract (the implementer also its amended
brief), the changed requirement IDs and, for each of its questions the
architect answered without a change, the requirement text it cites; never the
change log, `design.md` or the other worker's work. Cases and code for
requirements the amendment does not change keep moving and keep their work.
Tests revised for a changed requirement return to
[test review](contracts/test-review.md), scoped to those requirement IDs, with a stub
rebuilt from the fresh extract, under the amendment's own cap for the test
stage. A task whose test engineer works from the coordinator's requirement
packet, with no architect, refreshes the packet from the request and the user's answers, never
from the implementation ([prepare the contract](contracts/test-engineer.md#prepare-the-contract-before-dispatch)).
