# Property and metamorphic testing

Read this when a rule holds across a whole input domain, or when the correct answer can't be stated exactly. An example test checks one point. A property states a rule and lets a generator look for the counterexample.

Follow the [test-engineer contract](../../task-orchestration/references/contracts/test-engineer.md) for properties, models, generators, and corpus changes. Authors use approved contracts and interfaces, never implementation source or raw execution evidence. Missing guarantees, seams, or dependencies go to the coordinator.

That trade only pays when the contract has a shape to assert, such as an inverse, an invariant, or an approved reference to compare with. Code without one gets example tests, and that's a valid conclusion. A source-aware coordinator can check whether the shape is missing or buried under I/O and propose an approved interface; extracting production code stays outside the test author's ownership.

## Choosing a tool in Go

- **`pgregory.net/rapid`** for structured inputs. It generates typed values, shrinks failures, and has stateful model tests. `rapid.MakeFuzz` connects it to native fuzzing.
- **Native fuzzing** for coverage-guided exploration of bytes and strings. It only takes primitive arguments. See [Go fuzzing](go-fuzzing.md).
- **`testing/quick`** is frozen and doesn't shrink, so avoid it for new tests.

`rapid` is an MPL-2.0 test dependency. Adding one is the user's decision, so offer it once with the specific property you'd write. If the project already uses a property library, use that one.

## Pick the strongest property

From weakest to strongest:

1. It doesn't crash.
2. Its output has the right shape.
3. An invariant holds.
4. It's idempotent: `f(f(x)) == f(x)`.
5. It round-trips, or it matches a reference: `decode(encode(x)) == x`, or `fast(x) == slow(x)`.

"Doesn't crash" alone rarely justifies the dependency. A small independent model derived from the approved contract can be a useful oracle; a copy or adaptation of the implementation cannot supply independent expectations.

A property test can assert nothing in two ways:

- **Restating the implementation.** `Add(a, b) == a + b` shares every bug with the code. Choose a rule that constrains the function without recomputing it.
- **Filtering away the input.** A generator that rejects nearly everything passes without exercising much. Build valid values directly instead of filtering them.

```go
func TestNormalizeIsIdempotent(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.StringMatching(`[ab ]{0,12}`).Draw(t, "s")
		once := Normalize(s)
		if twice := Normalize(once); twice != once {
			t.Fatalf("not idempotent: %q then %q", once, twice)
		}
	})
}
```

For stateful code, run random operations against a simple model. Bugs in a sequence of calls rarely show up in single-call tests:

```go
func TestStackMatchesModel(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var stack Stack
		var model []int
		t.Repeat(map[string]func(*rapid.T){
			"push": func(t *rapid.T) {
				v := rapid.Int().Draw(t, "v")
				stack.Push(v)
				model = append(model, v)
			},
			"pop": func(t *rapid.T) {
				got, ok := stack.Pop()
				if len(model) == 0 {
					if ok {
						t.Fatal("pop succeeded on an empty stack")
					}
					return
				}
				want := model[len(model)-1]
				model = model[:len(model)-1]
				if !ok || got != want {
					t.Fatalf("pop = %d, %t; want %d", got, ok, want)
				}
			},
		})
	})
}
```

`rapid` saves a failing case under `testdata/rapid` and prints how to replay it. Treat those writes like fuzz corpus writes: keep a useful regression deliberately, and disclose them when the task's scope requires it.

## When a property fails

A failure means one of four things. The source-aware verifier triages raw failures and returns contract-level inputs, expected/actual outcomes, and permitted diagnostics to the author:

| Finding | What it means | Action |
| --- | --- | --- |
| The code is wrong | A stated guarantee is violated | The implementation owner fixes it; the author retains the contract regression |
| The generator is too broad | The input breaks a stated precondition | The author constrains the generator to that approved precondition |
| The property is wrong | It asserted something the contract never promised | Resolve the contract; the author corrects the property |
| The spec is ambiguous | Nobody decided this edge | Ask the coordinator; do not quietly narrow the generator |

The coordinator establishes promises from authoritative requirements and approved interfaces, reconciling documentation and existing behavior as needed. The author receives only the approved packet; existing tests, implementation outputs, and function names do not establish expected behavior.

## Metamorphic testing

Some code has no independent correct answer: a ranking, a planner, an optimizer, a numerical routine. Computing the expected output would mean writing the program twice. Freezing today's output as a golden file certifies today's bugs, and asserting no crash proves almost nothing.

So don't assert what one output is. Assert how outputs relate when the input changes in a known way.

| Relation | Rule | Example |
| --- | --- | --- |
| Invariance | Output doesn't change | Reordering unordered input, extra whitespace |
| Equivalence | Two routes give one result | Two query plans, a fast path and a slow path |
| Monotonicity | Output moves one way | Adding a matching row can't lower a count |
| Subset | A narrower query returns less | Results for "red shoes" are within those for "shoes" |
| Inverse | A round trip returns the input | `Parse(Format(x)) == x` |
| Conservation | A total is preserved | Debits and credits balance after any transfer |

```go
func TestTotalIgnoresOrder(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		xs := rapid.SliceOf(rapid.IntRange(-1000, 1000)).Draw(t, "xs")
		reversed := slices.Clone(xs)
		slices.Reverse(reversed)
		if Total(xs) != Total(reversed) {
			t.Fatalf("order changed the total for %v", xs)
		}
	})
}
```

Find relations by asking these in order:

1. What input change should change nothing? These are the strongest and easiest.
2. What undoes this operation?
3. What change should move the output in a known direction?
4. What are two different ways to compute the same thing?
5. What does the domain guarantee for any input, such as totals that must match?

A relation can hold on broken code, since a function returning a constant satisfies many invariance relations. Combine relations, and keep example tests wherever an exact answer exists. Float relations need a tolerance justified by the approved numerical contract; ask for a missing bound instead of tuning it to observed output.

_Property test failure triage and the strength ordering draw on ideas from `trailofbits/skills`, restated here. The metamorphic section is adapted from `coding-skills` by auralshin and rewritten for Go._
