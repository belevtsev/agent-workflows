package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// A1 (reference): R2 and DR1. Fails when a length prefix of 2^63 or more,
// far above max, is not rejected with ErrTooLarge.
func TestRefHugeLengthIsTooLarge(t *testing.T) {
	for _, n := range []uint64{1 << 63, 1<<64 - 1} {
		in := binary.AppendUvarint(nil, n)
		got, err := Decode(bytes.NewReader(in), 16)
		if !errors.Is(err, ErrTooLarge) || got != nil {
			t.Fatalf("n=%d: Decode = %v, %v; want nil, ErrTooLarge", n, got, err)
		}
	}
}
