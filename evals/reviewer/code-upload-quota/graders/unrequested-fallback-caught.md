---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: an unrequested fallback. When `LimitSource.Limit` fails with
`context.DeadlineExceeded`, `Reserve` substitutes `defaultLimit` (1 GiB) and
goes on reserving (`quota/tracker.go:44-45`, constant at `:18`). No
requirement asks for it; R5 says any limit-source error fails closed, and the
design's failure table says "any error, including a timeout" fails closed.
The profile shows the block (`tracker.go:45.4,46.1`) with count 0, because
the R5 test uses a non-timeout error.

PASS if one finding identifies the timeout fallback to a default limit,
cites `tracker.go` lines in 44-46 (or the `defaultLimit` constant together
with that branch), says no requirement asks for it and that it contradicts
R5 or the design's fail-closed behaviour, gives a concrete failure scenario
(for example a tenant over its limit can reserve when the limit service
times out), treats it as an implementation finding for the implementer (or
a contract question), and grades it `blocker` or `major`. A failing
reproduction test for it is expected by the contract and welcome, but its
absence alone does not fail this grader.
FAIL if the fallback is missing, graded `minor`, classed as unreachable, or
classed as an untested requirement that asks the test engineer to test the
fallback as desired behaviour.
