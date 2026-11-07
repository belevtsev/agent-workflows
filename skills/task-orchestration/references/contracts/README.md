# Role contracts

Each file is the whole assignment for one worker role. A worker reads only the
contract the brief names, so each role's output is independent evidence.

| Role | Contract | Reads | Writes |
| --- | --- | --- | --- |
| Architect | [architect.md](architect.md) | request, repository | `design.md`, `interface-contract.md`, `implementation-brief.md` |
| Implementer | [implementer.md](implementer.md) | brief, interface contract, repository | the change, `self-review.md` |
| Test engineer | [test-engineer.md](test-engineer.md) | interface contract, interface extract | tests, `test-matrix.md`, `requirement-map.md` |
| Reviewer | [worker-handoffs.md](../worker-handoffs.md#review-substantial-changes) | resulting source, reviewer bundle | findings |
| GitHub worker | [github-worker.md](../github-worker.md) | verified facts, carried authorization | commit, push, pull request |

## Flow

1. **Architect** writes the three artefacts; the coordinator settles open
   questions with the user.
2. **Contract.** The coordinator fixes `interface-contract.md`, extracts the
   public interface and hashes both before dispatch.
3. **Implementer and test engineer in parallel, isolated**: same contract,
   disjoint owned paths, fresh contexts. The implementer never sees
   `design.md`; the test engineer never sees the implementation.
4. **Integrate and run the matrix.** Apply both results unchanged and run the
   tests. Fix the implementation when it violates a requirement; return a test
   that contradicts the contract to an unexposed test engineer.
5. **Reviewer**, a fourth agent, reviews the integrated source against the
   requirements and the bundle.
6. **`cw task verify`** checks that every assignment, handoff, acceptance,
   check run and review is recorded and current.
7. **Commit** through the GitHub worker with the user's authorization.

Artefacts and records stay outside the source tree. Model and effort come from
the policy roles `architect`, `implementer` and `test_engineer` ([hosts](../hosts.md)).
