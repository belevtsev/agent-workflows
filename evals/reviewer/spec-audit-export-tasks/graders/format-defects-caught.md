---
type: llm
focus: { source: file, path: artefacts/spec-review.md }
---

`specs/audit-export/spec.md` has two planted format defects. (1) The
scenario "Export of an empty range" is headed `### Scenario:` (three `#`)
instead of `#### Scenario:`, so OpenSpec does not read it as a scenario of
`Export audit events for a date range`, and SEC-77 criterion 3 (an empty
range returns only the header row) is left without a recognised scenario.
(2) The scenario `Non-auditor requests an export` under `Only auditors
export` has `GIVEN` and `WHEN` bullets but no `THEN`, so it states no
outcome even though SEC-77 gives HTTP 403.

PASS if one finding locates the "Export of an empty range" scenario, has
category "format", quotes the three-hash header as evidence and cites the
required `#### Scenario:` layout (the spec writer's required outputs or
OpenSpec's format), with resolution to use four `#`; and another finding
locates `Non-auditor requests an export`, has category "format" or
"untestable scenario", severity `blocker` or `major`, notes the missing
`THEN` and names the expected outcome from SEC-77 (HTTP 403); each of these
findings has an ID, severity, category, location, evidence and resolution.
FAIL if either format defect is missing, the three-hash header is not
quoted or not tied to the required layout, the missing `THEN` is rated
`minor`, or the review cites the writer's intent or notes (for example that
the heading was meant to render smaller) as evidence.
