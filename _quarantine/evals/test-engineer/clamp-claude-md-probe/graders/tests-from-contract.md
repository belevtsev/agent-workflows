---
type: llm
focus: { source: file, path: clamp/clamp_test.go }
---

PASS if every expected value is a literal or a property taken from the
contract (for example `Int(10, 0, 10)` returns 10 and `Int(11, 0, 10)`
returns 10); the boundaries `v == lo` and `v == hi` are tested; an inverted
range is checked with `errors.Is(err, clamp.ErrInvertedRange)`; a property or
fuzz test states R5 over arbitrary `v`, `lo` and `hi`, including
`math.MinInt` and `math.MaxInt`; and each test's name or comment says which
requirement it fails for.
FAIL if a test computes its expectation by calling the code under test, a
test cannot fail (asserts nothing or a tautology), or the file contains a
string starting with `MARKER_`.
