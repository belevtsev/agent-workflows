# Interface contract: package `frame`

A decoder for length-prefixed frames read from an untrusted stream. Language:
Go. Module `example.com/frame`, package `frame` at the module root.

## Public interface

```go
var ErrTooLarge  error // the length prefix is above the caller's limit
var ErrMalformed error // the length prefix is not a valid uvarint

func Decode(r io.Reader, max int) ([]byte, error)
```

A frame is a length `n` encoded as an unsigned varint (`encoding/binary`
uvarint, at most 10 bytes) followed by exactly `n` payload bytes.

## Requirements

- R1: When `n <= max`, `Decode` returns the `n` payload bytes and a nil error.
  `n == 0` returns an empty, non-nil slice. `Decode` reads no byte of `r` past
  the end of the frame.
- R2: For every `n > max`, up to and including `2^64-1`, `Decode` returns a
  nil slice and `ErrTooLarge` (`errors.Is` matches it), before reading any
  payload byte and without allocating a buffer for the payload. It never
  panics on any prefix value.
- R3: When `r` ends before all `n` payload bytes are read, `Decode` returns
  `io.ErrUnexpectedEOF`.
- R4: A prefix that is not a valid uvarint (more than 10 bytes, or a value
  that overflows 64 bits) returns `ErrMalformed`.
- R5: When `r` is empty, `Decode` returns `io.EOF`; when `r` ends inside the
  prefix, `io.ErrUnexpectedEOF`. Any other error from `r` is returned so that
  `errors.Is` matches it.

## Scope

- `max` is non-negative; a negative `max` is out of scope.
- No encoder, no streaming of large payloads, no checksum.
