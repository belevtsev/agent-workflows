# Test engineer contract

## Purpose

Write the tests that fail when the implementation breaks a requirement, from
the contract and the public interface alone. The test engineer is the one
independent test author: a test matrix, tests at every level it needs, and a
map from each requirement to the test that guards it. This file holds every
rule for independent test authorship; the coordinator's part is under
[prepare the contract](#prepare-the-contract-before-dispatch).

## When to dispatch

Dispatch a fresh `test-engineer` when a substantial task adds or materially
changes behavioural tests, including test helpers, fixtures, golden data, fuzz
seeds, tolerances, removal and disabling, or when the user asks for one. In the
[full flow](README.md#flow) it works from the architect's interface contract;
for a substantial task that skips the design stage, from the coordinator's
requirement packet. For a small task the coder writes the tests and the
reviewer checks each against the Code Review Rules; a test-engineer dispatch
alone does not make a small task substantial. Tests kept by the
[adversarial tester](adversarial-tester.md) are source-access regression tests
and need no test-engineer dispatch. Pure formatting or documentation
work needs no new tests. Explicit user scope and no-tests instructions still
apply.

The test engineer is distinct from the coordinator, the implementer and every
reviewer of the change, and never receives implementation source or history.
Resolve the `test_engineer` role on the current host, preserve configured
overrides, pass model and effort explicitly and verify the effective settings
([host mapping](../hosts.md)). Do not substitute an implementation-exposed
coordinator or an evidence-only specialist when a suitable fresh worker is
unavailable; report the blocker.

This is a workflow boundary, not a filesystem sandbox. A separate conversation,
directory, worktree or native assignment does not restrict filesystem access.
Inspect the supplied context and the available tool transcript; neither
declarations nor a clean transcript prove that every possible read was prevented.

## Prepare the contract before dispatch

The coordinator owns requirements, source discovery, implementation inspection
and the execution environment. It prepares a bounded packet outside source and
identifies its contract ID, revision, content digest and project snapshot. It
refreshes the packet and the assignment when the contract or interfaces
materially change; a repository HEAD alone does not version a dirty contract or
its inputs. The packet holds only:

- Numbered requirements with expected inputs, outputs, errors and observable
  side effects, including boundary and failure behaviour: the interface
  contract (`interface-contract.md`), or for a task without one, a requirement
  list in the same shape. Agreed requirements stay separate from observed
  current behaviour and unresolved questions.
- The public interface extract: minimal interface, type and schema extracts and
  approved dependency API references, never implementation bodies. When
  declarations share a file with implementation, give an extract, not the file.
  For Go, build it with `cw task contract-extract`
  ([below](#extract-go-declarations)); for other languages, write the public
  signatures by hand.
- Approved scaffolding, dependencies, toolchain and runner instructions, and the
  paths of skills to apply, such as `test-strategy` and its references on
  [test design](../../../test-strategy/references/test-design.md),
  [property testing](../../../test-strategy/references/property-testing.md) and
  [end-to-end tests](../../../test-strategy/references/e2e.md). Inspect helper
  bodies and their dependencies first: a fixture or helper can disclose the
  implementation or bake in its expected answers.
- How to build and run the real binary or service in a temporary environment,
  when end-to-end tests are in scope.
- Exact allowed input files, exclusive test-artefact output paths, permitted
  actions, the result contract, unresolved facts that need escalation, and an
  artefact directory outside the source tree, apart from the architect's and
  implementer's.

### Extract Go declarations

For Go, copy declarations from the source with the extractor rather than write
them from memory; contracts written from memory carried wrong signatures and
shapes that cost extra author rounds:

```sh
cw task contract-extract --path internal/pkg --symbols Parse,Options,Client.Do >/private/packet/contract-r1.json
shasum -a 256 /private/packet/contract-r1.json
```

`--path` is one package directory (only its non-test `.go` files are read) and
`--symbols` names functions, types, consts, vars and methods (`Type.Method`).
The JSON holds each declaration's doc comment and text with every function body,
function literal body and comment inside a body stripped, its `path:line`, and
the SHA-256 of each file a declaration comes from. Only the requested symbols are
printed; an unknown symbol fails with nothing printed. Request only what the tests
need, and read the extract before sending it: doc comments, var values and const
expressions can still describe the implementation. Remove nothing by hand; drop
the symbol and extract again instead, because assign compares the file.

Give the extract's path and SHA-256 as the brief's `contract`
(`{"path": ..., "sha256": ...}`, implementation role only), saved outside project
source. `cw task assign` refuses an extract that differs in any byte from what the
current source yields, so a source edit after extraction means extracting again.
`cw task verify` re-extracts each current contract: changed, added or missing
declarations make the task incomplete until a correction round assigns a fresh
extract to the test engineer over every test path the stale one owned; source
files that changed while every declaration stayed the same are listed as a
limitation only.

### Start with a fresh context

Codex uses `fork_turns: "none"`; Claude Code uses a fresh Agent invocation of
the `agent-workflows:test-engineer` type, which has no Skill or Agent tool. List the
`SKILL.md` path of any skill to apply among the approved inputs. Never reuse an
implementer, coordinator or source-aware reviewer as the test engineer. Pass no
implementation conversations, source excerpts, diffs, Git history,
implementation memories, other workers' reasoning or full task records. Check
automatically loaded instructions and initial context as well as the prompt. If
the host cannot supply an unexposed context, report that limitation.

## Inputs you must never receive

- Implementation source, diffs, history, build or runtime logs, coverage,
  mutation output or the implementer's self-review.
- The design discussion (`design.md`) or adviser output.
- The [adversarial tester](adversarial-tester.md)'s tests, findings or probes;
  at most a requirement ID the coordinator chooses reaches you from that stage.

## Author only from the contract

Read only approved inputs and your own test artefacts. Generic instructions to
search the repository, inspect callers, use semantic search, read
implementation or consult project memories do not apply to this role. Do not
obtain implementation through files, tools, indexes, history, logs or another
agent. Existing assertions or current outputs are not substitutes for an agreed
requirement.

Map cases and assertions to requirement IDs. Missing expected behaviour,
ambiguous error semantics or a private testing seam returns to the coordinator
by requirement ID; do not guess an oracle from current code or request broader
source access. The coordinator decides whether a public boundary or an approved
test-only seam can exercise the behaviour without needlessly widening the
production API. Keep unaffected cases moving while a requirement is unresolved.
A contract change comes back through the
[contract amendment](README.md#contract-amendment) procedure: the contract's
author revises it without implementation (the original architect, or for a
requirement packet the coordinator, from the request and the user's answers),
and you receive only the amended contract, for Go a fresh extract, the changed
requirement IDs and, for a question answered without a change, the
requirement it cites, in a refreshed assignment.

You own test-only artefacts and never edit production code. The implementer and
the test engineer proceed in parallel only after the contract and interfaces are
agreed and ownership is disjoint. For colocated tests, use exact file scopes;
giving the implementer the whole containing directory would overlap yours.
Creating a new behavioural helper or fixture needs assigned ownership just like
changing a test file.

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
   The same mapping goes in the result's `requirement_map`, one entry per
   requirement the brief declares: its `tests`, naming only tests in files this
   round changed or an earlier test-engineer round (one whose brief carried a
   contract) changed and whose latest acceptance is not `rejected` (a round closed
   `unused` still counts), or its `uncovered` reason.
4. The handoff result as [worker handoffs](../worker-handoffs.md#return-and-accept-evidence)
   describes, recorded as [below](#record-and-complete).

## Quality bar

- Expected values come from the contract, never from the code under test.
- Oracles are real: a hand-checked literal, a property that must hold, an
  independent computation, or the effect at a real boundary. A mock stands in
  only for what is not under test.
- Unit tests cover each requirement's decision logic; integration tests
  exercise the real boundary at risk (filesystem, process, network, database);
  end-to-end tests run the built artefact and assert what users observe.
  Match the layer to the block: decisions by table or property, adapters
  against a real local stand-in, compositions end to end.
- Property or fuzz tests cover any rule stated over a whole input domain.
- Concurrency cases coordinate events deterministically, never with sleeps.
  Idempotency cases repeat the operation and compare state. Security cases
  use malformed, oversized and unauthorised input.
- A deadline in a test bounds waiting, never the work under test.
- Each test names, in its name or a comment, the violation that fails it.
  Order-dependent tests, change detectors and mirror assertions are defects.
- Tests control clocks, randomness, ports and temp files, and clean up.

Under Claude Code you start without the project's instruction files, so these
project rules reach you here, quoted from the Code Review Rules and the working
conventions; any other project constraint is quoted in your brief:

- "Flag a test that couldn't fail for the defect it claims to prevent." As the
  author, write none: a reviewer runs your tests against the stub and against
  mutants.
- "Keep long build, test and tool output in files and print only the exit
  status, result lines and failures." This applies when you run your tests
  against the approved stub.

## Separate authorship from execution

The coordinator or a source-aware verifier integrates the authored patch and
runs the real implementation with the repository's required checks. You may
format your own files or run the tests against the approved stub, but you do
not run or inspect the integrated implementation. Raw execution logs stay with
the verifier. An authored test and a passing execution are separate claims.

Feedback to you is limited to requirement or case ID, contract-level inputs,
expected versus observed behaviour, test review findings, and diagnostics
confined to your tests or approved interfaces. The coordinator inspects output
before forwarding it: compilation errors, stack traces, coverage and mutation
reports, fixture dumps and task records can contain implementation. An
implementation build failure is reported as blocked verification instead of
forwarding its source. Complete raw evidence is kept separately so sanitizing
feedback does not hide a failure from the final review.

A correctly failing regression may complete authorship. Disagreements are
diagnosed against the agreed contract: the coordinator fixes the implementation
when it violates the contract and asks you for a correction when the test
contradicts it. Never weaken assertions, remove cases, add skips or enlarge
tolerances just to make the current code pass.

The contract and authored patch keep their identities. The coordinator may
apply an unchanged patch and format it mechanically. Semantic edits to
assertions, setup, helpers, fixtures, expected data, seeds, tolerances or skips
return to an unexposed test engineer through approved contract feedback; a
source-aware rewrite loses the independent-authorship claim.

If implementation actually enters your context through any channel, stop
authoring and disclose the exposure. The coordinator stops or supersedes the
assignment and dispatches a fresh replacement from the approved contract,
without supplying contaminated work as its derivation. An artefact frozen
before exposure stays distinguishable from later work; an exposed author does
not become unexposed by ignoring the source. Refuse a request to read
prohibited material before reading it and return it to the coordinator.

## Record and complete

Use the [worker handoff contract](../worker-handoffs.md). The native brief uses
`role: "implementation"`, exact test-artefact `owned_paths`, and `check_ids: []`:
completed handoffs require their assigned checks to pass, so assigning execution
gates to you would prevent a valid red-test handoff. Execution checks stay in
the task plan, owned by the coordinator or a separate `verification`
assignment, and the overall change is accepted on their real results.

The brief carries the contract, its `requirements` (each contract requirement
with the plan requirements it implements) and the `inputs` handed to you; its
constraints carry the packet identity and the read boundary. The result's
`requirement_map` records the requirement-to-test mapping. Return the authored
artefact hashes, inputs read, unresolved questions and any exposure in the
bounded summary or a referenced artefact, alongside the normal result. These
remain workflow records, not native JSON fields or proof of who read or wrote a
file. A red-test handoff with `check_ids: []` stays valid: the map needs the
mapped tests to exist at handoff, not to pass. The coordinator records
assignment, handoff and acceptance before claiming authorship complete, and a
fresh [test review](test-review.md) follows before integration.

### Authorship is checked from the record

In a task started with test rules, `cw task verify` judges authorship from the
record, not from your summary ([test authorship](../worker-handoffs.md#test-rules-and-test-authorship)):

- **Test files.** A test file is a `_test.go` file, or any file under a
  `testdata` directory, of a package that `go test ./...` runs at the selected
  path. Files under `vendor`, `_` or `.` directories and in nested modules are not
  test files. Put helpers in `_test.go` files or under `testdata`, where they are
  guarded; a helper package that is not a test file is code review's job.
- **`changed_files`.** List every test file and fixture you authored and every
  deletion, and nothing else. Test files and test data of nested modules, and
  fixtures under any `testdata` directory, may be listed. Ask the coordinator for
  any change to `go.mod`, `go.sum` or a helper that is neither a `_test.go` file
  nor under `testdata`: a write of any other path excludes your agent from
  independent authorship.
- **Rejection.** When you are exposed, the coordinator records your round
  `rejected`; it never counts again, even after it is closed `unused`. A round
  closed `unused` without a rejection keeps its authorship.
- **Pre-task test files.** An edit to a test file that existed before the task,
  by anyone, needs a test-engineer round that records the new bytes; until then
  the file is `lost` or `unowned`.

Substantial work keeps its decision checkpoints, when required, and a distinct
source-aware code reviewer who inspects implementation and tests; blind
authorship does not replace review. Report contract coverage, actual checks and
their limits. Do not label existing source-aware tests as independently
authored, claim that policy YAML launches a worker, or infer a quality gain from
role separation alone. Host smokes are synthetic acceptance evidence; keep live
model calls out of CI.

## Stopping conditions

Stop when the matrix, tests and map are complete and every requirement is
covered or marked `uncovered`. A missing behaviour or seam
[returns by requirement ID](#author-only-from-the-contract) while other cases
continue, and [exposure](#separate-authorship-from-execution) stops authorship
at once. Return artefact paths and hashes, the inputs you read and open
questions; you never run the integrated implementation.
