package manageruntime

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Locators use the historical Python-compatible sorted, indented ASCII JSON
// seal. Keep this byte representation independent of workflow to avoid making
// low-level acquisition depend on the installer that consumes it.
func verifyLocatorSeal(data []byte) error {
	var value map[string]any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	expected, ok := value["integrity_sha256"].(string)
	if !ok {
		return errors.New("corrupt local state integrity checksum")
	}
	delete(value, "integrity_sha256")
	encoded, err := json.Marshal(value, json.Deterministic(true), jsontext.WithIndent("  "), jsontext.EscapeForHTML(false), jsontext.EscapeForJS(false))
	if err != nil {
		return err
	}
	var ascii strings.Builder
	for _, character := range string(encoded) {
		if character < 128 {
			ascii.WriteRune(character)
		} else if character <= 0xffff {
			fmt.Fprintf(&ascii, `\u%04x`, character)
		} else {
			first, second := utf16.EncodeRune(character)
			fmt.Fprintf(&ascii, `\u%04x\u%04x`, first, second)
		}
	}
	sum := sha256.Sum256(append([]byte(ascii.String()), '\n'))
	if expected != hex.EncodeToString(sum[:]) {
		return errors.New("corrupt local state integrity checksum")
	}
	return nil
}
