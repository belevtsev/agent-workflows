# Test design

Read this when writing or changing tests, or adding fakes or test helpers. A test exists to catch a specific break, so it has to name that break and exercise the real thing.

## Name the break

Before writing the test body, name the production change that should make it fail, and decide whether that change is a bug or a decision.

| Answer | Action |
| --- | --- |
| You can't name a change | Redesign around an observable behavior |
| Only the source text changed | Run the code and assert its effects |
| Only an intentional decision changed | This is a change detector. Test the behavior that depends on the decision |

**Derive expectations independently.** Use literals and hand-checked fixtures. An expected value computed by the code under test passes no matter what that code does:

```go
func TestBuildQuery(t *testing.T) {
	// ✗ Mirror assertion: the same function computes both sides, so it can't fail
	want := BuildQuery(Filter{Tag: "urgent"})
	if got := BuildQuery(Filter{Tag: "urgent"}); got != want {
		t.Errorf("BuildQuery() = %q, want %q", got, want)
	}

	// ✓ A hand-derived literal fails when the query changes
	if got, want := BuildQuery(Filter{Tag: "urgent"}), `tag:"urgent"`; got != want {
		t.Errorf("BuildQuery() = %q, want %q", got, want)
	}
}
```

**No change detectors.** A test on a constant's value, exact wording, or private structure fires on redesign and sleeps through bugs. Test what depends on the decision:

```go
func TestRetryStopsAtLimit(t *testing.T) {
	// ✗ if MaxAttempts != 5 only restates the constant
	// ✓ A failing call runs five times, and the sixth attempt never happens
	calls := 0
	err := Retry(func() error {
		calls++
		return errUnavailable
	})
	if !errors.Is(err, errUnavailable) {
		t.Fatalf("Retry() = %v, want errUnavailable", err)
	}
	if calls != 5 {
		t.Errorf("calls = %d, want 5", calls)
	}
}
```

**Test your code, not the framework.** Test the contract at your boundaries: the route you register, the query you emit, the payload you produce. Whether `net/http` calls a registered handler is the standard library's test. Constructors, getters, and trivial forwarding earn a test only when they validate, normalize, default, or cause side effects. Otherwise assert the first caller-visible result that depends on them.

## Exercise the real thing

**The fake earns no assertions.** An assertion that a fake was called proves the fake exists. Assert what the real component did with the result. If the fake is what you're checking, remove the assertion or the fake.

**Replace at the right level.** List the real method's side effects first. Replace only the slow or external operation below them, and keep real anything the test depends on. A fake that swallows a write the code later reads makes the test pass for the wrong reason.

**Keep doubles specific.** When arguments, call counts, or ordering are part of the contract, assert them. Give success, error, and malformed cases their own fixture, so the wrong branch can't satisfy the expectation.

**Mirror real data completely.** Build fixtures with every field the real value has. Partial fixtures fail silently when code reads a field you omitted, so the test passes while the integration breaks.

**Keep test-only code out of production.** A method called only from tests belongs in test utilities. `export_test.go` can reach internals without widening the public API.

**Prefer real components over complex fakes.** When fake setup outgrows the test logic, switch to an integration test with the real dependency.

## Mutation check

Before finishing, mentally change the production code. At least one test should fail for each realistic change:

- A wrong constant or argument.
- A wrong branch, or a comparison off by one at a limit.
- A missing state change or side effect.
- An empty or zero return, or a nil error on a failure path.
- Missing handling for zero, empty, nil, unauthorized, or malformed input.

A change nothing catches marks unprotected behavior or a tautological test. Run a real [mutation check](mutation-testing.md) when this doubt matters.

## Warning signs

- Setup and assertion share one object, which guarantees equality.
- The test can only fail through a panic.
- It fails on every intentional change and never on an accident.
- Expected values are hidden behind loops or helpers.
- It exists for coverage and checks no outcome.
- Fake setup is more than half the test, or you can't say why the fake is needed.

_Adapted from `writing-good-tests.md` in `obra/superpowers`, and rewritten for Go._
