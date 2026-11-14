# Silent failures

Use this when reviewing error paths, fallbacks, or retries. A silent failure is one where something went wrong and the caller, the user, or the operator can't tell. These cost the most, because they surface later, somewhere else, as wrong data.

Logging is handling only when the function's contract still holds afterwards. An error that's logged and dropped from a function promising a result is hidden, not handled.

## Patterns that hide errors

```go
func SaveAll(ctx context.Context, items []Item, store Store) error {
	for _, item := range items {
		if err := store.Save(ctx, item); err != nil {
			// ✗ The caller sees success while items are missing
			slog.Error("save failed", "item", item.ID, "err", err)
			continue
		}
	}
	return nil
}
```

Partial success needs to be visible. Return the failures joined with `errors.Join`, or a result that says which items failed.

```go
func Limit(cfg Config) int {
	v, err := strconv.Atoi(cfg.Limit)
	if err != nil {
		return 100 // ✗ A typo in config now runs with a limit nobody chose
	}
	return v
}
```

Look for these in Go:

- `_ = f()` on anything that can fail, and an ignored `ok` from a type assertion or map lookup that matters.
- `defer f.Close()` on a file being written. A failed close can mean lost data, so check it where durability matters.
- A goroutine's error that goes nowhere. Use `errgroup`, or a channel someone reads.
- `recover` that logs and continues, leaving state half-changed.
- A zero value returned with a nil error on a failure path.
- Treating `context.Canceled` as success, or the reverse, without deciding which the caller needs.
- Retries that exhaust and then return nil. Return the last error, wrapped, with the attempt count.
- A broad check on one error, such as `if err != nil { return defaultValue }`, covering failures it was never meant to, like a permissions error treated as "not found".

## Justify every fallback

Before accepting a fallback, answer these:

1. Did someone ask for it, or does the spec define it? If not, return the error.
2. Will anyone notice it happened? A fallback that's never measured hides an outage. Count it, and log it at a level someone reads.
3. Does the caller know it got degraded data? If the difference matters, say so in the result.
4. Does it fall back to a stub or fake outside tests? That's never acceptable.

Name what a broad handler could hide, so you can tell whether each case belongs there. If you can't list them, narrow it with `errors.Is` or `errors.As`.

## Messages

An error should say what operation failed and on what, so someone can act on it. `open config: permission denied` works and `operation failed` doesn't. Keep internal detail out of messages that reach end users.

_Adapted from `silent-failure-hunter` in `pr-review-toolkit` by Anthropic, and rewritten for Go._
