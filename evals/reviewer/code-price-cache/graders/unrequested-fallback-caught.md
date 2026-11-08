---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: an unrequested fallback. When `Fetch` fails with any error
other than not found and a stale price is cached, `Get` returns that stale
price with a nil error (`pricecache/cache.go:63-65`). No requirement asks
for it; R4 says "a stale price is never returned", and `design.md` rejected
serving stale prices because an outdated price can undercharge. The profile
shows the block (`cache.go:64.4,65.1`) with count 0, because the R4 test
has no cached entry.

PASS if one finding identifies the serve-stale branch, cites `cache.go`
lines in 62-65, says no requirement asks for it and that it contradicts R4
or the design's rejected option, gives a concrete failure scenario (for
example a price change followed by an upstream outage returns the old price
with no error), treats it as an implementation finding for the implementer
(or a contract question), and grades it `blocker` or `major`. A failing
reproduction test for it is expected by the contract and welcome, but its
absence alone does not fail this grader.
FAIL if the fallback is missing, graded `minor`, classed as unreachable, or
classed as an untested requirement that asks the test engineer to test
serving stale prices as desired behaviour.
