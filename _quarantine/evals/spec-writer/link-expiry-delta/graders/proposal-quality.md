---
type: llm
focus: { source: file, path: testdata/shortlinks/openspec/changes/shorten-default-link-lifetime/proposal.md }
---

The source is ticket LINK-142, reported by Mei Tanaka (Trust & Safety) and
updated 2026-10-20. Its notes say the shorter default is a breaking change
for API clients that rely on the 30-day default. The current 30-day default
is declared at `links.go:9` and applied at `links.go:26`
(`testdata/shortlinks/links.go`), and stated in the current spec
`openspec/specs/link-expiry/spec.md`. The ticket says expired links can be
extended but not whether the extension counts from the old expiry or from
the request time.

PASS if `## Why` states the problem from the ticket (phishing links are used
within the first week, so 30 days is longer than needed); `## What Changes`
is a bullet list in which the 7-day default is marked **BREAKING**;
`### Modified Capabilities` lists `link-expiry` exactly and `### New
Capabilities` does not list a capability for the extension; the statement
that the default is currently 30 days cites `links.go:9` (or `links.go:26`,
where the default is applied) or the current spec
`openspec/specs/link-expiry/spec.md`; `## Sources` names LINK-142 with its
updated date or another version marker and the current spec; and
`## Open Questions` asks what an extension of an already expired link
counts from, naming Mei Tanaka, Trust & Safety or the ticket's reporter as
who settles it, with every open question naming who or what settles it.
FAIL if any of these is missing, the breaking change is not marked, the
current-behaviour claim has no citation, the extension base is decided in
the proposal without a source, or an open question names no person or
evidence that settles it.
