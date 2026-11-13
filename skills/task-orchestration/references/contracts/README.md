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
| Adversarial tester (opt-in) | `adversarial-tester` | [adversarial-tester.md](adversarial-tester.md) | contract, `design.md`, integrated source, coverage profile | failing tests, `adversarial-findings.md` |
| Code review | `reviewer` | [code-review.md](code-review.md) | resulting source, reviewer bundle, interface contract, `design.md`, test matrix and requirement map, coverage profile | findings, a failing test per behaviour finding |
| GitHub worker (fallback to `cw publish`) | `github-worker` | [github-worker.md](../github-worker.md) | verified facts, carried authorization | commit, push, pull request |

## Flow

`cw task start` fixes the profile, whose stages, round caps and budget `cw`
holds ([profiles](../../../../docs/operations.md#profiles)). Every stage that
produces an artefact has a review loop: findings go back to that stage's
author, who revises and answers each one. The
[review cap](../worker-handoffs.md#review-stages) bounds each stage's reviews;
the round budget in the same section bounds spec, design, contract and
test-engineer rounds across the task, never implementer fixes. After the
before checkpoint, when checkpoints are required, advisers join only at the
[adviser points](../consultation-helper.md#adviser-points-in-the-flow) a to d
below, when available; without them nothing changes.

### `lite`, the default

Four agents: implementer, test engineer, reviewer (tests), reviewer (code).

1. **Contract.** The coordinator writes the requirement packet
   ([test engineer](test-engineer.md#prepare-the-contract-before-dispatch)),
   or under `standard` and `strict` fixes the architect's
   `interface-contract.md`; it extracts the public interface and hashes both
   before dispatch, and keeps the source record a later
   [amendment](../contract-amendment.md#before-dispatch) needs.
2. **Implementer ∥ test engineer, isolated**: same contract, disjoint owned
   paths, fresh contexts. The implementer never sees `design.md`; the test
   engineer never sees the implementation, and gets the spec's scenarios only
   as requirements in the contract
   ([architect](architect.md#inputs-you-receive)). A question either returns
   by requirement ID that needs a contract change follows the
   [contract amendment](#contract-amendment).
3. **Test engineer ⇄ reviewer (tests).** A reviewer that
   is not the test engineer and sees no implementation checks the matrix and
   tests against the contract under [test review](test-review.md). It starts
   once the test handoff is recorded, overlaps the implementer's work and
   finishes before integration. Red-first runs against the stub; optional
   mutant results reach it only if the coordinator ran them before the loop
   closes.
4. **Integrate and run.** Apply both results unchanged and run the tests. Fix
   the implementation when it violates a requirement; return a test that
   contradicts the contract to an unexposed test engineer.
5. **Reviewer (code) ⇄ implementer.** A further reviewer reviews the
   integrated source against the requirements, the contract and `design.md`,
   with the bundle and the final check's coverage profile, under
   [code review](code-review.md). The implementer fixes accepted findings; a
   finding about a test goes to a fresh test engineer by requirement ID,
   contract inputs and expected outcome only. A finding only `design.md`
   supports reaches the implementer as a constraint in contract terms; a rule
   the contract should carry goes through the amendment. Before rejecting a
   `blocker` or `major` finding at any stage, consult advisers (point c). When
   the plan opted in at classify, a fresh
   [adversarial tester](adversarial-tester.md) runs on the same source beside
   this review; each test it keeps fails there and is a finding for the
   implementer.
6. **Final checkpoint** (point d, when required), then **`cw task verify`**:
   every assignment, handoff, acceptance, check run and review is recorded
   and current.
7. **Commit** with `cw publish` under the user's authorization, which the
   coordinator carries; the GitHub worker is the
   [fallback](../github-worker.md#fallback-the-github-worker).

### `standard`: adds the design stage

Use it for an open design choice, a public interface change, or when the user
asks. Before step 1, **architect ⇄ reviewer (design)**: the architect writes
the three artefacts from the request or the reviewed change folder. When it
chooses between real options, consult advisers before the design review
(point b). The reviewer, never the architect, checks the decision record and
the contract under [design review](design-review.md). Show the user a short
summary of the design: the choice, the options it rejected and the interface
change. Ask the architect's open questions as the spec stage below says
([inputs](design-review.md#inputs-you-receive)). When the repository keeps ADRs
(`docs/decisions/` or another ADR folder), the brief names that folder
among the architect's owned paths; the architect writes an ADR there only for
a choice costly to reverse ([architect](architect.md#required-outputs), item
6), checked by the design review against `design.md` and committed with the
change.

### `strict`: adds the spec stage

Use it when the user asks, or the change touches security, data integrity or
concurrency. Before the design stage, **spec writer ⇄ reviewer (spec)**: the
spec writer turns the sources into an OpenSpec change folder; the reviewer
grades findings against the sources under [spec review](spec-review.md).
While that review runs, ask the user the spec's open questions. When advisers
are bound, a question the spec leaves to a choice waits for them at point a,
after the review, and then goes to the user with their input. The answers
reach the spec writer with the findings, and the reviewer only in the
follow-up review, listed with the dispositions, and through the folder's diff
([inputs](spec-review.md#inputs-you-receive)); a revision that answers them
gets that follow-up even without a `blocker` or `major`.

Skip the spec stage for a bug fix, a refactor or a test-only task; when a
reviewed spec exists (an OpenSpec change that passed review, or current specs
that cover the request); or when the request is its own spec: every
behaviour, input, output and failure stated with its values, nothing left to
settle. The architect then works from that spec or the request, and the
design review still runs.

A task that is not
[substantial](../../../../global/AGENTS.md#substantial-tasks) runs none of
these stages. Artefacts and records stay outside the source tree, except the
change folder under `openspec/changes/` and an ADR. Model and effort come
from each role in the policy ([hosts](../hosts.md)).

## Contract amendment

The interface contract changes only through its author, by the
[amendment procedure](../contract-amendment.md).
