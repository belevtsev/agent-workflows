---
type: llm
focus: { source: file, path: artefacts/adversarial_test.go }
---

This file holds the adversarial tester's kept tests for Go package `memo`.
The role contract says every kept test must fail on the current (seeded)
source because of the violation it cites, and must name its finding ID and
cited rule; a test that passes is discarded, not kept.

How the seeded source really behaves. Only one thing is wrong: on a miss the
helper `begin(key)` creates the in-flight call but never registers it in the
`c.inflight` map, so a second `Get` of a key whose `load` is still running
does not wait and calls `load` again. Overlapping Gets of one key therefore
make two (or more) calls of `load`, against R3 and DR1. Everything else
already matches the contract, so a test of it passes on the seeded source:
a first `Get` returns `load`'s value with a nil error; sequential Gets after a
completed successful load return the cached value without calling `load`
again; an error from `load` is returned as `""` with that error and is not
cached (the next `Get` calls `load` again); different keys load separately and
a slow load of one key does not block Gets of another (`load` runs with no
lock held); and concurrent Gets of distinct or already cached keys report no
data race under `-race`.

PASS if the file declares `package memo`; every test function in it
(`func TestXxx(t *testing.T)`) makes at least two Gets of the same key overlap
while a `load` of that key is in flight (for example a `load` blocked on a
channel until released) and asserts, from R3 or DR1, that `load` runs only
once or that the later Get waits for the first, so it fails on the seeded
source for that reason; and each test names its finding ID (`A1`, ...) and the
rule it cites (R3 or DR1) in its name or a comment. An assertion on the
in-flight map itself is acceptable, since DR1 states that rule.
FAIL if the file is missing or holds no test function; any test function
asserts only behaviour listed above as already present (it would pass on the
seeded source); any test lacks a finding ID or a cited requirement or design
rule, or cites a rule the contract and design do not state; or any test takes
its expected values from the current output rather than from the contract or
design.
