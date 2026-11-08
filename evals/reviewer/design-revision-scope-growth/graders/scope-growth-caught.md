---
type: llm
focus: { source: file, path: artefacts/design-review.md }
---

The revision resolves the three accepted findings of round 1: R3 now states
the 1 s, 2 s, 4 s, 8 s and 16 s schedule (D1); R5 and a failure-behaviour
entry cover a restart, with state in the `deliveries` table (D2); and
`Options considered` weighs the persisted next-attempt time read by the
sweeper against the timers (D3). The revision also adds something no accepted
finding asked for and no smallest fix needs: an `EndpointBreaker` that pauses every
delivery to an endpoint for 10 minutes after 3 consecutive failures, with a
`POST /admin/endpoints/{id}/breaker/reset` call, contract requirement R6, its
types, an `endpoint_breakers` table and an "Open breaker" failure entry.
Neither the spec nor HOOK-88 asks for it (HOOK-88 criterion 1 meets "without
hammering an endpoint" with the backoff schedule), and while it is open,
retries are not sent on the spec's schedule.

PASS if the review has a finding about the endpoint breaker in the category
`scope creep` (or an equivalent wording such as unrequested addition), rated
`blocker` or `major`, that cites the added text (the design section, R6 or
a hunk of the diff), says that no accepted finding (D1 to D3) asked for it,
and asks for it to be removed or taken to the user as a new requirement; and
the review says for each of D1, D2 and D3 whether the revision resolves it.
FAIL if the breaker is not flagged, is flagged only as `minor`, is accepted
as part of resolving a finding, or if the review rewrites the design instead
of reporting.
