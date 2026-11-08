---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: requirement R3 has no test. The 410 Gone branch of
`Deliver` (`dispatch/dispatch.go:72-76`), which calls `Disable` and returns
`ErrGone`, is implemented, but the coverage profile
`testdata/checks/cover.out` shows its blocks (`dispatch.go:73.3,73.56`,
`:74.4,75.1` and `:76.3,76.46`) with count 0: no test in
`dispatch_test.go` answers 410. Under the code-review contract this is an
untested requirement: a test finding for a fresh test engineer, at least
`major`, whose summary carries only the requirement ID, contract-level
inputs and the expected outcome.

PASS if one finding says R3 has no exercising test, cites the unexecuted
410 blocks or the profile lines as evidence, is graded `blocker` or
`major`, is classed as an untested requirement (or a test finding for the
test engineer), and states what the test engineer receives in contract
terms only: R3, an endpoint that answers 410 (and optionally a `Disable`
that fails), and the expected outcome (`Disable` called once with the
endpoint ID, an error wrapping `ErrGone`, and wrapping the `Disable` error
when that fails). The part addressed to the test engineer must not include
line numbers, unexported identifiers written as code (such as
`fallbackSecret` or `d.disabler`), or profile data. The contract's own names,
such as `New`, `Deliver`, `Disabler` and its `disabler` parameter, are
contract terms and allowed.
FAIL if no finding names the missing R3 test, the finding is `minor`, the
410 branch is classed as unrequested behaviour or as unreachable, or the
review asks for line numbers, the profile, coverage figures or
implementation source to be passed to the test engineer.
