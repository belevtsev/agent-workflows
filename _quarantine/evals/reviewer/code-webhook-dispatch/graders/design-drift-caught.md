---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: design drift in client ownership. `design.md` says the
queue worker owns the `*http.Client` (timeout, transport, egress proxy,
mTLS) and passes it to `New`, and that `Dispatcher` "sends every request
through that client and never builds or uses another one". `New` stores the
client (`dispatch/dispatch.go:45-47`), but `Deliver` sends with
`http.DefaultClient.Do(req)` (`:63`), so the configured timeout, proxy and
transport never apply and a hung endpoint can block a delivery with no
timeout unless the caller's context has a deadline. The tests pass because
`httptest` servers accept any client.

PASS if one finding says `Deliver` uses `http.DefaultClient` instead of the
client passed to `New`, cites `dispatch.go` around line 63, cites
`design.md` (the ownership table or the rejected option of building another
client), names a concrete consequence (the injected timeout, proxy or
transport is ignored, or requests can hang), and grades it `blocker` or
`major`. A failing reproduction test for it is expected by the contract and
welcome, but its absence alone does not fail this grader.
FAIL if the drift is missing, graded `minor`, or reported only as style
without naming the ownership breach.
