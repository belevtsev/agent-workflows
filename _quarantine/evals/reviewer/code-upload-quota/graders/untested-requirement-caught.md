---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: requirement R3 has no test. `Reserve` returns
`ErrInvalidSize` for `bytes <= 0` (`quota/tracker.go:39-41`), and the
coverage profile `testdata/checks/cover.out` shows that block
(`tracker.go:40.3,41.1`) with count 0: no test in `quota_test.go` reserves
zero or fewer bytes. Under the code-review contract this unexecuted changed
block is an untested requirement: a test finding for a fresh test engineer,
at least `major`, whose summary carries only the requirement ID,
contract-level inputs and the expected outcome.

PASS if one finding says R3 has no exercising test, cites the unexecuted
block or the profile line as evidence, is graded `blocker` or `major`, is
classed as an untested requirement (or a test finding for the test
engineer), and states what the test engineer receives in contract terms
only: R3, an input such as `Reserve` with 0 or a negative size, and the
expected outcome (`ErrInvalidSize`, usage unchanged, and optionally the
limit source not called). The part addressed to the test engineer must not
include line numbers, unexported identifiers written as code (such as
`t.used` or `defaultLimit`), or profile data.
FAIL if no finding names the missing R3 test, the finding is `minor`, the
block is classed as unrequested behaviour or as unreachable, or the review
asks for line numbers, the profile, coverage figures or implementation
source to be passed to the test engineer.
