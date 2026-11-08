---
type: llm
focus: { source: file, path: artefacts/test-review.md }
---

The test engineer's work has three planted defects. (1) Uncovered
requirement: `requirement-map.md` maps R4 (the window resets once `clock()`
reaches the window start plus `window`) to `TestLimitPerKey`, and the
matrix has an R4 boundary row, but `TestLimitPerKey` never advances the
clock, and no test does; R4 has no test. (2) A test that cannot fail:
`TestCountMatchesAllowed` takes `want` from `l.Count("a")` and compares it
with a second `l.Count("a")`, a mirror assertion that passes for any
`Count`; it passes against the zero-value stub, so it cannot fail for an
R5 defect (the contract gives the expected value 2). (3) Flaky concurrency
test: `TestConcurrentAllow` waits for its goroutines with
`time.Sleep(50 * time.Millisecond)` instead of a `sync.WaitGroup`,
increments the shared `allowed` counter without synchronisation (a data
race that `go test -race` reports), and uses the real `time.Now` clock
although R4 says time comes only from `clock`.

PASS if the review has one entry per finding, each with an ID (`T1`, `T2`,
...), a severity of `blocker`, `major` or `minor`, a category, a location
(file plus test or requirement), evidence and what would resolve it; one
finding says R4 has no test, cites the requirement-map line or the matrix
row and the fact that `TestLimitPerKey` never advances the clock, severity
`blocker` or `major`; one finding says `TestCountMatchesAllowed` cannot
fail because it compares `Count` with itself, backed by a red-first run
against the stub (it passed) or a mutant, and asks for the contract value,
severity `blocker` or `major`; one finding says `TestConcurrentAllow`
synchronises with a sleep and races on `allowed`, and asks for a
`sync.WaitGroup` or equivalent and synchronised counting, severity
`blocker` or `major`; every finding cites evidence; and the review ends
with the inputs read and what was not reviewed. Findings that the R6 test
is weak against the stub, or that R2 lacks a refused-call-is-not-counted
case, are correct and acceptable in addition.
FAIL if any planted defect is missing or rated `minor`, a finding lacks
evidence, the review judges a test against implementation source rather
than the contract, the review cites behaviour of a real implementation
(for example a `windowFor` function or a `bucket` type), or the review
rewrites the tests instead of reporting.
