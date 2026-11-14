---
name: principal-engineer
description: Language-neutral judgment for consequential changes, covering failure modes, ownership and lifecycle, durability, rollout, rollback, and compatibility.
disable-model-invocation: true
---

# Principal-engineer judgment

Use this before writing a change that touches state, background work, persistence, a public contract, or a deployment. It is language-neutral. A language pack adds idioms and examples: for Go, read `go-principal-engineer`. A routine local change needs none of this; say so and move on.

Start with the observable outcome and the invariant at risk. Follow the repository's existing ownership model, and read a focused reference only when it settles a real uncertainty.

## Failure modes

For each boundary the change crosses, walk the failure paths that can actually happen and write down what the caller sees, what state is left, and who repairs it:
- **Timeout.** The work may still complete after the caller gave up.
- **Duplicate.** A retry, redelivery or double click runs it twice.
- **Partial success.** The database committed but the message wasn't sent, or the reverse.
- **Crash between two steps,** and the restart that follows.
- **Concurrent writer.** Read, decide, write with no lock or constraint loses an update (`isolation-anomalies`).
- **Slow or down dependency.** Retries at several layers multiply load (`retry-amplification`).
- **Resource exhaustion:** full disk, file descriptors, memory, a connection pool.
- **Clock change or skew** (`clocks-and-time`).
- **Hostile or malformed input** at a trust boundary (`parse-dont-validate`).

A path is handled when it ends in a reported error, a safe retry, or a recorded state that recovery finishes. Logging and continuing is not handling, and neither is a fallback nobody asked for or retries that exhaust and then report success.

## Ownership and lifecycle

Every resource and every piece of background work has exactly one owner. The owner creates it, bounds it, observes its failure, and ends it. That covers connections, files, locks, temp directories, child processes, threads and tasks, timers, subscriptions, caches and queues. For each one, answer:
- Who starts it, and who waits for it to stop?
- What stops it on shutdown, cancellation or error, and how long can that take?
- Where does its failure go? An error nobody receives is a silent outage.
- What bounds it: how many run at once, how deep the queue grows, how much memory it holds?
- Is it on a liveness-critical path? Keep slow work off heartbeats, health checks and lock-holding paths unless ordering requires it.

Fire-and-forget work has no owner: it leaks on shutdown and loses its errors. Give each invariant one authority. A second copy of state, such as a cache, projection or replica, needs a named owner for its freshness and must never be the thing you decide from.

Prefer the smallest design that meets the requirement. Every new layer, queue, retry loop, cache, dependency or persisted field should solve a named problem and have a named owner.

## Durability

Decide what must survive a crash and what may be lost, then make the write order enforce it:
- Acknowledge only after the state that backs the acknowledgment is durable.
- Replace a file by writing a temp file in the same directory, flushing it to disk, renaming it over the old one, and flushing the directory. Never write in place.
- Write the data before the pointer to it, so a crash leaves an orphan, never a dangling reference.
- When persisted state and an external effect can diverge, record the intent first and make the effect idempotent (an idempotency key, an outbox, or a fencing token), so replay after a crash is safe.
- Recovery must work offline and from the state on disk alone. If it needs the process that crashed, it isn't recovery.
- Keep an irreversible effect behind the admission and durability boundary that authorizes it. When persisted state can no longer describe an effect that is already visible outside, fence the work.
- For agents and daemons, also check crash consistency, offline recovery, a full or failing disk, clock changes, and readiness that means the process can really serve. Running on a trusted host doesn't remove the need for crash recovery or for careful handling of sensitive data.

## Rollout and rollback

Assume old and new versions run side by side during a deploy, and that a rollback runs the old code against data the new code wrote.
- Make readers tolerant before you change writers. Expand, migrate, then contract (`expand-contract`).
- Put irreversible steps last, behind a check: dropping data, external notifications, payments, deletes.
- Name the signal that tells you to roll back, and confirm rollback works with the new data in place.
- Add a feature flag only when someone asked for one or the rollout really needs one, and give it an owner and a removal date.

## Compatibility

Name each contract the change touches and keep it unless the user agreed to break it: API and RPC schemas, CLI flags and exit codes, configuration keys, file and record formats, events, metric and log field names, and operator-visible behavior. Old records must still load. Unknown fields are kept or rejected on purpose, never silently dropped. A breaking change needs a migration path for every supported deployment.

## Consultation and verification

For a consequential ownership, lifecycle or recovery decision, or when the user asks, use configured advisers through `cw consult`. Routine work needs no consultation. Give the coordinator grounded choices and focused claims about constraints and failure behavior; it owns the calls, batching, and reuse across skills. Without configured advisers, decide on source evidence under the same [decision rules](../task-orchestration/SKILL.md#adjudicate). Advisers supply typed advice, not conclusions, proof, execution results or authorization. Verify material claims yourself, even at high confidence. If consultation is unavailable or inconclusive, state the limit and continue on source evidence.

Verify the dangerous boundary with the repository's checks: crash and restart, duplicate delivery, concurrent writers, a rollback that reads new data. Prefer deterministic event gates over sleeps. A diagram or a boundedness argument can clarify a design, but claims about throughput and latency need measurements under stated conditions (`performance`). Run the final relevant checks once, on the final state. Don't demand a full test campaign for every design question or minor edit, and respect review-only and no-tests scope.

Report the owner chosen for each resource, the failure behavior on each path, the evidence, and the remaining risk. Stop when the requested change is complete; don't extend it into unrelated cleanup.
