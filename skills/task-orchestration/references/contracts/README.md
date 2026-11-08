# Role contracts

Each file is the whole assignment for one worker role. A worker reads only the
contract the brief names, so each role's output is independent evidence.

| Role | Contract | Reads | Writes |
| --- | --- | --- | --- |
| Spec writer | [spec-writer.md](spec-writer.md) | sources, repository, review findings | OpenSpec change folder, `change-log.md` |
| Spec reviewer | [spec-reviewer.md](spec-reviewer.md) | change folder, sources, repository | `spec-review.md` |
| Architect | [architect.md](architect.md) | request or reviewed change folder, repository | `design.md`, `interface-contract.md`, `implementation-brief.md` |
| Implementer | [implementer.md](implementer.md) | brief, interface contract, repository | the change, `self-review.md` |
| Test engineer | [test-engineer.md](test-engineer.md) | interface contract, interface extract | tests, `test-matrix.md`, `requirement-map.md` |
| Reviewer | [worker-handoffs.md](../worker-handoffs.md#review-substantial-changes) | resulting source, reviewer bundle | findings |
| GitHub worker | [github-worker.md](../github-worker.md) | verified facts, carried authorization | commit, push, pull request |

## Flow

1. **Spec writer** turns the source documents into an OpenSpec change folder.
2. **Spec reviewer** grades findings against the sources. The coordinator
   settles open questions with the user; the writer revises against findings
   and answers, in rounds [its contract](spec-reviewer.md#required-outputs) bounds.
   The coordinator closes each superseded round's writer and reviewer
   assignments as `unused`, with dispositions, before `cw task verify`.
3. **Architect** writes the three artefacts from the reviewed change folder;
   the coordinator settles open questions with the user.
4. **Contract.** The coordinator fixes `interface-contract.md`, extracts the
   public interface and hashes both before dispatch.
5. **Implementer and test engineer in parallel, isolated**: same contract,
   disjoint owned paths, fresh contexts. The implementer never sees
   `design.md`; the test engineer never sees the implementation. The spec's
   scenarios reach the test engineer as requirements through the interface
   contract ([architect](architect.md#inputs-you-receive)).
6. **Integrate and run the matrix.** Apply both results unchanged and run the
   tests. Fix the implementation when it violates a requirement; return a test
   that contradicts the contract to an unexposed test engineer.
7. **Reviewer**, a further agent, reviews the integrated source against the
   requirements and the bundle.
8. **`cw task verify`** checks that every assignment, handoff, acceptance,
   check run and review is recorded and current.
9. **Commit** through the GitHub worker with the user's authorization.

Skip steps 1 and 2 when the task already has a reviewed spec (an OpenSpec
change that passed review, or current specs that cover the request), or when
the request is precise enough to be its own spec: every behaviour, input,
output and failure stated with its values, and nothing left to settle. The
architect then works from that spec, or from the request when there is none.

Artefacts and records stay outside the source tree; the change folder is the
one exception, under `openspec/changes/`. Model and effort come from the
policy roles `spec_writer`, `spec_reviewer`, `architect`, `implementer` and
`test_engineer` ([hosts](../hosts.md)).
