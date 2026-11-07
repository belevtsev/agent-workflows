package fixture

import (
	"errors"
	"strings"
)

var ErrMalformed = errors.New("malformed packet")

func Decode(input []byte) (string, error) {
	if len(input) == 0 || int(input[0]) != len(input)-1 {
		return "", ErrMalformed
	}
	return strings.TrimSpace(string(input[1:])), nil
}

func Encode(payload string) ([]byte, error) {
	if len(payload) > 255 {
		return nil, ErrMalformed
	}
	return append([]byte{byte(len(payload))}, []byte(payload)...), nil
}
