---
name: codebase-design
description: Design or deepen module interfaces, choosing seams and removing shallow layers so behavior stays testable.
license: MIT
---

# Deep module design

Use this when shaping a package, type, or function interface, deciding whether a layer earns its place, or when tests have to reach past an interface. For ownership and dependencies across components, use `software-architecture`. For where validation belongs, use `parse-dont-validate`.

Aim for deep modules: a lot of behavior behind a small interface, placed at a deliberate seam, and testable through that interface. Depth gives callers leverage, because one implementation pays back across every call site and test. It gives maintainers locality, because a change, a bug, or a piece of knowledge lives in one place.

## Vocabulary

Use these terms consistently, because shared language keeps design discussion precise.

- **Module:** anything with an interface and an implementation, from a function to a package to a vertical slice.
- **Interface:** everything a caller must know to use the module correctly, not just its signature. That includes invariants, ordering, error modes, required configuration, and performance characteristics.
- **Depth:** the behavior a caller or test can exercise per unit of interface they must learn. A module is shallow when its interface is nearly as complex as its implementation.
- **Seam:** a place where behavior can change without editing that place. Where the seam goes is its own decision, separate from what sits behind it.
- **Adapter:** a concrete implementation that fills a seam, such as a Postgres store or an in-memory fake.

## Tests for a design

**The deletion test.** Imagine deleting the module. If complexity vanishes, it was a pass-through. If the complexity reappears across several callers, the module was earning its keep.

**Name what callers stop knowing.** "Nothing, it's just organized differently" means this is not an abstraction. Write the interface out in full, including errors, ordering rules, configuration, and side effects. A long list means a shallow module, however clean the code looks.

**The interface is the test surface.** Callers and tests should cross the same seam. A test that has to reach past the interface usually means the module is the wrong shape. Tests written this way survive refactors, because they describe behavior rather than implementation.

**One adapter is a hypothetical seam. Two adapters make a real one.** An interface with a single implementation adds indirection and hides nothing. Introduce one when something actually varies across it, typically production and test.

## Shape

A shallow interface makes callers do the module's work:

```go
// Shallow: callers must sequence the steps and remember to flush and close.
type RowExporter interface {
	Open(ctx context.Context, path string) error
	WriteHeader(h Header) error
	WriteRow(r Row) error
	Flush() error
	Close() error
}

// Deep: one entry point hides ordering, buffering, and cleanup.
func Export(ctx context.Context, w io.Writer, rows iter.Seq2[Row, error]) error {
	return nil
}
```

Accept dependencies instead of creating them. Define the interface where it's consumed and keep it small, because a test then needs only the methods this module uses.

```go
// Charger is declared by the package that needs it, not the payment package.
type Charger interface {
	Charge(ctx context.Context, amount Money) (ChargeID, error)
}

func Checkout(ctx context.Context, o Order, c Charger) (Receipt, error) {
	id, err := c.Charge(ctx, o.Total)
	if err != nil {
		return Receipt{}, fmt.Errorf("charge order %s: %w", o.ID, err)
	}
	return Receipt{Order: o.ID, Charge: id}, nil
}
```

Return results rather than producing side effects where you can. `Discount(cart) Money` can be tested with a table. `ApplyDiscount(cart *Cart)` needs state setup and inspection for every case.

Prefer an interface slightly more general than today's need, with special cases on top. A small, fundamental operation is both simpler and more capable than one method per case. This isn't speculative generality, because no unused feature is added.

## Red flags

- **Pass-through methods.** A method that calls one with the same signature a layer down adds interface and hides nothing. Expose the lower layer, merge the two, or make the wrapper do real work. One thin adapter owned for testing or vendor isolation is a small real benefit. Three stacked ones aren't.
- **Pass-through parameters.** A value threaded through five functions because only the fifth uses it puts that concept in every interface along the way. Don't move it into `context.Context` to hide it, because that makes the dependency invisible rather than removing it.
- **Information leakage.** The same design decision appears in more than one module, so a format change touches both. Give one module ownership of that knowledge. This is the deepest form of coupling and the one worth hunting.
- **Temporal decomposition.** Splitting by when things happen (read, process, write) instead of by what knowledge each part holds usually causes that leakage, because the same knowledge is needed at several times.
- **Interfaces that mirror implementation.** One method per internal step is a to-do list, and callers must get the order right.
- **Names with no secret.** `utils`, `common`, `manager`, and `helper` usually mark a module that hides nothing coherent.

## Splitting

Small functions and small types optimize only the implementation side. Applied without the interface cost, they relocate complexity into the relationships between many tiny parts. Split when the pieces have separate interfaces and a caller needs only one. Don't split when the pieces are only usable together, because that adds an interface and hides nothing. After a proposed split, ask whether either half can be understood and used without reading the other.

## Define errors out of existence

The most reliable way to deepen a module is to reduce the ways it can fail. `os.RemoveAll` returns nil when the path doesn't exist, and `delete(m, k)` is a no-op for a missing key, so neither adds a branch to every caller. Each incidental error removed deletes a branch from every call site. Keep errors that matter: a failed payment must surface. Target only errors where the caller's sane response is to ignore them or do the obvious thing.

## Going further

- When writing new code, read [building blocks](references/building-blocks.md). It covers reuse before writing, the four kinds of block (value, decision, adapter, composition), how each is tested, and when to extract one.
- To deepen an existing cluster of shallow modules, read [deepening](references/deepening.md). It covers dependency categories, seam discipline, and which tests to replace.
- To compare genuinely different interfaces before committing to one, read [design it twice](references/design-it-twice.md).

Apply this at design and code review, before an interface ships. Retrofitting depth means changing every call site.
