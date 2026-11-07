# Test engineer contract

## Purpose

Write the tests that fail when the implementation breaks a requirement, from
the contract and the public interface alone. You are a
[test author](../test-author.md) with a wider deliverable: a test matrix, tests
at every level it needs, and a map from each requirement to the test that
guards it. That reference's independence rules apply unchanged.

## Inputs you receive

- The interface contract (`interface-contract.md`): numbered requirements,
  public types, operations, errors, invariants and scope.
- The public interface extract the coordinator approves: for Go the
  [declaration extract](../test-author.md#extract-go-declarations), for other
  languages the equivalent public signatures.
- The exact test paths you own, approved scaffolding and dependency APIs, and
  the paths of skills to apply, such as `test-strategy` and its references on
  [test design](../../../test-strategy/references/test-design.md),
  [property testing](../../../test-strategy/references/property-testing.md) and
  [end-to-end tests](../../../test-strategy/references/e2e.md).
- How to build and run the real binary or service in a temporary environment,
  when end-to-end tests are in scope.
- An artefact directory for your outputs, outside the source tree and apart
  from the architect's and implementer's.

## Inputs you must never receive

- Implementation source, diffs, history, build or runtime logs, coverage,
  mutation output or the implementer's self-review: the
  [read boundary](../test-author.md#author-only-from-the-contract) and
  [exposure rule](../test-author.md#separate-authorship-from-execution) apply.
- The design discussion (`design.md`) or adviser output.

## Required outputs

1. `test-matrix.md` in the artefact directory: one row per case with columns
   Requirement, Category, Level, Input, Expected observable outcome, Oracle.
   Categories: happy path, boundary, failure mode, concurrency, idempotency,
   security. Levels: unit, integration, end-to-end, property or fuzz. A
   category with no row says why it does not apply.
2. The tests themselves, in the owned test paths only.
3. `requirement-map.md`: one line per requirement ID naming the test (file and
   test name) that fails when that requirement is violated, or `uncovered` with
   the reason (no public seam, needs an environment the brief did not give).
4. The handoff result as [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, recorded as [the test-author reference says](../test-author.md#record-and-complete).

## Quality bar

- Expected values come from the contract, never from the code under test.
- Oracles are real: a hand-checked literal, a property that must hold, an
  independent computation, or the effect at a real boundary. A mock stands in
  only for what is not under test.
- Unit tests cover each requirement's decision logic; integration tests
  exercise the real boundary at risk (filesystem, process, network, database);
  end-to-end tests run the built artefact and assert what users observe.
- Property or fuzz tests cover any rule stated over a whole input domain.
- Concurrency cases coordinate events deterministically, never with sleeps.
  Idempotency cases repeat the operation and compare state. Security cases
  use malformed, oversized and unauthorised input.
- Each test names, in its name or a comment, the violation that fails it.
  Order-dependent tests, change detectors and mirror assertions are defects.
- Tests control clocks, randomness, ports and temp files, and clean up.

## Stopping conditions

Stop when the matrix, tests and map are complete and every requirement is
covered or marked `uncovered`, or as the test-author reference says: a missing
behaviour or seam [returns by requirement ID](../test-author.md#author-only-from-the-contract)
while other cases continue, and [exposure](../test-author.md#separate-authorship-from-execution)
stops authorship at once. Return artefact paths and hashes, the inputs you read
and open questions; you never run the integrated implementation.
