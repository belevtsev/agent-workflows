# Coordinated omission

Use this when writing or trusting a load test, or when production latency is far worse than a test predicted. Most load tests carry this bug, and it always flatters the result.

A generator that sends the next request only after the previous response stops sending exactly when the system is slowest. The stall is never measured, because nothing was in flight during it. The system gets graded only on the requests it served promptly.

## The arithmetic

Send one request every 10ms. The server handles 10,000 in 1ms each over 100 seconds, then freezes for 100 seconds.

A closed-loop harness records 9,999 samples of 1ms plus one of 100s, so it reports a p99 of 1ms. But 10,000 more requests were due during the freeze, and they'd have waited anywhere from 0 to 100 seconds. Across all 20,000, the true p75 is about 50 seconds and the true p99 about 98 seconds. Same run, a four-order-of-magnitude difference.

## Where it hides

- **A loop per simulated user.** This is the default shape of a hand-written test, including most Go test harnesses.
- **`wrk`, `ab`, and tools configured with think time.** A tool that asks for a rate is usually open-loop. One that asks for threads or virtual users is usually closed-loop.
- **Server timers started in the handler.** Time in the accept queue, a worker pool, a semaphore, or `database/sql` connection wait is left out, so the service reports 5ms while clients see 500ms. Start the timer before any queue, and export `sql.DBStats.WaitDuration` so pool wait is visible.
- **Bounded queues measuring only admitted requests.** Shed requests vanish from the distribution, and they were the worst ones.

## Generate load open-loop

Decide when each request should go, independently of responses, and measure from that intended time:

```go
// Run sends n requests on a fixed schedule. Latency starts at the intended send
// time, so time spent falling behind counts against the system.
func Run(ctx context.Context, interval time.Duration, n int, send func(context.Context) error, record func(time.Duration, error)) {
	var wg sync.WaitGroup
	defer wg.Wait()
	start := time.Now()
	for i := range n {
		intended := start.Add(time.Duration(i) * interval)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Until(intended)):
		}
		wg.Go(func() {
			err := send(ctx)
			record(time.Since(intended), err)
		})
	}
}
```

`time.Since(intended)` is the line that matters. Measuring from the actual send reintroduces the bug. Sending in a goroutine matters too, because a blocking send would stall the schedule. That means a freeze accumulates in-flight requests, so bound the run with a context.

Prefer an established tool when one fits: `vegeta` with a fixed `-rate`, `wrk2`, or k6's `constant-arrival-rate` executor. k6's default `constant-vus` executor is closed-loop.

## Don't average percentiles

The mean of ten servers' p99 values isn't the fleet p99, and neither is the maximum. No arithmetic recovers a combined percentile from separate percentiles. Merge histograms instead: sum the buckets, then read the percentile. Prometheus `histogram_quantile()` over `rate(..._bucket[5m])` works because it operates on buckets. Summaries can't be aggregated across instances, which is why histograms are the better default for latency.

## Reporting

- State the load level, because latency without a request rate means little.
- Report p99, p99.9, and the maximum, not just an average.
- Include failed and shed requests, or a system rejecting everything scores perfectly.
- Report variance across runs. One number from one run isn't a measurement.

Check before trusting tail numbers: load runs at a fixed rate, latency starts at the intended send, server timers start before any queue, errors stay in the distribution, and percentiles come from merged histograms.

_Adapted from `coding-skills` by auralshin, rewritten for Go._
