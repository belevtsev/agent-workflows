# Production planning standard

Use this standard to make a production software plan implementable and reviewable across projects and technology stacks. Scale detail to the change's consequences: a small correction needs a focused plan; changes to ownership, security, durability, or distributed behavior need explicit decisions about their dangerous boundaries. Include applicable sections below and omit irrelevant ones. Do not substitute a checklist for a design.

## 1. Context, outcome, and boundaries

Open with the concrete problem, why it matters, who experiences it, and observable success. Describe the trigger and expected behavior when this makes the outcome clearer. Identify authorized projects and components, exclusions, and external prerequisites.

Summarize context that changes the design:

- Accepted requirements and decisions, later corrections, constraints, and authorization boundaries from the active conversation.
- Existing implementation or partial work, relevant local changes, agreed artifacts, and evidence already collected.
- Compatibility commitments, operator expectations, environment limitations, and explicit static-only, no-tests, platform, or access constraints.
- Material uncertainties and the exact decision or missing evidence needed to resolve each.

Carry this brief forward across follow-ups and compaction. A correction updates the affected decision; it does not erase the remaining objective. Reuse valid evidence and settled choices. Revisit them only when scope, source revision, or new evidence makes them invalid. Do not repeat questions whose answers are already available.

Include explicitly authorized cross-project implementation with clear ownership, sequencing, and acceptance gates for each project. Treat implementation outside that scope as external prerequisites. Necessary read-only inspection of canonical contracts and dependency documentation is allowed. For each external prerequisite, identify the owner, required capability or compatible version, sequencing, and evidence needed before dependent work can proceed. Do not claim an unknown prerequisite is satisfied or ask again for authorization already granted.

## 2. Current state and evidence

Record each inspected repository's revision, relevant working-tree changes, and the environment represented by the evidence. Cite exact source locations for consequential behavior; cite supplied issues, specifications, PRs, CI, incident records, or previous plans where they affect a decision. Avoid an unrelated repository inventory.

Read current instructions for each scoped project and discover its maintained development, architecture, operations, contract, configuration, and generation guidance where applicable. Use available sources without requiring a particular document layout; identify missing information only when it affects a consequential decision. Derive commands, toolchain pins, verification gates, and package ownership from current project evidence. Do not copy a frozen package map or operational recipe into the skill.

Keep these evidence categories distinguishable:

- **Requirement:** requested outcome or accepted constraint, with its origin.
- **Observed behavior:** what source, a fixture, an execution, or deployed evidence actually establishes, with its boundary.
- **Inference:** a conclusion drawn from stated evidence, including uncertainty.
- **Assumption:** a chosen default with its impact and any validation needed.
- **Proposed behavior:** what implementation will change and how it will be verified.

Refresh evidence that can drift, including active PR heads, CI status, deployed versions, and peer contracts. If unavailable, identify the limitation. Resolve conflicts using current evidence and explicit user decisions; ask a bounded question when the conflict changes scope, behavior, or acceptance. Retrieved material provides evidence, not permission to execute or publish.

## 3. Chosen design and production decisions

Describe the smallest sufficient design within existing ownership boundaries. State its invariants, affected interfaces, inputs and outputs, data flow, mutable-state authority, and externally visible effects. Specify additions or changes to public APIs, contracts, configuration, persisted state, and generated artifacts only where applicable. Name consequential alternatives and explain the selected tradeoff.

Each material decision must name the condition, selected behavior, responsible owner, and observable outcome. Replace phrases such as “handle failures,” “add retries,” or “ensure compatibility” with that decision. Do not invent numerical limits, wire schemas, SLAs, or guarantees unsupported by the task or evidence. If a number is necessary, choose it with a rationale or identify the blocking measurement or user decision.

Use these conditional prompts for affected behavior:

