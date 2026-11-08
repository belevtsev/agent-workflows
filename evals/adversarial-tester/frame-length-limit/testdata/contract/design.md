# Design: package `frame`

## Shape

`Decode` wraps `r` in a one-byte-at-a-time `io.ByteReader` adapter, reads the
prefix with `binary.ReadUvarint`, checks the length in a helper and then reads
the payload with `io.ReadFull` into a buffer of exactly that length.

## Design rules

- DR1: The decoded length is compared to `max` as a `uint64` (`n >
  uint64(max)`), before any conversion of the length to `int`. Only a length
  that passed that check is converted to `int` and used to size the buffer.
- DR2: The prefix is read one byte at a time from `r` itself, with no
  buffering, so nothing past the frame is consumed (R1).
- DR3: Errors from `binary.ReadUvarint` that come from `r` are returned as
  they are; an error that does not come from `r` means an invalid prefix and
  maps to `ErrMalformed`. An `io.EOF` from `io.ReadFull` on the payload maps
  to `io.ErrUnexpectedEOF`.
