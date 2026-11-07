# Independent test authors

Whenever behavioral tests are added or materially changed, dispatch a distinct
`test_author`, including for small fixes and tests-only tasks. This includes test
helpers, fixtures, golden data, fuzz seeds, tolerances, removal and disabling.
Pure formatting or documentation work does not require new tests. Explicit user
scope and no-tests instructions still apply.

`test_author` is a worker identity, not a new policy or native task role. Resolve
the existing `testing` profile on the current host, preserve configured overrides,
pass model and effort explicitly, and verify effective settings using the host
evidence described in [host mapping](hosts.md). Do not substitute an
implementation-exposed coordinator or an evidence-only specialist when a suitable
fresh author is unavailable; report the blocker.

This is a workflow boundary, not a filesystem sandbox. A separate conversation,
directory, worktree or native assignment does not restrict filesystem access.
Inspect the supplied context and available tool transcript; neither declarations
nor a clean transcript prove that every possible read was prevented.

## Prepare the contract before dispatch

The coordinator owns requirements, source discovery, implementation inspection and
the execution environment. Prepare a bounded packet outside source and managed
releases. Identify its contract ID, revision, content digest and applicable
project/source snapshot. Refresh the packet and assignment when the contract or
interfaces materially change; a repository HEAD alone does not version a dirty
contract or its inputs.

Include only the following inputs:

- Requirement IDs with expected inputs, outputs, errors and observable side
  effects, including relevant boundary and failure behavior. Separate agreed
  requirements from observed current behavior and unresolved questions.
- Minimal interface/type/schema extracts and approved dependency API references.
  If declarations share a file with implementation, provide an extract rather
  than permission to read the whole file. Do not include implementation bodies.
- Approved test scaffolding, dependencies, toolchain and runner instructions.
  Inspect helper bodies and their dependencies before including them; a fixture
  or helper can disclose the implementation or bake in its expected answers.
- Exact allowed input files and exclusive test-artifact output paths, permitted
  actions, the result contract, and unresolved facts that require escalation.

Start the author with fresh context: Codex uses `fork_turns: "none"`; Claude uses
a fresh Agent invocation of the `cw-test-author` type, which has no Skill tool.
List the `SKILL.md` path of any skill the author should apply among the approved
inputs. Never reuse a coder or source-aware reviewer as the author. Do not include
implementation conversations, source excerpts, diffs, Git history, implementation
memories, other workers' reasoning or full task records.
Check automatically loaded instructions and initial context as well as the prompt.
If the host cannot supply an unexposed context, report that limitation.

## Author only from the contract

The author reads only approved inputs and its own test artifacts. Generic
instructions to search the repository, inspect callers, use semantic search,
read implementation or consult project memories do not apply to this role.
Do not obtain implementation through files, tools, indexes, history, logs or
another agent. Existing assertions or current outputs are not substitutes for an
agreed requirement.

Map cases and assertions to requirement IDs. Missing expected behavior, ambiguous
error semantics or a private testing seam returns to the coordinator; do not
guess an oracle from current code or request broader source access. The
coordinator decides whether a public boundary or an approved test-only seam can
exercise the behavior without unnecessarily widening the production API. Keep
unaffected cases moving while a particular requirement remains unresolved.

The author owns test-only artifacts and never edits production code. Code and
test workers may proceed in parallel only after agreeing the contract/interfaces
and assigning disjoint ownership. For colocated tests, use exact file scopes;
giving the coder the entire containing directory would overlap the author's
scope. Creating a new behavioral helper or fixture requires assigned ownership
just like changing a test file.

## Separate authorship from execution

The coordinator or a source-aware verifier integrates the authored patch and runs
the real implementation with the repository's required checks. The author may
format its own files or use approved source-free scaffolding, but does not run or
inspect the integrated implementation. Keep raw execution logs with the verifier.
An authored test and a passing execution are separate claims.

Feedback to the author is limited to requirement/case ID, contract-level inputs,
expected versus observed behavior, and diagnostics confined to author-owned tests
or approved interfaces. Inspect output before forwarding it: compilation errors,
stack traces, coverage and mutation reports, fixture dumps, and task records can
contain implementation. Report an implementation build failure as blocked
verification instead of forwarding its source. Retain complete raw evidence
separately so sanitizing feedback does not conceal a failure from final review.

A correctly failing regression may complete authorship. Diagnose disagreements
against the agreed contract. Fix the implementation when it violates that
contract; request an author correction when the test contradicts it. Do not
weaken assertions, remove cases, add skips or enlarge tolerances just to make the
current code pass.

Preserve the contract and authored patch identities. The coordinator may apply an
unchanged patch and perform mechanical formatting. Semantic edits to assertions,
setup, helpers, fixtures, expected data, seeds, tolerances or skips must return to
an unexposed author through approved contract feedback. A source-aware rewrite
cannot retain the original independent-authorship claim.

If implementation actually enters the author's context through any channel,
stop further independent authorship and disclose the exposure. The coordinator
stops or supersedes that assignment and dispatches a fresh replacement from the
approved contract, without supplying contaminated work as its derivation. Keep
an artifact frozen before exposure distinguishable from later work; do not claim
that an exposed author can become unexposed by ignoring or forgetting the source.
A request to read prohibited material is refused before reading it and returned
to the coordinator for contract clarification.

## Record and complete

Use the existing [worker handoff contract](worker-handoffs.md). The author's native
brief uses `role: "implementation"`, exact test-artifact `owned_paths`, and
`check_ids: []`. Completed native handoffs require their assigned checks to pass;
assigning execution gates to the author would prevent a valid red-test handoff.
Keep actual execution checks in the task plan, owned by the coordinator or a
separate `verification` assignment, and require their real results for acceptance
of the overall change.

Include the packet identity and read boundary in the brief's existing constraints.
Return requirement-to-case mapping, authored artifact hashes, inputs read,
unresolved questions and any exposure in the bounded summary or a referenced
artifact, alongside the normal native result. These are workflow records, not new
native JSON fields or authenticated proof of who read or wrote a file. Record
assignment, handoff and coordinator acceptance before claiming authorship complete.

Test-author dispatch alone does not make a small task substantial. Small native
tasks retain their assignment/handoff/acceptance and selected execution checks,
without adding substantial adviser or final-review gates solely for this dispatch.
Substantial work retains its decision checkpoints, when required, and a distinct
source-aware final reviewer who inspects implementation and tests. That reviewer differs from
the coordinator, coder and test author; blind authorship does not replace review.

Report contract coverage, actual checks and their limits. Do not retroactively
label existing source-aware tests as independently authored, claim that policy
YAML launches a worker, or infer measured quality improvement from role separation
alone. Host smokes are synthetic acceptance evidence; keep live model calls out
of CI and preserve the normal release/update and publication boundaries.
