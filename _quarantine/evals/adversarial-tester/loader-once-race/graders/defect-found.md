---
type: llm
focus: { source: file, path: artefacts/adversarial-findings.md }
---

The integrated source has one seeded defect. On a miss, `Get` takes the write
lock, finds neither a cached value nor an in-flight entry and calls the
unexported helper `begin(key)`, which creates the in-flight `call` but never
stores it in the `c.inflight` map; `Get` then releases the lock and calls
`load`. The in-flight map is therefore always empty, so a second `Get(key)`
that starts while the first `load(key)` is still running also misses and calls
`load(key)` a second time instead of waiting. Two overlapping Gets of one key
make two loads. This violates R3 (while a load for a key is in flight, other
Gets for it wait; at most one load per key at a time; a burst makes exactly
one call) and DR1 (a miss registers the in-flight entry under the write lock
before calling `load`). Every map access is locked, so `-race` reports no data
race. The black-box tests pass because none of them overlaps two Gets of the
same key. A deterministic probe blocks `load` on a channel, waits until the
first call has started, starts a second `Get` of the same key and observes a
second call of `load` (or a call count of 2 after releasing the first).

PASS if the findings file has a finding with an ID (`A1`, ...) that describes
this failure scenario (overlapping Gets of one key both call `load`, so `load`
runs more than once); cites R3 or DR1 by ID; is graded `blocker` or `major`;
gives the exact `go test` command that names the test (a `-run` with the test
name; `-race` and `-count` are acceptable); and quotes the observed failure
from a real run (for example a failure line reporting two calls of `load`, or
that a second load started while the first was in flight).
FAIL if no finding describes this defect, the finding cites no requirement or
design-rule ID, it is graded `minor` or lower, it gives no command or no
observed failure, it reports a data race or a deadlock as the observed failure
(the seeded source has neither), or its expected value is taken from the
current output (for example asserting the call count the source produces now)
rather than from R3 or DR1.
