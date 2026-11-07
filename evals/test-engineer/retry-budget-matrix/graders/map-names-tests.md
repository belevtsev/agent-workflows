---
type: llm
focus: { source: file, path: artefacts/requirement-map.md }
---

PASS if each requirement ID is on its own line naming a test function that
exists for it, or `uncovered` with a concrete reason, and every named test
would fail if that requirement were violated (the line says how).
FAIL if a requirement is missing, a line names no test, or a line names a
test that could not fail for that requirement.
