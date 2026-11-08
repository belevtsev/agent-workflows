# Design: price cache

Change `add-price-cache`. Decision record for caching prices in the
checkout service.

## Context

Checkout asks for a few hundred distinct SKUs per second, mostly hot ones.
The pricing service answers in 20 ms at the median, but its p99 is 2 s and
it has stalled for minutes during deploys.

## Decision

One `Cache` per process, holding a map from SKU to entry guarded by one
mutex. Fetches happen on demand inside `Get`; there is no background
refresh.

## Ownership, locking and lifecycle

| Resource | Owner | Rule |
| --- | --- | --- |
| entry map | `Cache` | guarded by `mu` |
| `mu` | `Cache` | held only to read or write the map, never across `Upstream.Fetch`: release it before the call and take it again to store the result |
| the upstream call's deadline | the caller, through `ctx` | `Get` passes `ctx` through unchanged |

Holding `mu` across `Fetch` would make one slow or stalled SKU block every
`Get` in the process, including hits on fresh entries; that is the outage
this cache exists to prevent. Two concurrent misses for the same SKU may
both fetch; that duplicate is accepted.

## Failure behaviour

| Boundary | Failure | Behaviour |
| --- | --- | --- |
| `Upstream.Fetch` | not found | `ErrUnknownSKU`, remembered for `negativeTTL` |
| `Upstream.Fetch` | any other error, timeout included | returned wrapped; never serve a stale price (checkout must not charge an outdated price) |

## Options considered

1. Serve stale prices while the upstream is down. Rejected by pricing: an
   outdated price can undercharge after a price change.
2. Background refresh goroutine. Rejected: more moving parts than demand
   fetching needs.
3. **Chosen:** on-demand fetch, lock never held across the call.
