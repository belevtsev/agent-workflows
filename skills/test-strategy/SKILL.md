---
name: test-strategy
description: Choose tests for changed behavior, including Go fuzzing, with meaningful oracles and repository verification gates.
---

# Testing changed behavior

Identify the behavior that could fail, the invariant to protect, and the observable signal of failure. Inspect the existing test harness and required repository checks before choosing a test technique. Use repository commands and environments as the authority; a skill does not waive CI gates or an explicit no-tests instruction.

Prefer the smallest test that exercises the real boundary at risk:

| Behavior at risk | Useful evidence |
| --- | --- |
| Local decision, validation, or error mapping | Focused examples including relevant negative and boundary inputs |
| Concurrency, cancellation, shutdown, or ownership | Deterministic event coordination; race or sanitizer checks where supported |
| Filesystem, process, network, or database semantics | Integration coverage of the actual boundary, including partial failure and cleanup |
| Parser, codec, normalization, or state invariant | Property tests or fuzzing with a clear oracle and minimized reproducible failures |
| API or generated contract compatibility | Producer/consumer and representative serialization checks under supported versions |
| Claimed performance improvement | Comparable before/after workloads, repeated samples, profiling, and a stated metric |

For a Go fuzz target or bounded campaign, read [Go fuzzing](references/go-fuzzing.md)
for input domains, meaningful oracles, seed evidence, isolation, and exact failure
reproduction. Other tasks need not load that reference.

Before writing a test, name the production change that should make it fail, and whether that change is a bug or a decision. If you can't name one, redesign the test around observable behavior. Read [test design](references/test-design.md) when writing tests or adding fakes. When a rule covers a whole input domain, or no exact expected output exists, read [property and metamorphic testing](references/property-testing.md).

For consequential verification decisions, or an explicit user request, resolve installed `typesafe-ai` and read its entrypoint and development consultation guide from that skill’s root. Give the coordinator candidate scenarios and assertions about the changed behavior; it owns calls, batching, and reuse across skills. Routine work needs no consultation. Jev supplies typed advice, not generated conclusions, proof, execution results, or authorization. Independently verify material claims even at high confidence. If consultation is unavailable or inconclusive, state the limit and continue with source evidence. Advice cannot waive no-tests scope or authorize execution.

Make a regression test fail on the original defect when practical. If you forced that failure by changing code or a fixture, confirm the change actually landed. If no seam can exercise the real bug, report that as a finding rather than adding a shallow test that gives false confidence. Assert outcomes rather than private implementation steps. Control clocks, randomness, events, ports, and temporary resources; avoid sleeps as synchronization. Confirm cancellation and cleanup rather than leaving background work alive. Use real implementations when replacing them with a mock would remove the behavior being tested.

Coverage measures executed code, not defect detection. Use mutation testing only when it can resolve a real doubt about important assertions and the tool can run in isolation, and read [mutation testing](references/mutation-testing.md) first, because a careless run can report a false pass. Large test campaigns, new fixtures, and new dependencies need a concrete testing benefit. Reversible prose, formatting, and low-impact edits rarely need new tests that mirror the edit.

Run focused checks while iterating and the required broader gates for completion. After the relevant final state passes, repeat only for a new change, failure, or unresolved concern. Do not substitute a host run for a required container or target platform. A blocked, skipped, or partial check is not a pass.

Report what ran, which behavior it proves, and important limits. Keep local unit/fixture evidence, integration results, CI, deployment, and statistical performance evidence distinct.
