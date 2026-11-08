---
type: llm
focus: { source: file, path: testdata/bookmarks/openspec/changes/add-bookmark-export/proposal.md }
---

The source is `testdata/product-brief.md`, version 3 dated 2026-10-12, owned
by Dana Whitfield (Product). It says users cannot get their bookmarks out and
support handles export requests by hand; it caps an export at 10,000
bookmarks but does not say what happens to a user with more; its "Not in this
release" list is JSON or HTML export, importing, another user's or a team's
bookmarks, and emailing the export. In the repository, the existing `401`
for a missing user is at `server.go:31-32` (`testdata/bookmarks/server.go`).
Adding a new endpoint breaks no existing behaviour.

PASS if `## Why` states the problem taken from the brief; `## What Changes`
is a bullet list and marks nothing **BREAKING**; `### New Capabilities` lists
`bookmark-export` and `### Modified Capabilities` lists no capability (empty,
"None" or equivalent); `## Non-goals` names the brief's excluded items;
`## Sources` names the product brief together with its version (3 or
2026-10-12); `## Open Questions` contains a question about what an export
does for a user with more than 10,000 bookmarks, and every open question
names the person or evidence that settles it (for example Dana Whitfield or
the product owner); and any statement about current behaviour (such as the
existing `401`) cites `server.go` with a line number or says it is
uncited.
FAIL if any of these is missing, if the proposal decides the over-10,000
behaviour itself instead of asking, if an open question names no person or
evidence that settles it, if the Sources entry has no version, or if the
proposal prescribes an implementation (such as `encoding/csv` or an
`exportHandler`) as decided.
