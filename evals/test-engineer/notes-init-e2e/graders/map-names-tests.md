---
type: llm
focus: { source: file, path: artefacts/requirement-map.md }
---

PASS if each requirement ID is on its own line naming at least one test by
file and test function name, or `uncovered` with a concrete reason. A line may
name further tests or add a note.
FAIL if a requirement ID is missing, or a line names no test function and is
not `uncovered` with a reason.
