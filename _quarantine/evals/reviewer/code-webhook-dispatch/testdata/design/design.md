# Design: webhook dispatch

Change `add-webhook-dispatch`. Decision record for delivering signed webhook
events.

## Context

The delivery queue (outside this change) pulls due deliveries and calls
`Dispatcher.Deliver` for each. Endpoints are registered through the API,
which rejects an endpoint without a secret; every stored endpoint has one.

## Decision

`Dispatcher` is a thin, stateless sender: it signs the event, makes one
request and maps the response to an outcome. It keeps no state between
calls, starts no goroutines and never retries.

## Ownership and lifecycle

| Resource | Owner | Notes |
| --- | --- | --- |
| `*http.Client` (timeout, transport, connection pool, proxy and mTLS settings) | the queue worker in `cmd/worker`, which builds it once from config and passes it to `New` | `Dispatcher` sends every request through that client and never builds or uses another one, so the configured 10 s timeout and the egress proxy apply to every delivery |
| response body | `Deliver` | drained and closed before `Deliver` returns |
| endpoint secrets | the endpoint store | `Dispatcher` reads `ep.Secret` and never substitutes one |

## Failure behaviour

| Boundary | Failure | Behaviour |
| --- | --- | --- |
| HTTP request | transport error or timeout from the injected client | return it wrapped; the queue schedules the retry |
| HTTP response | non-2xx | `ErrDeliveryFailed` with the status; 410 also disables the endpoint (`ErrGone`) |
| `Disabler.Disable` | error | return it together with `ErrGone`; the queue retries the disable |
| `ep.Secret` | empty (should never happen) | treated as a data error by the store's validation; `Dispatcher` signs with whatever key it is given and never falls back |

## Options considered

1. Dispatcher builds its own `http.Client` per endpoint. Rejected: bypasses
   the egress proxy and the timeout the worker configures.
2. **Chosen:** inject one client from the worker.
