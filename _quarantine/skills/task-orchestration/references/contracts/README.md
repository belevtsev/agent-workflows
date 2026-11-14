# Role contracts

Each file is one worker's whole assignment. A worker reads only the contract
its brief names; give it the inputs that contract lists.

| Agent | Contract |
| --- | --- |
| `spec-writer` | [spec-writer.md](spec-writer.md) |
| `architect` | [architect.md](architect.md) |
| `implementer` | [implementer.md](implementer.md) |
| `test-engineer` | [test-engineer.md](test-engineer.md) |
| `reviewer` | [spec-review.md](spec-review.md), [design-review.md](design-review.md), [test-review.md](test-review.md), [code-review.md](code-review.md) |
| `adversarial-tester` (opt-in) | [adversarial-tester.md](adversarial-tester.md) |
| `github-worker` (fallback to `cw publish`) | [github-worker-fallback.md](../github-worker-fallback.md) |

## Flow

The plan's `profile` fixes the stages, round caps and budget
([profiles](../../../../docs/operations.md#profiles)). Each review stage's
findings go back to that stage's author, within the
[review cap](../worker-handoffs.md#review-cap) and the
[round budget](../worker-handoffs.md#round-budget). When the task binds
advisers, consult them only at the
[adviser points](../adviser-providers.md#adviser-points-in-the-flow) a to d.

### `lite`, the default

1. **Contract.** Write the requirement packet
   ([prepare it](../dispatching-reviews-and-tests.md#prepare-the-test-contract));
   under `standard` and `strict` fix the architect's `interface-contract.md`
   instead. Extract the public interface, hash both before dispatch and keep
   the [source record](../contract-amendment.md#before-dispatch).
2. **Implementer ∥ test engineer, isolated**: the same contract, disjoint
   owned paths, fresh contexts
   ([dispatch](../dispatching-reviews-and-tests.md#dispatch-a-test-engineer)).
3. **Test engineer ⇄ reviewer (tests)**, under `test-review.md`. Start the
   review once the test handoff is recorded, beside the implementer's work;
   finish it before integration.
4. **Integrate and run.** Apply both results unchanged and run the tests;
   settle a failure
   [against the contract](../dispatching-reviews-and-tests.md#feedback-and-exposure).
5. **Reviewer (code) ⇄ implementer.** A further reviewer reviews the
   integrated source under `code-review.md`, from the
   [brief and bundle](../worker-handoffs.md#review-substantial-changes);
   [route its findings](../dispatching-reviews-and-tests.md#route-code-review-findings).
   When the plan opted in at classify, dispatch a fresh
   [adversarial tester](../dispatching-reviews-and-tests.md#adversarial) on the
   same source beside this review.
6. **Final checkpoint**, when required, then **`cw task verify`**.
7. **Publish** as the [GitHub worker reference](../github-worker.md) says.

### `standard`: adds the design stage

Use it for an open design choice, a public interface change, or when the user
asks. Before step 1, **architect ⇄ reviewer (design)**: the architect writes
its three artefacts from the request or the reviewed change folder, and an
independent reviewer checks them under `design-review.md`. Show the user a
short summary of the design: the choice, the options it rejected and the
interface change. Handle the architect's open questions as the spec stage
below handles the spec's. When the repository keeps ADRs (`docs/decisions/`
or another ADR folder), name that folder among the architect's owned paths
and commit its ADR with the change.

### `strict`: adds the spec stage

Use it when the user asks, or the change touches security, data integrity or
concurrency. Before the design stage, **spec writer ⇄ reviewer (spec)**: the
spec writer turns the sources into an OpenSpec change folder, and a reviewer
checks it against the sources under `spec-review.md`. While that review runs,
ask the user the spec's open questions. With advisers bound, a question the
spec leaves to a choice waits for point a, after the review, and then goes to
the user with their input. The answers reach the spec writer with the
findings and the reviewer only in the follow-up review; a revision that
answers them gets that follow-up even without a `blocker` or `major`.

Skip the spec stage for a bug fix, a refactor or a test-only task; when a
reviewed spec exists (an OpenSpec change that passed review, or current specs
that cover the request); or when the request is its own spec: every
behaviour, input, output and failure stated with its values, nothing left to
settle. The architect then works from that spec or the request, and the
design review still runs.

A task that is not
[substantial](../../../../global/AGENTS.md#substantial-tasks) runs none of
these stages.

## Contract amendment

At any step, a question or finding that needs a contract change follows the
[amendment procedure](../contract-amendment.md): the interface contract
changes only through its author.
