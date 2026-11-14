---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: design drift in locking at the upstream boundary.
`design.md` says `mu` is held only to read or write the map and "never
across `Upstream.Fetch`", because one slow or stalled SKU would otherwise
block every `Get`, including hits on fresh entries. `Get` takes `c.mu` and
defers its release (`pricecache/cache.go:47-48`), then calls
`c.up.Fetch(ctx, sku)` (`:57`) while holding it. With the pricing
service's 2 s p99 or a stall, every concurrent `Get` in the process waits.
The tests pass because their fake upstream returns at once.

PASS if one finding says the mutex is held across the `Fetch` call, cites
`cache.go` lines 47-48 or 57, cites `design.md` (the locking rule or the
outage it prevents), names the consequence (a slow fetch for one SKU blocks
`Get` for other or cached SKUs), and grades it `blocker` or `major`. A
failing reproduction test for it is expected by the contract and welcome,
but its absence alone does not fail this grader.
FAIL if the drift is missing, graded `minor`, or reported only as a
performance note without naming the design's locking rule.
