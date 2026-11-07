# Interface contract: package `pool`

A fixed-size worker pool that runs submitted tasks on at most `size`
goroutines. Language: Go. Module `example.com/pool`, package `pool`.

## Public interface

```go
package pool

var ErrInvalidSize = errors.New("pool: size must be at least 1")
var ErrClosed = errors.New("pool: closed")

type Pool struct { /* unexported */ }

func New(size int) (*Pool, error)
func (p *Pool) Submit(ctx context.Context, task func(ctx context.Context)) error
func (p *Pool) Close(ctx context.Context) error
```

## Requirements

- R1: `New(size)` returns `ErrInvalidSize` when `size < 1`; otherwise a pool
  whose workers are started lazily or eagerly, at the implementer's choice.
- R2: `Submit` runs `task` on one of the pool's workers and returns nil once
  the task has been accepted. While all `size` workers are busy, `Submit`
  blocks until one is free.
- R3: If `ctx` is done before the task is accepted, `Submit` returns
  `ctx.Err()` and the task never runs.
- R4: After `Close` has been called, `Submit` returns `ErrClosed`, even while
  `Close` is still waiting for running tasks.
- R5: `Close(ctx)` prevents new submissions, waits for every accepted task to
  finish, then returns nil. If `ctx` is done first, `Close` returns `ctx.Err()`
  but the pool stays closed and running tasks still finish on their own.
- R6: `Close` may be called again at any time; a later call behaves like the
  first (waits for the remaining accepted tasks, or returns `ctx.Err()`) and
  never panics or starts new work.
- R7: At most `size` tasks run concurrently, and when any `Close` call returns
  nil, every goroutine the pool started has exited.

## Invariants and scope

- The context passed to `task` is the `Submit` context.
- A task that panics terminates the process; recovering is out of scope.
- No metrics, no dynamic resizing, no priority ordering.