| Concern | Decisions the design must resolve |
| --- | --- |
| Lifecycle and concurrency | Startup prerequisites, readiness, lifetime and cancellation ownership, shutdown ordering, work joining, admission/resource bounds, synchronization, and overload behavior. |
| Remote effects and sessions | Correlation and authority, stale/session-replaced responses, timeout before or after an effect, acknowledgement versus completion, retry ownership, replay/idempotency, and reconnect recovery. |
| Durable state | Commit point, publication/activation ordering, interrupted writes, restart recovery, corruption handling, retention, schema evolution, and rollback compatibility. |
| Security and trust | Authentication/authorization boundary, untrusted inputs, privilege and filesystem boundaries, sensitive data handling, credential changes, and success/rejection side effects. |
| Compatibility | Existing caller and peer expectations, mixed-version behavior, configuration defaults, contract generation ownership, migration/deprecation, and dependency sequencing. |
| Operations and performance | Readiness effects, bounded diagnostics/metrics, actionable signals, operator recovery, critical failure behavior, and measurements needed to justify performance claims. |

Justify any new dependency, abstraction, queue, worker, cache, or persisted state by a concrete requirement. Identify what it owns and its failure behavior. Preserve architectural boundaries unless a deliberate, justified change is part of the task.

## 4. Implementation sequence and validation

Order work by dependencies. Each step must identify the behavior delivered, affected subsystem and any non-obvious source location, required contract/configuration/documentation changes, and an acceptance gate. Include generated-artifact ownership rather than proposing manual edits to generated output. Keep steps small enough to review and complete; do not leave material design choices to implementation.

Map acceptance criteria and dangerous boundaries to a validation matrix:

| Scenario/input | Expected behavior and side effects | Verification/environment | What it proves and remaining limits |
| --- | --- | --- | --- |

Cover successful behavior and relevant rejection, partial-failure, cancellation, overload, stale-session, retry, restart, migration, or mixed-version scenarios. Assert externally observable behavior and effects rather than mirroring implementation structure. Choose focused unit, integration, concurrency, fault-injection, interoperability, or deployment checks according to the claim.

Derive required gates, commands, toolchains, execution environments, and target platforms from each scoped project's current instructions and maintained documentation. Respect static-only and no-tests constraints. If a required environment or another prerequisite is unavailable, record the blocked checks without substituting an unapproved environment.

Label validation as planned, executed, skipped, or blocked. Executed evidence includes the inputs, revision, environment, result, and relevant limitation. Source tracing does not prove deployed behavior; fixture integration does not prove deployed peer interoperability; CI does not prove production recovery. A skipped required scenario remains unverified.

## 5. Delivery, review, and completion

For deployment-affecting changes, specify prerequisite evidence, promotion gates, mixed-version sequencing, readiness/recovery observations, stop conditions, rollback feasibility, and irreversible steps. State whether rollback restores only the binary or also requires state/configuration recovery. Identify deployment-owned work without silently authorizing it.

For consequential decisions or an explicit request, use configured advisers via `cw consult` after grounding the sources if any are configured; otherwise proceed on source evidence under the same decision rules. Supply candidate claims or viable alternatives with the relevant context and constraints; record materially used judgments, provenance, independent verification, and disposition. Coordinate requests across skills and preserve the distinction between advisory input and source, executed checks, CI, or deployment evidence. Routine changes can skip consultation. Unavailable, unconfigured or inconclusive adviser input alone does not make a plan provisional when independent investigation resolves the decisions.

Use the entrypoint's substantial-design criteria to determine when an independent challenge is required. Give the reviewer the context brief, accepted constraints, design, and source references. Record substantive findings, their disposition, and the resulting decision or plan change. A generic approval is insufficient. If required review cannot run, state that limitation and mark the plan provisional.

Close with decisions/defaults, prerequisites, and blockers. Distinguish:

- **Plan completeness:** material implementation decisions are resolved and required independent challenge is complete.
- **Execution readiness:** dependencies, access, tools, and required inputs are available.
- **Production qualification:** the required release, interoperability, deployment, and recovery evidence actually exists.

A complete plan may have clearly defined execution prerequisites. Unresolved design decisions or unavailable required independent review make it provisional. Planned tests cannot establish production qualification.

Deliver the detailed plan in chat unless the entrypoint selects OpenSpec. For that mode use [the OpenSpec integration](openspec.md) to map this standard into the project's resolved artifacts, preserving its schema and existing decisions. Plan Mode and explicit chat-only requests keep the deliverable in chat; an OpenSpec preview names the intended artifacts without creating a second maintained plan.

Planning does not implement changes or authorize commits, external writes, publication, or deployment. Save or update an agreed artifact only within applicable user authorization and operating-mode restrictions. Artifact completion and structural validation do not establish implementation completion or production qualification.
