---
type: llm
focus: { source: file, path: artefacts/design.md }
---

PASS if the design shows with `path:line` evidence that the current code
shares one mutable pointer, presents at least two options with concrete
consequences (for example an atomic snapshot swap versus restart versus a
locked struct), chooses one, names the single owner of the active
configuration, states what each reader observes during a swap, what happens
when the new file is invalid and when two SIGHUPs arrive during one batch,
and gives a rollout and rollback step naming any irreversible step or saying
there is none.
FAIL if invalid-file behaviour, in-flight batch isolation or concurrent
reload handling is missing, if the design introduces a new process or
service, or if options lack consequences.
