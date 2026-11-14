# Interface contract: package `limits`

Parses a flat map of configuration strings into a value whose fields are
already narrowed, so callers never re-check them. Language: Go. Module
`example.com/limits`, package `limits`.

## Public interface

```go
package limits

var ErrMissing = errors.New("limits: missing key")
var ErrInvalid = errors.New("limits: invalid value")
var ErrUnknownKey = errors.New("limits: unknown key")

// Count is an integer in 1..65535. Parse is the only constructor this
// package offers; a hand-written Count is a caller bug outside this contract.
type Count int

type Limits struct {
	Connections Count
	Timeout     time.Duration // 1ms..1h, narrowed by Parse
}

func Parse(raw map[string]string) (Limits, error)
func (l Limits) String() string
```

## Requirements

- R1: `Parse` accepts exactly the keys `connections` (decimal integer 1 to
  65535) and `timeout` (a Go duration string from 1ms to 1h inclusive) and
  returns the parsed `Limits` with a nil error.
- R2: A missing key produces an error `e` with `errors.Is(e, ErrMissing)` true
  and `e.Error()` containing the key name.
- R3: A present key whose value is malformed or out of range produces an
  error with `errors.Is(e, ErrInvalid)` true and the key name in its text.
- R4: Any other key produces an error with `errors.Is(e, ErrUnknownKey)` true
  and the key name in its text.
- R5: Whenever any key fails, `Parse` returns the zero `Limits` and every
  failing key's error joined with `errors.Join`, in the order `connections`,
  `timeout`, then unknown keys in sorted order.
- R6: `Parse` never mutates `raw`. `String` returns
  `connections=<N> timeout=<D>` with `D` in `time.Duration` text form.

## Scope

- No defaults: a missing key is an error, not a value.
- No file or environment reading; the caller supplies the map.
