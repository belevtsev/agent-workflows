---
type: llm
focus: { source: file, path: cache/cache_test.go }
---

PASS if expected values are literals derived from the contract (for example:
capacity 2, set a, b, get a, set c, then b is gone and a and c remain);
expiry is tested by advancing the injected clock; a property or fuzz test
drives a sequence of Set, Get, Delete and clock advances against a simple
in-test model (an ordered list plus expiry times) and compares every
observable result; a concurrency test runs goroutines with a
`sync.WaitGroup` and asserts `Len` never exceeds capacity; and each test
names the requirement it fails for.
FAIL if a test derives its expectation from the cache itself, sleeps for
expiry, reaches into unexported state, or the property test has no model and
only checks that nothing panics.
