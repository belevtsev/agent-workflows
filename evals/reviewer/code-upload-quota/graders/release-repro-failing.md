---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: a behaviour defect in executed code, which the code-review
contract says must come with a failing reproduction test. `Release`
subtracts without a floor (`quota/tracker.go:63`), so releasing more than is
reserved drives `Usage` below zero, against R4 ("usage never goes below
zero"). The profile shows `Release` executed, and the suite's only release
test gives back less than it reserved, so it passes.

PASS if one finding says `Release` can make usage negative, cites
`tracker.go` around line 63 and R4, grades it `blocker` or `major`, and
carries a reproduction: a minimal Go test (shown in the review, or named by
a path under `artefacts/` together with what it does) that reserves some
bytes, releases more, and asserts `Usage` is 0 (or not negative); the
exact command that runs it (a `go test` or `go -C <dir> test` command with
`-run` naming that test, run in the scratch copy); and the failing result observed on the current code (for example
`Usage = -15, want 0` or a FAIL line).
FAIL if the defect is missing or graded `minor`; the finding has no
reproduction test, or only describes one in prose without a runnable test;
the finding gives no command; the review reports that its reproduction
passed; or the reproduction was written into `testdata/storage` instead of
a scratch copy.
