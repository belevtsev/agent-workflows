# Capacity and tail latency

Use this when a bound, pool size, queue depth, or timeout needs a number. Derive it from the workload rather than choosing a round value.

## Little's Law

> Concurrency = arrival rate × time in system

This holds for any stable system, whatever the arrival pattern or scheduling.

| Question | Calculation | Example |
| --- | --- | --- |
| Pool size | Rate × latency | 500 req/s × 40ms = 20 concurrent, not 200 |
| Maximum throughput | Concurrency ÷ latency | 20 connections at 40ms = 500 q/s ceiling |
| Lag from queue depth | Depth ÷ drain rate | 3,000 messages at 100/s = 30s behind |
| Time to drain | Backlog ÷ (capacity − arrivals) | 3,000 at 100/s with 60/s arriving = 75s |

Use it to sanity-check load tests. A claim of 10,000 req/s at 100ms with 50 workers implies 1,000 concurrent requests, which 50 workers can't produce, so one of the three numbers is wrong.

## Utilization

For a single queue, wait time grows as service time ÷ (1 − utilization):

| Utilization | Latency multiple |
| --- | --- |
| 50% | 2× |
| 70% | 3.3× |
| 80% | 5× |
| 90% | 10× |
| 95% | 20× |

The curve is a hyperbola, so the damage concentrates at the end. "CPU is at 85%, so we have headroom" describes throughput headroom with no latency headroom. Aim for 50 to 70% on latency-sensitive services, and accept 80 to 90% for batch work. Check per-shard utilization, because a 40% average with one node at 95% is a broken system on a healthy dashboard.

Queueing delay also scales with variability, so reducing variance reduces delay with no new hardware. Separating slow requests from fast ones, capping request size, and rate-limiting bursty producers all work this way. One shared queue with N workers also tolerates more load than N separate queues, because separate queues let one worker idle while another has a backlog.

## Queues don't add capacity

A queue absorbs a burst, and does nothing for sustained overload. When arrivals exceed service, it grows until everything in it is late.

1. Bound every queue. An unbounded channel or slice fails by becoming infinitely slow instead of rejecting work.
2. Set the bound so depth ÷ drain rate stays under the client timeout, because work finishing after that is wasted.
3. Shed past that point, as `retry-amplification` describes.
4. Alert on queue depth, which rises before latency does.

## Fan-out

When a request needs all of n dependencies, the slowest one sets its latency. The chance of hitting at least one p99 event is 1 − 0.99ⁿ:

| Dependencies | Chance of a p99 event | Chance of a p99.9 event |
| --- | --- | --- |
| 10 | 9.6% | 1.0% |
| 50 | 39% | 4.9% |
| 100 | 63% | 9.5% |

So at a fan-out of 100, one server's p99 becomes the system's p63, and service latency is set by dependencies' p99.9. Improving a dependency's median barely matters there. Sequential calls compose the same way.

## Tolerating slow components

- **Propagate deadlines.** Pass `context.Context` with a deadline through the whole chain, including queries. Without it, a client that gave up leaves several services working for nobody. This is the most valuable item here and the one most often missing.
- **Hedge idempotent reads.** If there's no response by about the p95, send a second copy to another replica, take the first answer, and cancel the other. Only about 5% of requests hedge. Cap the hedge rate, so a system-wide slowdown doesn't double load.
- **Avoid unbounded fan-out and network N+1 calls.** A hundred sequential 5ms calls is 500ms of pure tail exposure.
- **Set timeouts from the tail.** A timeout near the median fires on healthy traffic and turns slow responses into retries.
- **Consider partial results** at the deadline. That's a product decision, so make it explicitly and record which responses were partial.

Measure p99.9 for every dependency, and merge histograms rather than averaging percentiles. For load test accuracy, see coordinated omission in `golang-benchmark`.

_Adapted from `queueing-and-utilization` and `tail-latency` in `coding-skills` by auralshin._
