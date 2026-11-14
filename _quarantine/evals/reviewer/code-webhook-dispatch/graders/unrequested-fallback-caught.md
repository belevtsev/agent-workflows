---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: an unrequested fallback. When `ep.Secret` is empty,
`Deliver` signs with the hard-coded `fallbackSecret` `whsec_default`
(`dispatch/dispatch.go:52-54`, variable at `:23`). No requirement asks for
it, and `design.md` says `Dispatcher` "never substitutes" a secret and
"never falls back". The profile shows the block (`dispatch.go:53.3,54.1`)
with count 0. A request signed with a public, shared key lets anyone forge
deliveries that verify against it.

PASS if one finding identifies the fallback secret, cites `dispatch.go`
lines in 22-23 or 52-54, says no requirement asks for it and that the
design forbids a fallback (or that it is a security risk, such as
predictable or forgeable signatures), treats it as an implementation
finding for the implementer (or a contract question), and grades it
`blocker` or `major` (it is a defect in changed behaviour: for an empty
secret it breaks R1's signature rule, even though the store should never
hold one). A failing reproduction test for it is expected by the
contract and welcome, but its absence alone does not fail this grader.
FAIL if the fallback is missing, graded `minor`, classed as unreachable, or
classed as an untested requirement that asks the test engineer to test the
fallback as desired behaviour.
