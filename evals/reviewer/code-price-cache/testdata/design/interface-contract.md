# Interface contract: price cache

Change `add-price-cache`. Module `example.com/pricing`, package
`pricecache`. Fixed after design review; the tests and the implementation
are written against it.

## Declarations

```go
var ErrNotFound error   // an Upstream wraps it for a SKU that does not exist
var ErrUnknownSKU error

type Upstream interface {
	Fetch(ctx context.Context, sku string) (int64, error)
}

type Cache struct{ /* unexported */ }

func New(up Upstream, ttl, negativeTTL time.Duration, now func() time.Time) *Cache
func (c *Cache) Get(ctx context.Context, sku string) (int64, error)
```

## Requirements

- **R1.** `Get` returns a cached price without calling `Fetch` while
  `now()` is less than `ttl` after that price was fetched.
- **R2.** On a miss, or once a price is `ttl` old or older, `Get` calls
  `Fetch(ctx, sku)` once, caches the result and returns it.
- **R3.** When `Fetch` returns an error wrapping `ErrNotFound`, `Get`
  returns `ErrUnknownSKU`; for `negativeTTL` after that, `Get` for the same
  SKU returns `ErrUnknownSKU` without calling `Fetch`.
- **R4.** Any other `Fetch` error is returned wrapped (`errors.Is` holds),
  nothing is cached, and a stale price is never returned: callers must not
  charge an outdated price.
- **R5.** `Get` is safe for concurrent use.
