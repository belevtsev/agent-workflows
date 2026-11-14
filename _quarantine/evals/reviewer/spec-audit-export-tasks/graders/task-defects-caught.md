---
type: llm
focus: { source: file, path: artefacts/spec-review.md }
---

`tasks.md` has two planted task defects. (1) Task 1.2 "Improve audit export
performance and tidy up the audit package as needed" has no size, no
dependencies and no check, and "as needed" cannot be verified; SEC-77 asks
for no performance work. (2) Task 1.3 depends on task 1.4, a later task, so
the tasks are not in dependency order.

PASS if one finding locates task 1.2, has category "unsizeable or
unverifiable task", severity `blocker` or `major`, quotes the task text as
evidence, and names what is missing (a size, a check that fails if the task
is not done, and a source for the work, or its removal); and a finding
locates task 1.3, quotes or cites its `depends on: 1.4`, and says a task may
depend only on earlier tasks (category "unsizeable or unverifiable task" or
"format"); each of these findings has an ID, severity, category, location,
evidence and resolution.
FAIL if either task defect is missing, task 1.2 is rated `minor` or filed
only as a clarity issue, the dependency-order defect is not tied to tasks
1.3 and 1.4, or a finding lacks evidence. A separate scope-creep finding for
task 1.2 is acceptable in addition, not instead.
