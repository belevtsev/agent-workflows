# Go Performance Optimization

Read the section needed for the current task. Commands are examples to adapt to the repository's permitted environment and selected toolchain; setup, broad audits, and external actions require their own task scope.


## Core Philosophy

1. **Profile before optimizing** — profile evidence helps distinguish bottlenecks from guesses. Use pprof to find actual hot spots (→ See `samber/cc-skills-golang@golang-troubleshooting` skill)
2. **Allocation reduction yields the biggest ROI** — Go's GC is fast but not free. Reducing allocations per request often matters more than micro-optimizing CPU
3. **Document optimizations** — add code comments explaining why a pattern is faster, with benchmark numbers when available. Future readers need context to avoid reverting an "unnecessary" optimization

## Rule Out External Bottlenecks First

Before optimizing Go code, verify the bottleneck is in your process — if 90% of latency is a slow DB query or API call, reducing allocations won't help.

**Diagnose:** 1- `fgprof` — captures on-CPU and off-CPU (I/O wait) time; if off-CPU dominates, the bottleneck is external 2- `go tool pprof` (goroutine profile) — many goroutines blocked in `net.(*conn).Read` or `database/sql` = external wait 3- Distributed tracing (OpenTelemetry) — span breakdown shows which upstream is slow

**When external:** optimize that component instead — query tuning, caching, connection pools, circuit breakers (→ See `samber/cc-skills-golang@golang-database` skill, [Caching Patterns](caching.md)).

## Iterative Optimization Methodology

### The cycle: Define Goals → Benchmark → Diagnose → Improve → Benchmark

1. **Define your metric** — latency, throughput, memory, or CPU? Without a target, optimizations are random
2. **Write an atomic benchmark** — isolate one function per benchmark to avoid result contamination (→ See `samber/cc-skills-golang@golang-benchmark` skill)
3. **Measure baseline** — `go test -bench=BenchmarkMyFunc -benchmem -count=6 ./pkg/... | tee /tmp/report-1.txt`
4. **Diagnose** — use the **Diagnose** lines in each deep-dive section to pick the right tool
5. **Improve** — apply ONE optimization at a time with an explanatory comment
6. **Compare** — `benchstat /tmp/report-1.txt /tmp/report-2.txt` to confirm statistical significance
7. **Commit** — paste the benchstat output in the commit body so reviewers and future readers see the exact improvement; follow the `perf(scope): summary` commit type
8. **Repeat** — increment report number, tackle next bottleneck

Refer to library documentation for known patterns before inventing custom solutions. Keep all `/tmp/report-*.txt` files as an audit trail.

Keep competing variants and their results distinguishable. Optional delegated implementation needs independent ownership and task authorization. Run comparisons serially on shared hardware; consult the benchmark specialist if the comparison mechanics need detail.

## Decision Tree: Where Is Time Spent?

| Bottleneck | Signal (from pprof) | Action |
| --- | --- | --- |
| Too many allocations | `alloc_objects` high in heap profile | [Memory optimization](memory.md) |
| CPU-bound hot loop | function dominates CPU profile | [CPU optimization](cpu.md) |
| GC pauses / OOM | high GC%, container limits | [Runtime tuning](runtime.md) |
| Network / I/O latency | goroutines blocked on I/O | [I/O & networking](io-networking.md) |
| Repeated expensive work | same computation/fetch multiple times | [Caching patterns](caching.md) |
| Wrong algorithm | O(n²) where O(n) exists | [Algorithmic complexity](caching.md#algorithmic-complexity) |
| Lock contention | mutex/block profile hot | → See `samber/cc-skills-golang@golang-concurrency` skill |
| Slow queries | DB time dominates traces | → See `samber/cc-skills-golang@golang-database` skill |

## Common Mistakes

| Mistake | Fix |
| --- | --- |
| Optimizing without profiling | Use profiling or other workload evidence to identify the bottleneck |
| Default `http.Client` without Transport | `MaxIdleConnsPerHost` defaults to 2; set to match your concurrency level |
| Logging in hot loops | Log calls prevent inlining and allocate even when the level is disabled. Use `slog.LogAttrs` |
| `panic`/`recover` as control flow | panic allocates a stack trace and unwinds the stack; use error returns |
| `unsafe` without benchmark proof | Only justified when profiling shows >10% improvement in a verified hot path |
| No GC tuning in containers | Set `GOMEMLIMIT` to 80-90% of container memory to prevent OOM kills |
| `reflect.DeepEqual` in production | 50-200x slower than typed comparison; use `slices.Equal`, `maps.Equal`, `bytes.Equal` |

## Deep Dives

- [Memory Optimization](memory.md) — allocation patterns, backing array leaks, sync.Pool, struct alignment
- [CPU Optimization](cpu.md) — inlining, cache locality, false sharing, ILP, reflection avoidance
- [I/O & Networking](io-networking.md) — HTTP transport config, streaming, JSON performance, cgo, batch operations
- [Runtime Tuning](runtime.md) — GOGC, GOMEMLIMIT, GC diagnostics, GOMAXPROCS, PGO
- [Caching Patterns](caching.md) — algorithmic complexity, compiled patterns, singleflight, work avoidance
- [Production Observability](observability.md) — Prometheus metrics, PromQL queries, continuous profiling, alerting rules

## CI Regression Detection

Automate benchmark comparison in CI to catch regressions before they reach production. → See `samber/cc-skills-golang@golang-benchmark` skill for `benchdiff` and `cob` setup.
