---
type: llm
focus: { source: file, path: artefacts/test-review.md }
---

The test engineer's work has three planted defects. (1) A timing bound that
also limits real work: `TestSweepRemovesExpired` sweeps 200000 sessions
under `context.WithTimeout(..., 20*time.Millisecond)` and requires
`err == nil` and all 200000 removed, but R4 sets no latency bound and says a
cancelled `Sweep` returns a partial count with `ctx.Err()`; on a slow or
`-race` run a correct `Sweep` is cancelled and the test fails, so the
deadline caps the work under test and makes the test flaky. The R4
cancellation behaviour itself is never tested. (2) Flaky real-clock test
that misses the boundary: `TestActiveExpiresAfterIdle` builds the store
with `time.Now` and waits with `time.Sleep(30 * time.Millisecond)`, although
R3 says time comes only from `clock`; it never checks the exact boundary
(at last activity plus `idle` the session is expired) and never checks
that the session was active before, so it passes against the zero-value
stub. (3) Requirement without a real test: `requirement-map.md` maps R6
(`Close` is idempotent) to `TestClose`, which calls `Close` once; no test
calls it twice, so the test passes against the stub and cannot fail for an
idempotency defect.

PASS if the review has one entry per finding, each with an ID (`T1`, `T2`,
...), a severity of `blocker`, `major` or `minor`, a category, a location
(file plus test or requirement), evidence and what would resolve it; one
finding says the 20 ms deadline in `TestSweepRemovesExpired` limits the
real work or makes the test flaky, cites R4's absence of a latency bound,
and asks to drop the deadline or test cancellation separately (for example
with a context cancelled up front), severity `blocker` or `major`; one
finding says `TestActiveExpiresAfterIdle` uses the real clock and a sleep
instead of the injected clock and does not test the exact-`idle` boundary,
severity `blocker` or `major`; one finding says R6 has no test that calls
`Close` twice, cites the requirement-map line, and is backed by a
red-first run against the stub or by reasoning from the test body,
severity `blocker` or `major`; every finding cites evidence; and the
review ends with the inputs read and what was not reviewed. A finding that
R2's `ErrClosed` after `Close` is untested is correct and acceptable in
addition.
FAIL if any planted defect is missing or rated `minor`, a finding lacks
evidence, the review recommends a longer timeout as the fix for the first
defect, the review judges a test against implementation source rather
than the contract, the review cites a real implementation (for example a
`sweepBatch` constant), or the review rewrites the tests instead of
reporting.
