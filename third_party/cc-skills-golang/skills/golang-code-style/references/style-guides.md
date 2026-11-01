# Decisions from the Go style guides

These are the judgment calls from the Google and Uber Go style guides that linters can't make.

## When rules conflict

Google's guide ranks readability attributes, in order: clarity, simplicity, concision, maintainability, consistency. Use the order to settle a disagreement. Consistency matters, but it doesn't justify a less clear version of new code, and concision never outranks clarity.

## Least mechanism

When several approaches work, use the least sophisticated tool that does the job, because each step up adds something a reader must understand. Prefer core language constructs such as a slice, a map, a loop, or a channel. Then the standard library. Reach for a new dependency only when it's clearly justified.

## No in-band errors

Don't signal failure with a special value of the normal result, because callers forget to check and the value flows on as if it were real.

```go
// ✗ Bad — "" might be a real value, and nothing forces a check
func Lookup(key string) string { return "" }

// ✓ Good — the second result can't be ignored by accident
func LookupValue(key string) (value string, ok bool) { return "", false }
```

## Exit once, in main

Calling `os.Exit` or `log.Fatal` outside `main` makes code hard to test and skips deferred cleanup. Keep one exit, and put the real work in a function that returns an error:

```go
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	file, err := os.Open("config.yaml")
	if err != nil {
		return fmt.Errorf("open config: %w", err)
	}
	defer file.Close() // an exit in here would skip this
	return nil
}
```

## Use time types

Use `time.Time` for instants and `time.Duration` for periods, including in function arguments and struct fields, because a bare `int` leaves its unit for the caller to guess. `Poll(10)` could mean seconds or milliseconds, and `Poll(10 * time.Second)` can't.

Where a format can't carry a duration, put the unit in the name, as in `IntervalMillis`.

_Drawn from the [Google Go style guide](https://google.github.io/styleguide/go/), CC-BY-3.0, and the [Uber Go style guide](https://github.com/uber-go/guide), Apache-2.0, restated here._
