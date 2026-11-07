---
type: llm
focus: { source: file, path: pool/pool.go }
---

PASS if the code satisfies the contract: `New` rejects size < 1 with
`ErrInvalidSize`; `Submit` blocks while all workers are busy and returns the
context's error when the caller's context ends first; `Submit` after `Close`
returns `ErrClosed`; `Close` stops new submissions, waits for running tasks
and returns the context's error if that context ends first while still
refusing new work; a repeated `Close` waits the same way and never panics; at
most `size` tasks run at once; every goroutine the pool started has exited
whenever a `Close` call returns nil.
FAIL if there is an interface, options struct, functional option, generic
parameter or configuration hook that no caller in the contract needs; if a
task panic is swallowed or converted into a retry; if `Submit` spawns an
unbounded goroutine; if a mutex-protected flag is read without the mutex; or
if any goroutine can outlive `Close`.
