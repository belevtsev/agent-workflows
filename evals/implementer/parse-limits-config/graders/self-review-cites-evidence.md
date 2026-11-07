---
type: llm
focus: { source: file, path: artefacts/self-review.md }
---

PASS if every one of the eight checklist items (parse don't validate,
explicit error contract, cancellation and ownership, no speculative
abstraction, no unrequested fallback, concurrent writers, unrelated
changes preserved, checks run) is answered `yes` with a `path:line`
reference or `n/a` with a reason, and a Checks section lists each
command run with its exit status.
FAIL if an item is missing, answered without evidence, or a check is
claimed as passed without a command and exit status.
