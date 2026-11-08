# Role contracts

Each file is the whole assignment for one worker role or review stage. A worker
reads only the contract the brief names, so each role's output is independent
evidence.

| Role | Agent | Contract | Reads | Writes |
| --- | --- | --- | --- | --- |
| Spec writer | `spec-writer` | [spec-writer.md](spec-writer.md) | sources, repository, review findings | OpenSpec change folder, `change-log.md` |
| Spec review | `reviewer` | [spec-review.md](spec-review.md) | change folder, sources, repository | `spec-review.md` |
| Architect | `architect` | [architect.md](architect.md) | request or reviewed change folder, repository | `design.md`, `interface-contract.md`, `implementation-brief.md`, an ADR when the repository keeps them |
| Design review | `reviewer` | [design-review.md](design-review.md) | the architect's artefacts, spec, sources, repository | `design-review.md` |
| Implementer | `implementer` | [implementer.md](implementer.md) | brief, interface contract, repository | the change, `self-review.md` |
| Test engineer | `test-engineer` | [test-engineer.md](test-engineer.md) | interface contract, interface extract | tests, `test-matrix.md`, `requirement-map.md` |
| Test review | `reviewer` | [test-review.md](test-review.md) | contract, extract, tests, stub | `test-review.md` |
| Code review | `reviewer` | [code-review.md](code-review.md) | resulting source, reviewer bundle | findings |
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
   against the sources under [spec review](spec-review.md). The coordinator
   settles open questions with the user, consulting advisers on any open
   question the spec leaves to a choice (point a).
2. **Architect ⇄ reviewer (design).** The architect writes the three artefacts
   from the reviewed change folder. When it chooses between real options, the
   coordinator consults advisers before the design review (point b). The
   reviewer, never the architect, checks the decision record and the interface
   contract under [design review](design-review.md); the coordinator settles
   open questions with the user. When the target repository keeps ADRs
   (`docs/decisions/` or an existing ADR folder), the brief names that folder
   among the architect's owned paths; the architect writes an ADR there only
   for a choice that is costly to reverse
   ([architect](architect.md#required-outputs), item 6), the design review
   checks it against `design.md`, and it is committed with the change. A
   repository without such a folder gets no ADR.
3. **Contract.** The coordinator fixes `interface-contract.md`, extracts the
   public interface and hashes both before dispatch.
4. **Implementer ∥ test engineer, isolated**: same contract, disjoint owned
   paths, fresh contexts. The implementer never sees `design.md`; the test
   engineer never sees the implementation. The spec's scenarios reach the test
   engineer as requirements through the interface contract
   ([architect](architect.md#inputs-you-receive)).
5. **Test engineer ⇄ reviewer (tests)**, before integration. A reviewer that is
   not the test engineer and sees no implementation checks the matrix and tests
   against the contract under [test review](test-review.md).
6. **Integrate and run.** Apply both results unchanged and run the tests. Fix
   the implementation when it violates a requirement; return a test that
   contradicts the contract to an unexposed test engineer.
7. **Reviewer (code) ⇄ implementer.** A further reviewer reviews the integrated
   source against the requirements and the bundle under
   [code review](code-review.md); the implementer fixes accepted findings, and
   a finding about a test goes to a fresh test engineer.
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

Artefacts and records stay outside the source tree; the exceptions are the
change folder, under `openspec/changes/`, and an ADR (step 2). Model and effort come from the
policy roles `spec_writer`, `spec_review`, `architect`, `design_review`,
`implementer`, `test_engineer`, `test_review`, `review` and `review_followup`
([hosts](../hosts.md)).
