---
type: llm
focus: { source: file, path: testdata/shortlinks/openspec/changes/shorten-default-link-lifetime/specs/link-expiry/spec.md }
---

The current spec `openspec/specs/link-expiry/spec.md` has two requirements.
`### Requirement: Default link lifetime` says a link without an explicit
expiry expires 30 days after creation and an expired link returns HTTP 410
Gone, with two scenarios: requested at 2026-01-30T23:59:59Z after creation
at 2026-01-01T00:00:00Z gives HTTP 302 with `Location`; requested at
2026-01-31T00:00:00Z gives HTTP 410 Gone. `### Requirement: Explicit expiry`
is unchanged by the ticket. Ticket LINK-142 changes the default to 7 days
(410 stays), keeps the expiry of links created before the release, and adds
`POST /links/{code}/extend` with body `{"days": N}`: N a whole number from 1
to 90 inclusive gives HTTP 200 with the new `expires_at`; N outside 1 to 90
or not a whole number gives HTTP 400 with the expiry unchanged; anyone other
than the owner gets HTTP 403 with the expiry unchanged; expired links can be
extended. The ticket does not say whether an extension counts from the
link's current expiry or from the time of the request.

PASS if under `## MODIFIED Requirements` there is a block headed exactly
`### Requirement: Default link lifetime` that restates the whole requirement
with the 7-day default and the 410 Gone response, and carries both of the
existing scenarios updated to the 7-day boundary (one just before 7 days
gives 302 with `Location`, one at exactly 7 days gives 410 Gone) rather than
only the changed sentence; links created before the release keeping their
existing expiry is stated as a requirement or scenario somewhere in the
file; `Explicit expiry` is not listed under MODIFIED or REMOVED; and under
`## ADDED Requirements` the extension is specified with scenarios for an
owner's successful extension (HTTP 200 with a new `expires_at`), the
boundaries 1 and 90 accepted, values such as 0, 91 or 2.5 rejected with 400
and the expiry unchanged, a non-owner rejected with 403 and the expiry
unchanged, and an expired link being extendable.
FAIL if the MODIFIED header differs from the existing one in any character,
the MODIFIED block drops a scenario or keeps the 30-day values, the
extension is missing its 400 or 403 cases or the 1 and 90 boundaries, a
scenario fixes the resulting `expires_at` of an extension of an already
expired link (which presumes the base the ticket does not give), or the
file adds behaviour the ticket does not ask for (such as a maximum total
lifetime, rate limits or notifications).
