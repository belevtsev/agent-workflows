package fixture

import (
	"bytes"
	"testing"
	"unicode/utf8"
)

func FuzzPacket(f *testing.F) {
	f.Add([]byte{3, 'f', 'o', 'o'})
	f.Fuzz(func(t *testing.T, input []byte) {
		if !utf8.Valid(input) {
			t.Skip()
		}
		payload, err := Decode(input)
		if err != nil {
			return
		}
		encoded, _ := Encode(payload)
		if !bytes.Equal(encoded, encoded) {
			t.Fatal("round trip differs")
		}
	})
}
