---
name: go-principal-engineer
description: Resolve consequential Go ownership, lifecycle, durability, or rollout decisions with failure behavior and checks.
---

# Production Go decisions

Establish the observable outcome and invariant at risk. Use the existing repository ownership model; load a focused technical reference only when it resolves a real uncertainty. A routine local change does not need a separate architecture exercise.

For a consequential change, identify who owns mutable state, work, cancellation, and external effects. Trace relevant timeout, duplicate, partial-success, restart, and rollback paths. Name compatibility boundaries that actually change: API, protobuf, storage, configuration, metrics, or operator behavior.

For consequential ownership, lifecycle, or recovery decisions, or an explicit user request, use configured advisers via `cw consult` if any are configured; otherwise proceed on source evidence under the same [decision rules](../task-orchestration/SKILL.md#adjudicate). Give the coordinator grounded choices and focused claims about constraints and failure behavior; it owns calls, batching, and reuse across skills. Routine work needs no consultation. Advisers supply typed advice, not generated conclusions, proof, execution results, or authorization. Independently verify material claims even at high confidence. If consultation is unavailable or inconclusive, state the limit and continue with source evidence.

Prefer the smallest design that gives each invariant one authority. A new layer, interface, goroutine, queue, retry loop, cache, dependency, or persisted field should solve a concrete problem with a named owner. Keep irreversible effects behind the appropriate admission/durability boundary. Fence work when persisted state can no longer describe an externally visible effect. Bound concurrency, queues, retries, memory, and shutdown waits; keep slow work off liveness-critical paths unless ordering requires it.

For agents and daemons, examine crash consistency, offline recovery, disk failure, clock changes, and usable readiness when relevant. Trusted-host assumptions do not remove crash recovery or sensitive-data handling. A diagram or boundedness argument may clarify a design; throughput and latency claims require measurements under stated conditions.

When a change crosses service, persistence, schema, or authority boundaries, read [backend ownership boundaries](references/backend-boundaries.md). When durable state and external effects can diverge under duplicate work, timeouts, crashes, or ownership transfer, read [distributed workflows](references/distributed-workflows.md). When work can be retried or redelivered, read [idempotency keys](references/idempotency.md). When a pool, queue, or timeout needs a number, read [capacity and tail latency](references/capacity-and-latency.md). Load these references only for the affected decision.

Verify the dangerous boundary through the repository's appropriate checks. Prefer deterministic event gates over sleeps. Complete required gates once for the final relevant state; do not require a full test campaign for every design question or minor edit. Respect review-only and no-tests scope. Report concrete findings, the chosen owner and failure behavior, evidence, and residual risk; stop when the requested change is complete rather than extending it into unrelated cleanup.
