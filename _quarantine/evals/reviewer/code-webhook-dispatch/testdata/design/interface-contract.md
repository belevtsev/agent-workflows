# Interface contract: webhook dispatch

Change `add-webhook-dispatch`. Module `example.com/hooks`, package
`dispatch`. Fixed after design review; the tests and the implementation are
written against it.

## Declarations

```go
var ErrDeliveryFailed error
var ErrGone error

type Endpoint struct {
	ID     string
	URL    string
	Secret []byte
}

type Disabler interface {
	Disable(ctx context.Context, id string) error
}

type Dispatcher struct{ /* unexported */ }

func New(client *http.Client, disabler Disabler) *Dispatcher
func (d *Dispatcher) Deliver(ctx context.Context, ep Endpoint, event []byte) error
```

## Requirements

- **R1.** `Deliver` sends one `POST` to `ep.URL` with `event` as the body,
  `Content-Type: application/json`, and `X-Signature` set to the lowercase
  hex HMAC-SHA256 of `event` keyed with `ep.Secret`. A 2xx response returns
  nil.
- **R2.** A response status outside 2xx, other than 410, returns an error
  that wraps `ErrDeliveryFailed` and contains the status code.
- **R3.** A 410 Gone response calls `Disabler.Disable(ctx, ep.ID)` once and
  returns an error that wraps `ErrGone`. When `Disable` fails, the returned
  error wraps both `ErrGone` and the `Disable` error.
- **R4.** A transport failure (connection refused or reset, deadline) returns
  an error that wraps it, after exactly one attempt; retries belong to the
  caller's queue.
- **R5.** The response body is read to the end and closed on every path.
- **R6.** An `ep.URL` that cannot form a request returns an error and sends
  nothing.
