# Verification for a Go refactor

Choose evidence for the actual affected paths and the claim of behavior preservation. Use the repository's required environment and checks; respect an explicit test prohibition. These examples do not impose a global coverage target or a full test suite on every transform.

## What changes the verification needed

- A local semantic rename with understood callers may need diagnostics and existing focused checks. Inspect tags, templates, reflection, generated code, and consumers the tool cannot see.
- A branch, callback, error, cleanup, or state-transition rewrite needs evidence that covers the changed observable behavior. Existing tests may already provide it; add a meaningful characterization or regression test when they do not.
- A package or API move needs import-cycle, initialization, method-set, serialization, and affected-consumer evidence. Local workspace success may hide a published-module mismatch.
- Concurrency changes need ordering, cancellation, and lifetime evidence; run race or integration checks when they address the risk and are permitted.
- A hot-path change needs comparable correctness and performance evidence. Statistical significance is not itself a correctness verdict; assess magnitude and the performance contract.

Coverage helps locate unexercised statements. It does not establish branch coverage, useful assertions, or external compatibility. Inspect the relevant paths instead of deriving permission to refactor from a percentage. When using `-coverpkg`, scope it to the affected packages and understand how untested dependencies appear in the report.

Characterization tests capture existing behavior for a structural change. If that behavior includes a known bug, distinguish preserving it from authorizing a separate behavior change. Avoid large refactors of an uncertain critical path when a smaller change meets the request.

## Seams — What to Introduce When There's No Net Yet

- A seam, in Michael Feathers's sense, is a place in the code where you can alter behavior without editing that exact spot.
- Seams are how Feathers mode gets a fake into a test without first performing the larger refactor the test is meant to protect against.
- Two seam types matter in Go:
  - An **object seam** is an interface, or a function-typed field or parameter, injected at the point of construction — a test substitutes a fake implementation through that injection point instead of exercising the real dependency. This is the seam type that matters most in Go, because interfaces are satisfied implicitly: introducing one at the point of use requires touching only the consumer, never the producer package, which means you can add a seam to legacy code without an invasive edit to whatever it depends on.
  - A **link/build-tag seam** swaps an entire implementation at build time via `//go:build` constraints; it's used far more rarely, mostly for platform- or environment-specific substitutions where an interface would be overkill.
- The enabling move for untested code with no seam yet: extract the smallest possible interface — often just one method — at the exact call site where the untested code depends on something external (a database client, the filesystem, a clock), and inject the concrete implementation through a constructor parameter instead of constructing it inline.
  - This single move does two things at once: it breaks a potential import cycle between the consumer and whatever concrete type it depended on, and it opens the door for a fake in a characterization test, without requiring any change to the producer side at all.

```go
// Before — no seam: NewReport constructs its own client, so a test
// exercising Generate has no way to substitute a fake and is stuck
// hitting a real database.
func NewReport(dsn string) *Report {
    db, _ := sql.Open("postgres", dsn)
    return &Report{db: db}
}

func (r *Report) Generate(ctx context.Context, id int) (Summary, error) {
    row := r.db.QueryRowContext(ctx, "SELECT ... WHERE id = $1", id)
    // ...
}

// After — a one-method interface extracted at the point of use;
// the concrete *sql.DB already satisfies it implicitly, so the
// producer package needs no change at all.
type rowQuerier interface {
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func NewReport(db rowQuerier) *Report {
    return &Report{db: db}
}

func (r *Report) Generate(ctx context.Context, id int) (Summary, error) {
    row := r.db.QueryRowContext(ctx, "SELECT ... WHERE id = $1", id)
    // ... unchanged — a characterization test can now inject a fake rowQuerier
}
```

→ See `samber/cc-skills-golang@golang-design-patterns` skill for constructor and dependency-injection patterns this move builds on, and [catalog.md](catalog.md) in this skill for the Sprout/Wrap mechanics that typically pair with a freshly introduced seam.

## Verification command examples

Adapt package patterns, build tags, timeouts, and execution wrapper to the repository. Run the commands that support the task and required gates, not this entire list by default.

```bash
go build ./path/to/affected/...
go vet ./path/to/affected/...
go test -run TestBehavior ./path/to/affected
go test -race ./path/to/concurrent/...
go test -coverprofile=cover.out ./path/to/affected/...
go tool cover -func=cover.out
go tool cover -html=cover.out
go test -run '^$' -bench BenchmarkOperation -benchmem -count=10 ./path/to/affected > new.txt
benchstat old.txt new.txt
```

Benchmark comparisons require the same workload, command, toolchain, and hardware conditions. Run variants serially on shared hardware. Reuse relevant prior evidence when those conditions and the changed code still match.

For a staged migration, see [workflow](workflow.md); for seams and package moves, see [the catalog](catalog.md) and [structural changes](structural.md).
