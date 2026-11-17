# Reduce coordinator protocol overhead

## Why

The retained ordinary canary captured 1,856,554 coordinator tokens, about 57%
of its observed total. Most coordinator input was cached. Twelve short waits,
separate status probes and repeated handoff bookkeeping suggest opportunities
to reduce coordinator turns, but do not establish recoverable token savings.
The existing saved-handoff replay also omitted ordinary assignment transport.

## What changes

- Repair the offline handoff baseline and add complete scripted host/worker
  transport replays through review, repair and a real local hooked commit.
- Align managed coordinator instructions with existing prepare/finish mechanics,
  avoid repeated unchanged configuration/capability discovery and short polling,
  and preserve deadline, cancellation and progress obligations.
- Extend preparation with an explicit versioned generated native dispatch
  description, and add bounded structured worker-return ingestion in Go.
- Update generated role/skill text, help/completion, OpenSpec, README and how-to
  guidance with regression and local validation evidence.

## Scope and authority

Compact mode remains opt-in. Default dispatch output and ordinary task-start
behavior remain compatible. Native hosts launch fresh workers; Kitsu invokes no
model. Workers retain full original contracts, roles, selected skills and limits.
Returns are untrusted claims, not proof of native execution or skill loading.
Meaningful red tests, independent review, write bounds, current checks, human
approvals and the final hooked commit retain their authority.

Bounded coordinator state views, state deltas, durable small-task resumption,
model routing changes and automatic model orchestration are outside this change.
No paid pilot, model calibration, paid reviewer or CI waiting is included.
Historical frozen results and assets remain unchanged. Offline bytes and calls
cannot establish real-task quality equivalence or provider-token savings.
