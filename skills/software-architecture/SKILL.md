---
name: software-architecture
description: Design or assess component ownership, dependencies, and interface evolution for substantial changes across packages or repositories.
---

# Software architecture

Start with the requested behavior, constraints, and decision to make. Ground the current design in source, tests, interfaces, and maintained documentation. Keep observed behavior separate from a proposed design. Ordinary local changes do not require an architecture document.

Trace only the affected components and consumers. Identify who owns mutable state, lifecycle, identity, persistence, and external effects. Record the direction of calls and data flow; a transport client is not necessarily the business owner. For work across repositories, establish exact revisions and distinguish local workspace replacements from published dependencies. If a requested symbol is absent, report that exact non-match before proposing an alternative.

For consequential architecture decisions, or an explicit user request, use configured advisers via `cw consult` if any are configured; otherwise proceed on source evidence under the same [decision rules](../task-orchestration/SKILL.md#adjudicate). Give the coordinator viable alternatives and grounded constraints on authority, coupling, compatibility, and recovery; it owns calls, batching, and reuse across skills. Routine work needs no consultation. Advisers supply typed advice, not generated conclusions, proof, execution results, or authorization. Independently verify material claims even at high confidence. If consultation is unavailable or inconclusive, state the limit and continue with source evidence.

Compare the current design with the smallest viable change. Add another option only when a meaningful tradeoff remains. Evaluate coupling (use [naming coupling](references/coupling.md) to make that argument concrete), failure recovery, consistency, compatibility, operational cost, and reversibility against the actual workload. New abstractions, services, queues, caches, and dependencies need a concrete benefit and an owner. Preserve explicit boundaries that encode distinct failure or durability semantics.

For an interface change, trace authoritative schema or API, generated artifacts, producers, consumers, stored representations, and deployment order. Check defaults, missing and unknown values, errors, version negotiation, and whether mixed versions must work. Derive migration requirements from the supported deployment model; do not invent legacy support.

Use an editable diagram when relationships or ordering are easier to inspect visually. Label actual ownership, protocol direction, trust boundaries, and failure paths. Use Mermaid for a compact static explanation; use the diagram tool requested by the user for a deliverable. Inspect the rendered result and correct it before delivery.

For a substantial decision, use the optional [decision record](references/decision-record.md). Finish with a chosen design, reviewable implementation steps, and evidence that would establish the important invariants. When implementation is authorized, carry it through the relevant verification rather than stopping at the design.
