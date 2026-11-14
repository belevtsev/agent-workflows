---
type: llm
focus: { source: file, path: retry/budget_test.go }
---

PASS if every expected value is a literal or a property derived from the
contract (for example: after 10 `RecordRequest` calls with ratio 0.2 and
minRetries 1, `Allow` returns true exactly twice, then false); the sliding
window is tested by advancing the injected clock, never by sleeping; the
concurrency case starts goroutines, waits with a `sync.WaitGroup` and asserts
the allowed count is at most `max(minRetries, int(math.Ceil(ratio*float64(requests))))`; a
property or fuzz test states an invariant over arbitrary request and retry
sequences; and each test's name or comment says which requirement it fails
for.
FAIL if a test computes its expectation by calling the code under test, if
`time.Sleep` is used for synchronisation or window expiry, if the tests
reach into unexported fields, or if a test cannot fail (asserts nothing or
asserts a tautology).
