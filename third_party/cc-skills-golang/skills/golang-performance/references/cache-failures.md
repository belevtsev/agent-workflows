# Cache failure modes

Caches fail in four distinct ways, and each needs a different fix. For single-flight and jitter in Go, see [caching](caching.md).

A cache must be an optimization, not a dependency. If the hit rate fell to zero right now, would the system stay up? If not, every mode below becomes an outage rather than a slowdown. Test it with a flush under production-like load.

## Stampede

A hot key expires, and every concurrent request recomputes the same value. A key served at 1,000 req/s with a 500ms recompute produces about 500 identical origin queries at each expiry, and running them all makes each one slower. Under enough load this doesn't recover.

Single-flight collapses this per instance. Probabilistic early refresh prevents it with no lock or coordination: each reader refreshes slightly early, with a chance that rises near expiry and scales with how long a recompute takes.

```go
// shouldRefresh reports whether this reader should recompute before expiry. The
// logarithm draws from an exponential distribution, so usually one reader
// refreshes while the others keep serving the cached value.
func shouldRefresh(now, expiry time.Time, recompute time.Duration) bool {
	early := time.Duration(float64(recompute) * -math.Log(1-rand.Float64()))
	return now.Add(early).After(expiry)
}
```

`1-rand.Float64()` avoids taking the logarithm of zero. Store the recompute duration with the value.

## Penetration

Requests for keys that don't exist miss every time and reach the origin, so a loop over random IDs bypasses the cache entirely. Cache the not-found result with a short TTL, and validate the key's shape before any lookup. A Bloom filter helps with a large, mostly static key space.

## Avalanche

Many keys expire together because they were filled together after a deploy, a flush, or a nightly warm. Add jitter to every TTL. After a cold start, ramp traffic rather than opening the gate, because a cold cache under full traffic can't recover by itself.

## Hot key

One key lives on one shard, so enough traffic saturates that node while the cluster idles, and sharding can't split a single key. A short-lived in-process cache of one to five seconds is the highest-leverage fix. Splitting the key into `key#0` to `key#N`, with readers picking at random, works too, at N times the writes.

## Monitoring

Watch origin request rate, not just hit ratio, because a stampede can be invisible in a hit-ratio graph. Alert on hit-ratio drops, which lead the outage.

_Adapted from `cache-stampede` in `coding-skills` by auralshin, and rewritten for Go._
