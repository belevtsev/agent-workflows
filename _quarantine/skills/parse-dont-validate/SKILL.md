---
name: parse-dont-validate
description: Move checks to input boundaries by returning narrower Go types so invalid states stop spreading.
license: MIT AND Apache-2.0
---

# Parse, don't validate

Use this when deciding where validation belongs, when the same check repeats along a call path, when a type allows states that should be impossible, or when reviewing a new domain type.

A validator checks a condition and throws the result away, so every function downstream must check again or hope. A parser checks and returns a value carrying the proof, so code holding that value can't be wrong about it.

> Validation returns a boolean. Parsing returns a narrower type.

## The Go recipe

Use a struct with an unexported field, built only through a constructor. A defined string type doesn't work, because any package can write `Email("nonsense")`.

```go
// Email is a parsed address. The unexported field means no other package can
// build one without going through ParseEmail.
type Email struct{ value string }

func ParseEmail(raw string) (Email, error) {
	addr, err := mail.ParseAddress(raw)
	if err != nil {
		return Email{}, fmt.Errorf("parse email: %w", err)
	}
	return Email{value: addr.Address}, nil
}

func (e Email) String() string { return e.value }
```

Two rules make this work. **Keep one construction path,** because a second way to build the value removes the guarantee. **Parse at the outermost edge:** the HTTP handler, the queue consumer, the CLI flag, the row mapping.

Go can't forbid the zero value, so `Email{}` always exists. Make it obviously unusable, or valid by design. Decide which, and test code paths that could receive it, for example a struct field that was never set.

Decoding can bypass your constructor. Implement `encoding.TextUnmarshaler` so JSON goes through the parser, and `sql.Scanner` so rows do:

```go
func (e *Email) UnmarshalText(text []byte) error {
	parsed, err := ParseEmail(string(text))
	if err != nil {
		return err
	}
	*e = parsed
	return nil
}
```

## Parse first, then act

Shotgun parsing interleaves checks with effects, so the program learns the input was bad after it has already acted on part of it. That produces half-written rows and half-charged customers.

```text
input -> parse: all checks, no effects -> valid value -> act: no checks, all effects
```

Any check that can reject input after the first side effect is shotgun parsing. Parse a whole batch before opening the transaction, not row by row inside it. This is the same shape as a functional core and an imperative shell. Errors from the effects themselves, such as a failed write, still need handling.

## Where parsing belongs

```text
transport handler   parse here: raw input to domain types
  service           already typed, no re-checking
    domain logic    total functions only
      repository    parse rows back into domain types
```

**The database is an input boundary too.** A fetched row is untrusted, because old rows were written by older code under weaker rules. Parse rows in the repository and return an error that names the row, so one bad record fails clearly rather than corrupting logic further in.

## Make illegal states unrepresentable

A struct whose optional fields are only valid in certain combinations allows states nothing should handle:

```go
// Allows Status "settled" with no reference, or a failure reason on a pending payment.
type PaymentRecord struct {
	Status    string
	Reference string
	Reason    string
}
```

Model the real states instead. An interface with an unexported method closes the set to other packages:

```go
type PaymentState interface{ paymentState() }

type Pending struct{}
type Settled struct{ Reference string }
type Failed struct{ Reason string }

func (Pending) paymentState() {}
func (Settled) paymentState() {}
func (Failed) paymentState()  {}
```

Go doesn't check exhaustiveness, so a `switch` over these needs a `default` that returns an error, and a new state won't fail to compile. A linter such as `exhaustive` can cover enums. Use this for states with different data. A plain enum is enough when states carry nothing extra.

Each impossible state removed is a defensive branch you never write.

## Reviewing a type

Find the invariants first: consistency between fields, valid transitions, and business rules. Then check each one:

- **Can it be broken from outside?** Look for exported fields and for methods returning internal slices or maps. Return `slices.Clone` or `maps.Clone`, because a caller holding your slice can change your state.
- **Is it enforced at construction?** Invariants that live only in comments aren't enforced.
- **Is every mutation guarded?** One unguarded setter defeats the constructor.
- **Does the rule prevent a real bug?** A guarantee costs a type. Weigh it against the maintenance it adds, and prefer a simpler type with fewer guarantees over one trying to do too much.

Types with too many responsibilities and types relying on callers to keep them consistent are the usual findings.

## Signs of validating where parsing belongs

- The same `== ""` or `== nil` check in several functions along one path.
- A comment saying a value "has already been validated".
- A function taking `string` or `map[string]any` and immediately narrowing it.
- `panic` for a business rule below the boundary.
- A request type with many optional fields where only certain combinations are legal.

## When not to apply it

- A value with one call site, or a short script. One caller means a check is enough.
- Input you don't own the rules for. Be strict about the fields you depend on and permissive about the rest, because parsing too strictly rejects valid input. Preserve unknown protobuf fields rather than rejecting them.
- Every string in the program. Wrap values with a real invariant, not all of them.

_Adapted from `coding-skills` by auralshin and `type-design-analyzer` from `pr-review-toolkit` by Anthropic, and rewritten for Go._
