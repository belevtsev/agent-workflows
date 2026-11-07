---
type: llm
focus: { source: file, path: artefacts/design.md }
---

PASS if the design presents at least two options (one must be the smallest
change) each with a concrete consequence; states the chosen decision; names
which component owns the per-tenant counters and how the three replicas share
them; states what happens on a Redis timeout or outage (fail open or fail
closed, and why) as explicit behaviour rather than "errors are handled";
gives a rollout order across replicas and a rollback step, naming any
irreversible step or saying there is none; and maps each requirement to a
kind of evidence (unit, integration, end-to-end, property or manual).
FAIL if Redis unavailability or multi-replica sharing is not addressed, if
options lack consequences, if the design adds a new service, queue or cache
without naming its benefit and owner, or if claims about current code lack
`path:line` evidence.
