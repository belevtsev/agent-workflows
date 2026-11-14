# Interface contract: upload quota

Change `add-upload-quota`. Module `example.com/storage`, package `quota`.
Fixed after design review; the tests and the implementation are written
against it.

## Declarations

```go
var ErrQuotaExceeded error
var ErrInvalidSize error

type LimitSource interface {
	Limit(ctx context.Context, tenant string) (int64, error)
}

type Tracker struct{ /* unexported */ }

func NewTracker(limits LimitSource) *Tracker
func (t *Tracker) Reserve(ctx context.Context, tenant string, bytes int64) error
func (t *Tracker) Release(tenant string, bytes int64)
func (t *Tracker) Usage(tenant string) int64
func (t *Tracker) Snapshot() map[string]int64

type Store interface {
	Save(ctx context.Context, usage map[string]int64) error
}

type Flusher struct{ /* unexported */ }

func NewFlusher(tracker *Tracker, store Store, every time.Duration) *Flusher
func (f *Flusher) Run(ctx context.Context) error
```

## Requirements

- **R1.** `Reserve(ctx, tenant, n)` with `n > 0` returns nil and adds `n` to
  `Usage(tenant)` when `Usage(tenant) + n` is at most the tenant's limit from
  `LimitSource.Limit`.
- **R2.** When `Usage(tenant) + n` is more than the limit, `Reserve` returns
  `ErrQuotaExceeded` and `Usage(tenant)` is unchanged.
- **R3.** `Reserve` with `n <= 0` returns `ErrInvalidSize`, does not call
  `LimitSource.Limit`, and leaves `Usage(tenant)` unchanged.
- **R4.** `Release(tenant, n)` subtracts `n` from `Usage(tenant)`; usage never
  goes below zero (releasing more than is reserved leaves it at 0).
- **R5.** When `LimitSource.Limit` returns any error, `Reserve` returns an
  error that wraps it (`errors.Is` holds) and `Usage(tenant)` is unchanged:
  the tracker fails closed.
- **R6.** `Flusher.Run(ctx)` saves `Snapshot()` to the store once per `every`
  while `ctx` is live and once more after `ctx` is done, then returns that
  last save's error. No save happens before `Run` is called or after it
  returns.
- **R7.** Every method of `Tracker` is safe for concurrent use; concurrent
  reservations never take a tenant past its limit.
