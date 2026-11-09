package frame

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// Self-test fixture (AG4): one probe that fails on the seeded source and one
// test that passes on it. TestDecode is a prefix of the probe's name, so the
// gate only reports it as passing when it runs each kept test alone
// (-run '^Name$').

// R2 and DR1: the reference probe, renamed. Fails when a length prefix of
// 2^63 or more, far above max, is not rejected with ErrTooLarge.
func TestDecodeHugeLengthIsTooLarge(t *testing.T) {
	for _, n := range []uint64{1 << 63, 1<<64 - 1} {
		in := binary.AppendUvarint(nil, n)
		got, err := Decode(bytes.NewReader(in), 16)
		if !errors.Is(err, ErrTooLarge) || got != nil {
			t.Fatalf("n=%d: Decode = %v, %v; want nil, ErrTooLarge", n, got, err)
		}
	}
}

// R1 with a small length: behaviour the seeded source already has, so the
// gate must name this test. Fails when a 3-byte frame under max is not
// returned as its payload.
func TestDecode(t *testing.T) {
	in := append(binary.AppendUvarint(nil, 3), "abc"...)
	got, err := Decode(bytes.NewReader(in), 16)
	if err != nil || string(got) != "abc" {
		t.Fatalf("Decode = %q, %v; want %q, nil", got, err, "abc")
	}
}
