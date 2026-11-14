# Building blocks

Use this when writing new code, so it composes from small, deep, reusable pieces
that are each testable on their own. It complements the deep-module rules in
[SKILL.md](../SKILL.md): a building block is a deep module chosen so that other
code can assemble it, not a layer added for its own sake.

## Reuse before writing

Search the repository for an existing function, type or package that already
does the job, or almost does: `rg` the verb and the noun, read the package that
owns the concept, check the standard library. Reuse it, or extend it when the
change is general enough to serve its other callers. Write a new block only when
nothing fits, and say why in one sentence. Two near-identical blocks are a
defect: one of them is information leakage waiting to drift.

## Four kinds of block

Sort each piece of new code into one of these. The kind decides where it lives,
what it may depend on and how it is tested.

| Kind | Does | May depend on | Tested by |
|---|---|---|---|
| **Value** | Holds data that is valid by construction | other values | constructor and method tables |
| **Decision** | Computes a result from values; no I/O, no clock, no randomness | values, other decisions | table and property tests |
| **Adapter** | Talks to one outside thing: file, process, network, database, clock | values, the outside thing | integration test against a real local stand-in |
| **Composition** | Wires blocks together for one use case; owns lifecycle and errors | everything above, passed in | a few end-to-end tests through the built artefact |

Most bugs and most logic belong in decisions, which are the cheapest to test.
Keep adapters thin: translate, call, translate back. Keep compositions short:
if one grows branches, a decision is hiding in it.

## Make blocks composable

- **Explicit dependencies.** A block receives what it needs as parameters or
  constructor arguments. No package-level mutable state, no work in `init`, no
  reaching for globals, environment variables or the clock from inside a
  decision.
- **Small consumer-side interfaces.** The block that needs a capability
  declares the interface, with only the methods it calls. Accept the standard
  shapes where they fit (`io.Reader`, `io.Writer`, `fs.FS`, `iter.Seq`,
  `context.Context`), because every existing producer and consumer then works
  with it.
- **Return values, not effects.** A decision returns what should happen; the
  composition performs it. `Plan(state) []Action` composes and tests better
  than `Apply(state *State)`.
- **Configuration is a value.** Options arrive as a typed struct validated at
  the boundary, not as flags read deep inside.
- **One reason to change.** A block owns one piece of knowledge. If a format,
  rule or protocol change would edit two blocks, merge the knowledge into one.
- **Stable names.** Name a block after the secret it keeps (`retryBudget`,
  `manifestReader`), never after its position (`helper`, `manager`, `utils`).

## When to extract a block

Extract when a second real caller needs the behaviour, or when separating a
decision from an effect makes the decision testable with a table. Do not extract
for a hypothetical caller, and do not add an interface with one implementation
unless the second implementation is the test's stand-in for an outside thing.

## Shape

```go
// Decision: pure, table-testable.
func nextAttempt(b Budget, failures int, now time.Time) (time.Time, bool)

// Adapter: thin, integration-tested against a real local stand-in.
type httpFetcher struct{ client *http.Client }
func (f httpFetcher) Fetch(ctx context.Context, url string) ([]byte, error)

// Composition: wires blocks, owns lifecycle; covered end to end.
func Sync(ctx context.Context, f Fetcher, s Store, clock func() time.Time, b Budget) error
```

`Sync` takes `Fetcher` and `Store` as small interfaces it declares, and the
clock as a function, so a test can drive it with in-memory stand-ins and a
fixed time, while `nextAttempt` needs no stand-ins at all.
