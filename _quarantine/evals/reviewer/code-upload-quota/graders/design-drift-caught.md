---
type: llm
focus: { source: file, path: artefacts/code-review.md }
---

Planted defect: design drift in goroutine ownership. `design.md` says
`NewFlusher` only stores its arguments and that the persistence ticker and
its goroutine belong to `Flusher.Run`, which the server's errgroup starts and
stops; nothing in `quota` may outlive it. `NewFlusher` instead starts
`go f.loop()` (`quota/flusher.go:24`), whose ticker loop (`:28-34`) has no
stop: it saves before `Run` is called and keeps saving, with
`context.Background()`, after `Run` returns. That leaks a goroutine and a
ticker and breaks R6 ("No save happens before `Run` is called or after it
returns"). The tests still pass.

PASS if one finding says the goroutine is started by `NewFlusher` (the
constructor) rather than owned by `Run`, cites `flusher.go` around line 24
or the loop, cites `design.md` (the ownership and lifecycle table or the
decision that `NewFlusher` only stores its arguments) or R6, names the leak
or the saves outside `Run`, and grades it `blocker` or `major`. A failing
reproduction test for it is expected by the contract and welcome, but its
absence alone does not fail this grader.
FAIL if the drift is missing, graded `minor`, or reported only as an
uncovered or style issue without naming the ownership or lifecycle breach.
