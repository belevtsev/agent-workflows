---
name: go-principal-engineer
description: Resolve consequential Go ownership, lifecycle, durability, or rollout decisions with failure behavior and checks.
---

# Production Go decisions

This is the Go extension of `principal-engineer`. First read `../principal-engineer/SKILL.md`, next to this skill. It holds the language-neutral judgment: failure modes, ownership and lifecycle, durability, rollout and rollback, compatibility, consultation and verification. This file adds only what Go changes about applying it. A routine local change needs neither.

## Goroutines and cancellation

Every goroutine has an owner that waits for it and receives its error. A `go` statement with nothing waiting is fire-and-forget: it leaks on shutdown and drops its failure.

```go
// run starts the workers and the server, and returns when all of them have
// stopped. Cancelling ctx stops everything; the first error cancels the rest.
func run(ctx context.Context, srv *http.Server, jobs <-chan Job) error {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	var wg sync.WaitGroup
	for range 4 { // bounded: four workers, never one per job
		wg.Go(func() {
			if err := work(ctx, jobs); err != nil {
				cancel(err)
			}
		})
	}
	wg.Go(func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			cancel(err)
		}
	})
	var shutdownErr error // written by one goroutine, read after wg.Wait
	wg.Go(func() {
		<-ctx.Done()
		shutdownCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer done()
		// ctx is already cancelled here, so cancel(err) would be ignored:
		// keep the error and return it.
		shutdownErr = srv.Shutdown(shutdownCtx)
	})
	wg.Wait()
	cause := context.Cause(ctx)
	if errors.Is(cause, context.Canceled) {
		cause = nil // a normal stop, not a failure
	}
	return errors.Join(cause, shutdownErr)
}
```

- Pass `context.Context` as the first argument. Never store it in a struct. Every blocking call in the goroutine selects on `ctx.Done()` or takes `ctx` (`golang-context`).
- Use `signal.NotifyContext` in `main` to turn SIGINT and SIGTERM into cancellation, and bound shutdown with its own timeout derived through `context.WithoutCancel`.
- The sender closes a channel, and only one sender does. Stop every `time.Ticker`, and `Stop` a `time.Timer` whose result you no longer need.
- Bound fan-out with a fixed worker count or a semaphore; a goroutine per request item is unbounded memory (`golang-concurrency`).

## Resources and errors

- `defer f.Close()` is right for files you only read. For a file you wrote, check the `Close` error: a write can fail there. Join it into the returned error with `errors.Join`, not a log line (`golang-error-handling`).
- `rows.Close`, `resp.Body.Close` and `tx.Rollback` belong to the function that opened them, in a `defer` right after the error check. Check `rows.Err()` after the loop (`golang-database`).
- Open input-derived paths through `os.Root` (Go 1.24+), which refuses `..` and symlinks that leave the directory. `filepath.IsLocal` only checks the text of the path, so it stops `..` but not a symlink inside the directory that points out of it.

## Durable writes

The neutral rule is temp file, flush, rename, flush the directory. In Go:

```go
// writeFileAtomic replaces path with data so that a crash leaves either the
// old file or the new one, never a partial write.
func writeFileAtomic(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	renamed := false
	defer func() {
		if err != nil && !renamed { // after the rename, the temp name is gone
			err = errors.Join(err, os.Remove(f.Name()))
		}
	}()
	if _, err := f.Write(data); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Chmod(perm); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(f.Name(), path); err != nil {
		return err
	}
	renamed = true
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	return errors.Join(d.Sync(), d.Close())
}
```

`os.WriteFile` truncates in place and is not crash-safe for state you must keep.

## References

When a change crosses service, persistence, schema, or authority boundaries, read [backend ownership boundaries](references/backend-boundaries.md). When durable state and external effects can diverge under duplicate work, timeouts, crashes, or ownership transfer, read [distributed workflows](references/distributed-workflows.md). When work can be retried or redelivered, read [idempotency keys](references/idempotency.md). When a pool, queue, or timeout needs a number, read [capacity and tail latency](references/capacity-and-latency.md). Load these references only for the affected decision.

Verify Go concurrency changes with `go test -race`, and prefer channels or hooks that signal an event over `time.Sleep` in tests (`golang-testing`).
