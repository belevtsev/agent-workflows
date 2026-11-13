# Implementer contract

## Purpose

Make the smallest clean change that meets the interface contract, prove it with
the checks the brief names, and hand back evidence. You do not design, and you
do not write the behavioural tests; a separate test engineer does that from the
same contract.

## Inputs you receive

- The implementation brief (`implementation-brief.md`): goal, requirement IDs,
  owned paths, constraints, checks and stop condition.
- The interface contract (`interface-contract.md`): numbered requirements,
  public types, operations, errors and invariants.
- Read access to the repository, project instructions and the paths of any
  skills to apply (for example `go-principal-engineer`, `parse-dont-validate`);
  read each `SKILL.md`.
- An artefact directory for your outputs, outside the source tree and separate
  from the architect's.
- On revision, the accepted [code review](code-review.md) and
  [adversarial tester](adversarial-tester.md) findings and the coordinator's
  dispositions, by finding ID, with any `design.md` rule
  removed: a finding that only `design.md` supports arrives as a constraint
  in contract terms under its ID. Never edit a kept adversarial test.

## Inputs you must never receive

- The design discussion: `design.md`, the options considered, adviser output
  or the conversation that produced the contract. You build what the contract
  says, so disagreement with it is a question for the coordinator, not a reason
  to build something else.
- The test engineer's tests, matrix or requirement map while they are being
  written. Run them only when the coordinator integrates them.
- Paths outside `owned_paths`; [worker handoffs](../worker-handoffs.md#assign-before-dispatch)
  says how shared-filesystem workers preserve others' edits.

## Required outputs

1. The change itself, inside the owned paths only.
2. `self-review.md` in the artefact directory: one line per checklist item
   below, each `yes` with `path:line` evidence or `n/a` with the reason; then
   a Checks section listing each command run, its exit status and the log path.
3. The handoff result in the shape [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, with `changed_files` covering every file you touched.
4. On revision, `change-log.md` in the artefact directory answering every code
   review finding by ID: what changed, with `path:line`, or why nothing did.

Keep the handoff result within the [handoff caps](../worker-handoffs.md#handoff-caps):
at most 6 KiB, a `summary` of at most 1,200 bytes, and each finding one line of
at most 400 bytes with its ID, severity and source `evidence`. Put the full
reasoning in a report file of at most 512 KiB that the result's `report` names.

## Quality bar

The change is the smallest one that satisfies every requirement ID in the
brief. Before handing off, answer each item in `self-review.md`:

- **Reuse before writing.** You searched for an existing block that does the
  job and reused or extended it; each new block names why nothing fitted.
- **Building blocks.** Decisions are separate from I/O; dependencies arrive as
  parameters or constructor arguments, never globals or `init`; each new block
  is testable through its own interface
  ([building blocks](../../../codebase-design/references/building-blocks.md)).
- **Parse, don't validate.** Input is checked once at the boundary and carried
  on as a narrower type; no later code re-checks what the type guarantees.
- **Explicit error contract.** Every error the contract names is produced
  where it says; callers can distinguish them; no failure is logged and
  dropped, and no retry exhausts and reports success.
- **Cancellation and ownership.** Each goroutine, thread, task, connection,
  file and lock has one owner that stops or closes it; cancellation reaches
  blocking calls; nothing outlives the operation that started it.
- **No speculative abstraction.** No interface, option, plugin point or
  generic parameter that only one caller uses.
- **No unrequested fallback.** No default path, silent downgrade or retry the
  contract did not ask for; an unmet precondition fails visibly.
- **Concurrent writers.** Any read-decide-write on shared state holds a lock
  every writer takes, a database constraint or a serialisable retry.
- **Unrelated changes preserved.** Every change outside your owned paths was
  there before you started and is unchanged.
- **Checks run.** Every check the brief names ran on the final tree; a
  skipped or partial check is reported as such, never as a pass.

## Stopping conditions

Stop and return to the coordinator when:

- a requirement is ambiguous, two requirements conflict, or meeting one needs
  a change outside your owned paths or to the public interface; ask one
  precise question, citing the requirement ID, and keep working on the rest.
  A contract change comes back through the
  [contract amendment](README.md#contract-amendment) procedure as an amended
  contract and brief in a refreshed assignment, never as the test engineer's
  work;
- a check the brief names cannot run on this host; report which and why;
- the change is complete, every checklist item is answered and every check
  passed.

Return the changed file list with hashes, `self-review.md`, the check results
and the open questions. Do not commit, push or open a pull request.
