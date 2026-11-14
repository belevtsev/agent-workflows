---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: requirement R3 has no test. `Get` maps an upstream
`ErrNotFound` to `ErrUnknownSKU` and caches the miss
(`pricecache/cache.go:59-61`), and answers from that negative entry
(`:51-53`), but the coverage profile `testdata/checks/cover.out` shows both
blocks (`cache.go:52.3,53.1` and `:60.3,61.26`) with count 0: no test in
`cache_test.go` makes the upstream report not found. Under the code-review
contract this is an untested requirement: a test finding for a fresh test
engineer, at least `major`, whose summary carries only the requirement ID,
contract-level inputs and the expected outcome.

PASS if one finding says R3 has no exercising test, cites the unexecuted
blocks or the profile lines as evidence, is graded `blocker` or `major`, is
classed as an untested requirement (or a test finding for the test
engineer), and states what the test engineer receives in contract terms
only: R3, an upstream whose `Fetch` returns an error wrapping
`ErrNotFound`, and the expected outcome (`ErrUnknownSKU`, and a second
`Get` within `negativeTTL` returns `ErrUnknownSKU` without calling `Fetch`).
The part addressed to the test engineer must not include line numbers,
unexported identifiers written as code (such as `c.entries`,
`entry.missing` or `fetchedAt`), or profile data; plain words such as
"missing" are fine.
FAIL if no finding names the missing R3 test, the finding is `minor`, the
not-found blocks are classed as unrequested behaviour or as unreachable, or
the review asks for line numbers, the profile, coverage figures or
implementation source to be passed to the test engineer.
