---
name: retry-amplification
description: Prevent retry storms and metastable failure with retry budgets, single-layer retries, jitter, and load shedding.
license: MIT
---

# Retry amplification

Use this when configuring retries, when a service stays down after its cause is fixed, or when reviewing a call chain, a timeout, or load shedding. For herds caused by cache expiry, use `golang-performance`.

Retries multiply down a call chain. Three attempts at each of three layers means 27 times the load at the database, and it arrives exactly when the database is least able to serve it.

## Metastable failure

A system is in a metastable failure when it stays down after the trigger is gone, because the failure now sustains itself. A brief latency spike causes timeouts, clients retry, load rises, and more requests time out. The spike ends, but the load doesn't.

Removing the cause doesn't fix it, and neither does restarting, because the retry backlog hits the fresh instance immediately. **The only way out is reducing load:** block traffic at the edge, let the system drain, then ramp back slowly. Put that in the runbook, so nobody loses hours restarting things.

A cold cache that the origin can't repopulate, an exhausted connection pool, and a queue backlog older than every consumer's timeout can all sustain themselves the same way.

## Rules

**Retry at one layer.** Choose the layer closest to the user, which knows whether a retry is still useful and holds the deadline. Lower layers fail fast and report upward. If several layers must retry, reason about the total explicitly instead of letting it emerge from three teams' defaults.

**Use a budget, not just a count.** A per-request count bounds nothing in aggregate. A budget ties retries to successes, so they stop on their own when a dependency is unhealthy:

```go
// RetryBudget allows retries while enough recent calls succeed. Each retry
// spends a token, and each success refills a fraction of one.
type RetryBudget struct {
	mu         sync.Mutex
	tokens     float64
	limit      float64
	perSuccess float64
}

func NewRetryBudget(limit, perSuccess float64) *RetryBudget {
	return &RetryBudget{tokens: limit, limit: limit, perSuccess: perSuccess}
}

func (b *RetryBudget) OnSuccess() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tokens = min(b.limit, b.tokens+b.perSuccess)
}

func (b *RetryBudget) AllowRetry() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
```

With `perSuccess` at 0.1, retries settle at about 10% of successful requests. gRPC has this built in as `retryThrottling` in the service config, so a `retryPolicy` should normally come with it.

**Only retry what can succeed.** Retry connection failures, and 429, 502, or 503 responses, honoring `Retry-After`. In gRPC, `UNAVAILABLE` is the usual retryable code. Don't retry validation or authorization failures, or anything past its deadline.

**A timeout has an unknown outcome.** Retrying a non-idempotent call after a timeout is how customers get charged twice. Make it safe first with an idempotency key: the client generates one per logical operation, and the server stores it with the result, so a repeat returns the stored result.

**Always add jitter.** Plain exponential backoff synchronizes clients, so everyone who failed together retries together.

```go
// fullJitter returns a random delay below the capped exponential ceiling.
// attempt must be nonnegative; base and ceiling must be positive.
func fullJitter(attempt int, base, ceiling time.Duration) time.Duration {
	if attempt < 0 || base <= 0 || ceiling <= 0 {
		panic("invalid backoff parameters")
	}
	limit := min(base, ceiling)
	for ; attempt > 0 && limit < ceiling; attempt-- {
		if limit > ceiling/2 {
			limit = ceiling
		} else {
			limit *= 2
		}
	}
	return rand.N(limit)
}
```

Jitter belongs on anything periodic across hosts: cron schedules, cache TTLs, health checks, reconnects, and token refreshes.

**Add circuit breakers and bulkheads.** A breaker stops calling a failing dependency for a cooldown, which gives it room to recover. A separate connection pool per dependency stops one slow dependency from taking every goroutine and connection in the process.

**Cancel abandoned work.** Pass `context.Context` with a deadline through every call, including database queries. A service still processing a request its client gave up on is burning the capacity it needs to recover.

## Load shedding

Retries are the client's half. Shedding is the server's half, and it's what actually prevents collapse.

- **Shed at the front door,** before taking a goroutine slot, a connection, or a database handle. A rejection that costs a database round trip isn't shedding.
- **Shed on queue depth or latency,** not CPU, which lags. If a request already waited past its deadline, drop it unprocessed.
- **Shed retries first.** Mark retries with a header and drop them preferentially. A retry is load that's already been partly served, so this cuts amplification directly. It's a small change with an outsized effect.
- **Then shed background work** before interactive traffic.
- **Return 429 with `Retry-After`,** which tells a well-behaved client both that it may retry and when.

## Review

1. How many layers on this path retry? It should be one.
2. Multiply retry counts along the longest chain. Above 10 means a latent outage is configured and waiting.
3. Is there a budget, or only a count?
4. Is jitter applied everywhere periodic?
5. Are non-idempotent calls protected before being retried?
6. Do deadlines propagate, and does expiry cancel the work?
7. Does the shed path cost less than the work path?
8. Does the runbook say to drop load and then ramp?

_Adapted from `coding-skills` by auralshin, rewritten for Go._
