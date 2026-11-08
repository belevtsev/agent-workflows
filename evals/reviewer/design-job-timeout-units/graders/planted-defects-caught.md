---
type: llm
focus: { source: file, path: artefacts/design-review.md }
---

The design has two planted defects. (1) Compatibility and rollout: the
decision renames `timeout_seconds` to `timeout_ms`, and R3 in
`interface-contract.md` returns only `{"id", "state", "timeout_ms"}`. The
reviewed spec (`Job status timeout fields`, scenario `Status of a sub-second
job`) and API-412 criteria 3 and 4 require both fields in responses through
the deprecation period, and the Go SDK that customers pin decodes
`timeout_seconds` and fails when it is missing (`jobs/client/client.go:16`,
`:33-34`; the package comment at `:1-3` says integrations run several
releases behind). `Rollout and rollback` deploys the server before any
client can cope, ignores pinned SDKs, and its rollback (redeploy the
previous server) would break clients already moved to SDK 3.0. (2) Missing
interface behaviour: the spec scenario `Conflicting timeouts` and API-412
criterion 2 require HTTP 400 with `conflicting_timeout` when both fields
disagree, but no requirement covers it, the error codes list only
`malformed_body` and `invalid_timeout`, and `Failure behaviour` does not
mention it.

PASS if the review has one entry per finding, each with an ID (`D1`, `D2`,
...), a severity of `blocker`, `major` or `minor`, a category, a location
(file plus requirement or section), evidence and what would resolve it; one
finding, severity `blocker` or `major`, says dropping `timeout_seconds` from
responses breaks existing clients, cites the spec requirement or API-412
criterion 3 or 4 and a `path:line` in `client/client.go`, and says the
rollout or rollback plan does not account for clients on old or new SDK
releases (asking for both fields during deprecation, or an equivalent
compatible rollout); another finding, severity `blocker` or `major`, says
the contract has no requirement or error for conflicting timeouts, cites
the `Conflicting timeouts` scenario or API-412 criterion 2, and asks for a
requirement with HTTP 400 and `conflicting_timeout`; every finding cites
evidence; and the review ends with the inputs read and what was not
reviewed. An extra finding that `JobRequest` has no `timeout_seconds` field
although R1 accepts it is correct and acceptable.
FAIL if either planted defect is missing or rated `minor`, a finding lacks
evidence, the review accepts the rename as compatible, the review rewrites
the design or contract instead of reporting, or it cites the architect's
conversation (for example that customers upgrade the SDK within a sprint,
or that the handler can prefer `timeout_ms`) as evidence.
