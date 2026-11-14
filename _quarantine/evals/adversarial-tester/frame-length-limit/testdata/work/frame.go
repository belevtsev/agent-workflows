// Package frame decodes length-prefixed frames from an untrusted stream.
package frame

import (
	"encoding/binary"
	"errors"
	"io"
)

var (
	// ErrTooLarge reports a length prefix above the caller's limit.
	ErrTooLarge = errors.New("frame: length exceeds limit")
	// ErrMalformed reports a length prefix that is not a valid uvarint.
	ErrMalformed = errors.New("frame: malformed length prefix")
)

// Decode reads one frame from r: a uvarint length followed by that many
// payload bytes. A length above max returns ErrTooLarge.
func Decode(r io.Reader, max int) ([]byte, error) {
	br := &byteReader{r: r}
	n, err := binary.ReadUvarint(br)
	if err != nil {
		if br.err != nil {
			return nil, err
		}
		return nil, ErrMalformed
	}

	size, err := checkLength(n, max)
	if err != nil {
		return nil, err
	}

	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return buf, nil
}

// checkLength turns the decoded prefix into a buffer size, rejecting a
// length above max.
func checkLength(n uint64, max int) (int, error) {
	size := int(n)
	if size > max {
		return 0, ErrTooLarge
	}
	return size, nil
}

// byteReader reads single bytes from r without buffering and remembers the
// last error r returned.
type byteReader struct {
	r   io.Reader
	err error
	b   [1]byte
}

func (br *byteReader) ReadByte() (byte, error) {
	if _, err := io.ReadFull(br.r, br.b[:]); err != nil {
		br.err = err
		return 0, err
	}
	return br.b[0], nil
}
