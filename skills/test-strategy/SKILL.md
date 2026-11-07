---
name: test-strategy
description: Choose tests for changed behavior, including Go fuzzing, with meaningful oracles and verification gates.
---

# Testing changed behavior

Identify the behavior that could fail, the invariant to protect, and the observable signal of failure. The source-aware coordinator inspects the existing test harness and required repository checks before choosing a test technique. Use repository commands and environments as the authority; a skill does not waive CI gates or an explicit no-tests instruction.

New or meaningfully changed behavioral tests require a separate fresh `test_author`, including small and test-only tasks, helpers, fixtures, goldens, fuzz seeds, and removal or disabling of tests. Use the existing testing profile and follow the canonical [test-author protocol](../task-orchestration/references/test-author.md). Small author dispatch alone does not trigger substantial-task adviser or review checkpoints; it still needs the small-task assignment, handoff, and acceptance record.

The coordinator supplies requirement IDs, source/version identity, approved interface/type extracts and behavior contracts, approved scaffolding/dependencies, and an exact file allowlist with test-file ownership. Authors derive expectations from that packet, never production source, implementation conversations, diffs/history/memory, broad searches, hidden implementation-derived assertions, or raw traces. Generic discovery and source-reading advice yields to these restrictions. Ask the coordinator about missing behavior or private seams. Parallel implementation and test work starts only after interface agreement and with disjoint exact scopes.

The author writes only tests and test-only artifacts. The source-aware verifier runs integrated checks and returns only requirement/case IDs, contract inputs, expected/actual outcomes, and test or approved-interface diagnostics. Raw build/runtime logs, coverage, and mutation output stay with the verifier. The coordinator may integrate the unchanged authored patch and mechanically format it; semantic assertions, fixtures, tolerances, skips, and seeds return to an unexposed author. Preserve patch identity. Exposure through files, tools, injected context, scaffolding, or messages stops independent authorship: replace the author without using contaminated tests as derivation, or report the blocker. This process is not a sandbox or proof of test quality, and substantial work still requires separate independent source-aware final review. Do not relabel existing tests as independently authored.

Prefer the smallest test that exercises the real boundary at risk:

| Behavior at risk | Useful evidence |
| --- | --- |
| Local decision, validation, or error mapping | Focused examples including relevant negative and boundary inputs |
| Concurrency, cancellation, shutdown, or ownership | Deterministic event coordination; race or sanitizer checks where supported |
| Filesystem, process, network, or database semantics | Integration coverage of the actual boundary, including partial failure and cleanup |
| Parser, codec, normalization, or state invariant | Property tests or fuzzing with a clear oracle and minimized reproducible failures |
| API or generated contract compatibility | Producer/consumer and representative serialization checks under supported versions |
| Claimed performance improvement | Comparable before/after workloads, repeated samples, profiling, and a stated metric |

For tests that run the built binary or service, read [end-to-end tests](references/e2e.md).
For a Go fuzz target or bounded campaign, read [Go fuzzing](references/go-fuzzing.md)
for input domains, meaningful oracles, seed evidence, isolation, and exact failure
reproduction. Other tasks need not load that reference.

Before writing a test, name the contract violation that should make it fail. If you can't name one, redesign the test around observable behavior. The verifier can also name a concrete production change that should cause that violation. Read [test design](references/test-design.md) when writing tests or adding fakes. When a rule covers a whole input domain, or no exact expected output exists, read [property and metamorphic testing](references/property-testing.md).

For consequential verification decisions, or an explicit user request, the coordinator uses configured advisers via `cw consult` if any are configured, alongside applicable orchestration checkpoints; otherwise it proceeds on source evidence under the same [decision rules](../task-orchestration/SKILL.md#adjudicate). Give the coordinator contract-based candidate scenarios and assertions; it owns calls, batching, and reuse across skills. Routine work needs no separate consultation. Advisers supply typed advice, not generated conclusions, proof, execution results, or authorization. The coordinator independently verifies material claims, records unavailable or inconclusive coverage, and retains source-aware decisions. Advice cannot expand author inputs, waive no-tests scope, or authorize execution.

The source-aware verifier makes a regression test fail on the original defect when practical and confirms any temporary production mutation actually landed. A correctly failing authored test can hand off before the implementation passes; the coordinator/verifier owns real plan checks. Route semantic test or fixture changes back through the author. If no approved seam can exercise the real bug, report that as a finding rather than adding a shallow test that gives false confidence. Assert outcomes rather than private implementation steps. Control clocks, randomness, events, ports, and temporary resources; avoid sleeps as synchronization. Confirm cancellation and cleanup rather than leaving background work alive. Use real implementations when replacing them with a mock would remove the behavior being tested.

Coverage measures executed code, not defect detection. Use mutation testing only when it can resolve a real doubt about important assertions and the tool can run in isolation, and read [mutation testing](references/mutation-testing.md) first, because a careless run can report a false pass. Large test campaigns, new fixtures, and new dependencies need a concrete testing benefit. Reversible prose, formatting, and low-impact edits rarely need new tests that mirror the edit.

The source-aware verifier runs focused integrated checks while iterating and the required broader gates for completion, keeping author feedback within the protocol. After the relevant final state passes, repeat only for a new change, failure, or unresolved concern. Do not substitute a host run for a required container or target platform. A blocked, skipped, or partial check is not a pass.

Report what ran, which behavior it proves, and important limits. Keep local unit/fixture evidence, integration results, CI, deployment, and statistical performance evidence distinct.
