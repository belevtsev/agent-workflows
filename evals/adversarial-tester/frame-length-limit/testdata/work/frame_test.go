package frame_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"example.com/frame"
)

const limit = 16

// encode builds a frame header for length n followed by payload.
func encode(n uint64, payload []byte) []byte {
	return append(binary.AppendUvarint(nil, n), payload...)
}

// R1: fails if a frame within the limit is not returned exactly.
func TestDecodeWithinLimit(t *testing.T) {
	for _, n := range []int{1, 7, limit - 1, limit} {
		payload := bytes.Repeat([]byte{'x'}, n)
		got, err := frame.Decode(bytes.NewReader(encode(uint64(n), payload)), limit)
		if err != nil {
			t.Fatalf("n=%d: err = %v, want nil", n, err)
		}
		if !bytes.Equal(got, payload) {
			t.Fatalf("n=%d: payload = %q, want %q", n, got, payload)
		}
	}
}

// R1: fails if a zero-length frame is not an empty, non-nil slice.
func TestDecodeEmptyPayload(t *testing.T) {
	got, err := frame.Decode(bytes.NewReader(encode(0, nil)), limit)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("payload = %#v, want empty non-nil slice", got)
	}
}

// R1, DR2: fails if Decode consumes bytes past the end of the frame.
func TestDecodeLeavesTrailingBytes(t *testing.T) {
	r := bytes.NewReader(append(encode(3, []byte("abc")), "next"...))
	got, err := frame.Decode(r, limit)
	if err != nil || string(got) != "abc" {
		t.Fatalf("Decode = %q, %v; want %q, nil", got, err, "abc")
	}
	if r.Len() != len("next") {
		t.Fatalf("%d bytes left after the frame, want %d", r.Len(), len("next"))
	}
}

// R2: fails if a length just above the limit is not rejected before the
// payload is read.
func TestDecodeAboveLimit(t *testing.T) {
	for _, n := range []uint64{limit + 1, limit + 2, 300, 1 << 20} {
		payload := bytes.Repeat([]byte{'y'}, limit+2)
		header := binary.AppendUvarint(nil, n)
		r := bytes.NewReader(append(header, payload...))
		got, err := frame.Decode(r, limit)
		if !errors.Is(err, frame.ErrTooLarge) || got != nil {
			t.Fatalf("n=%d: Decode = %q, %v; want nil, ErrTooLarge", n, got, err)
		}
		if r.Len() != len(payload) {
			t.Fatalf("n=%d: %d payload bytes left, want all %d unread", n, r.Len(), len(payload))
		}
	}
}

// R3: fails if a short payload does not return io.ErrUnexpectedEOF.
func TestDecodeShortPayload(t *testing.T) {
	for _, in := range [][]byte{encode(5, []byte("ab")), encode(5, nil)} {
		_, err := frame.Decode(bytes.NewReader(in), limit)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("input %x: err = %v, want io.ErrUnexpectedEOF", in, err)
		}
	}
}

// R4: fails if an over-long or overflowing prefix is not ErrMalformed.
func TestDecodeMalformedPrefix(t *testing.T) {
	inputs := [][]byte{
		bytes.Repeat([]byte{0xff}, 11),
		append(bytes.Repeat([]byte{0xff}, 9), 0x02),
	}
	for _, in := range inputs {
		_, err := frame.Decode(bytes.NewReader(in), limit)
		if !errors.Is(err, frame.ErrMalformed) {
			t.Fatalf("input %x: err = %v, want ErrMalformed", in, err)
		}
	}
}

// R5: fails if empty input or a truncated prefix returns the wrong error.
func TestDecodeTruncatedPrefix(t *testing.T) {
	if _, err := frame.Decode(bytes.NewReader(nil), limit); err != io.EOF {
		t.Fatalf("empty input: err = %v, want io.EOF", err)
	}
	if _, err := frame.Decode(bytes.NewReader([]byte{0x80}), limit); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated prefix: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

type failingReader struct{ err error }

func (f failingReader) Read([]byte) (int, error) { return 0, f.err }

// R5: fails if a reader error is lost or replaced.
func TestDecodeReaderError(t *testing.T) {
	errRead := errors.New("read failed")
	if _, err := frame.Decode(failingReader{errRead}, limit); !errors.Is(err, errRead) {
		t.Fatalf("err = %v, want %v", err, errRead)
	}
}
