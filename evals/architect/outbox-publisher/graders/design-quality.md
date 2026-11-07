---
type: llm
focus: { source: file, path: artefacts/design.md }
---

PASS if the design names the defect in the current code with `path:line`
evidence (publish after commit in a goroutine), presents at least two options
with concrete consequences (for example transactional outbox with a relay
versus change data capture versus the smallest change), chooses one, names
the owner of ordering and deduplication, states what happens when the relay
crashes mid-batch, when Kafka is down, and when the same event is relayed
twice, and gives a rollout order that avoids a dual-write window plus a
rollback step naming any irreversible step (such as a new table) or saying
there is none.
FAIL if at-least-once delivery, per-order ordering or relay crash behaviour
is missing, if the options lack consequences, or if the design claims
exactly-once delivery.
