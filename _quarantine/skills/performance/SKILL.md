---
name: performance
description: Language-neutral performance practice to measure before optimizing, avoid hot-path hazards, and compare before/after evidence.
disable-model-invocation: true
---

# Performance

Use this when a change is meant to be faster or smaller, when code sits on a hot path, or when a review sees a scaling hazard. It is language-neutral, with Go examples. For Go measurement mechanics (benchmarks, pprof, traces, benchstat), use `golang-benchmark`. For fixing a measured Go bottleneck (allocation, CPU, I/O, caching, runtime), use `golang-performance`.

Two rules pull in different directions, and both hold:
- **Don't optimize without a measurement.** A clever change with no before/after numbers is a readability cost with an unknown benefit.
- **Don't write the hazards below,** measurement or not. They are bugs that wait for the data to grow.

## Hazards to avoid by design

**Accidental O(n²).** A linear search inside a loop over a growing collection. It is fast in the test and quadratic in production:

```go
// Quadratic: slices.Contains scans seen for every item.
func dedupSlow(items []string) []string {
	var seen []string
	for _, it := range items {
		if !slices.Contains(seen, it) {
			seen = append(seen, it)
		}
	}
	return seen
}

// Linear: one map lookup per item, and the output is allocated once.
func dedup(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, it := range items {
		if _, ok := seen[it]; !ok {
			seen[it] = struct{}{}
			out = append(out, it)
		}
	}
	return out
}
```

Repeated string concatenation in a loop and re-sorting inside a loop are the same hazard. Use a builder, or sort once.

**N+1 calls.** One query, RPC or file read per item of a list. Each call is fast, but N of them dominate the request. Fetch the set in one call: `WHERE id = ANY($1)`, a batch endpoint, or a join. Watch for it hidden behind an innocent accessor called in a loop.

**Unbounded reads.** Reading all of something whose size someone else controls: a request body, a response, a file, a query without `LIMIT`, a channel or slice that grows with input. Bound it, and fail loudly at the limit instead of truncating silently:

```go
// readLimited reads at most limit bytes and reports an error for anything
// larger, instead of loading an arbitrary body into memory.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	return data, nil
}
```

For HTTP servers, wrap the body with `http.MaxBytesReader`. For large results, paginate or stream.

**Repeated setup.** Compiling a regular expression, parsing a template, or building a client per call. Do it once, at construction.

**Holding a lock across I/O.** It turns one slow call into a queue for everyone. Copy what you need under the lock, release it, then do the I/O.

**Unbuffered small writes.** Use a buffered writer around files and sockets, and flush it, checking the error.

## Measure before optimizing

1. **Name the objective and the workload.** For example: p99 latency of this endpoint at 200 requests per second with 10k rows, or peak memory for a 2 GB input. An improvement without a target has no stopping point.
2. **Find the bottleneck with a profile, not a guess.** Separate CPU, allocation and garbage collection, lock contention, I/O wait, and time spent waiting on other services. Optimizing what doesn't dominate changes nothing.
3. **Take a baseline before changing code,** with the exact command, revision, toolchain, hardware, data size and concurrency.
4. **Change one thing,** then measure again under the same conditions.

## Comparable evidence

A before/after claim holds only when the runs are comparable:
- **Same conditions.** Same machine, toolchain, flags, data, concurrency, and warm or cold state. Run the variants one after another, never in parallel on shared hardware.
- **Repeated runs, compared as distributions.** Take at least 6–10 runs each, and use a statistical comparison such as `benchstat`, not one number against another. Report the variation along with the change.
- **The right statistic.** Latency is a distribution: report percentiles, not the mean. A load generator that waits for each response hides stalls (coordinated omission). Use an open-loop or fixed-rate load.
- **The right scope.** A microbenchmark measures one function. It doesn't show end-to-end throughput, and a 30% faster function that accounts for 2% of the request saves 0.6%.

In Go, `golang-benchmark` covers writing the benchmark, running it repeatedly, profiling, and comparing runs with `benchstat`.

## Report

State the objective, the conditions and commands, the before and after numbers with their variation, and what was not measured, such as other platforms, production data or concurrency. "Should be faster" is a hypothesis, not a result. Keep the change only if it meets the objective and the code stays correct: same results, same bounds, same cancellation, and caches with a defined size and invalidation.
