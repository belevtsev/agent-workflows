package decl

import (
	"errors"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

// A name a repository holds is printed as it is when it holds no control
// character, and quoted otherwise, so that it adds neither a line nor a
// terminal command to what gatecub prints.
func TestPlain(t *testing.T) {
	t.Parallel()
	unchanged := []string{
		"",
		"src/app.txt",
		"docs/a file with spaces.md",
		"naïve/Ünïcödé/файл.txt",
		"tab-free~!@#$%^&*()[]{}",
	}
	for _, text := range unchanged {
		if got := Plain(text); got != text {
			t.Errorf("Plain(%q) = %q, want it unchanged", text, got)
		}
	}
	quoted := map[string]string{
		"a line break":      "src/a\nb.txt",
		"a carriage return": "src/a\rb.txt",
		"a tab":             "src/a\tb.txt",
		"ESC":               "src/\x1b[31mred.txt",
		"NUL":               "a\x00b",
		"DEL":               "a\x7fb",
		"a C1 control":      "a\u0085b",
		"CSI as C1":         "a\u009b2Jb",
		"invalid UTF-8":     "a\xffb",
	}
	for name, text := range quoted {
		got := Plain(text)
		if got != strconv.Quote(text) {
			t.Errorf("%s: Plain(%q) = %q, want %q", name, text, got, strconv.Quote(text))
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: Plain(%q) = %q is not valid UTF-8", name, text, got)
		}
		for _, r := range got {
			if unicode.IsControl(r) {
				t.Errorf("%s: Plain(%q) = %q still holds the control character %U", name, text, got, r)
			}
		}
	}
}

// The ids that Windows reads as a device are refused on every system, so
// that one declaration is valid everywhere (second challenge, finding P6).
func TestDeviceNamesAreRefusedAsIDs(t *testing.T) {
	t.Parallel()
	for _, id := range []string{"con", "prn", "aux", "nul", "com0", "com1", "com9", "lpt1", "lpt9"} {
		_, err := Parse([]byte(withCheck("id = \"" + id + "\"\nrun = [\"sh\"]\n")))
		var declErr *Error
		if !errors.As(err, &declErr) {
			t.Errorf("id %q: error %v, want a *decl.Error", id, err)
			continue
		}
		text := err.Error()
		for _, want := range []string{FileName + ":4:", "`" + id + "`", "Windows", "device"} {
			if !strings.Contains(text, want) {
				t.Errorf("id %q: error %q does not hold %q", id, text, want)
			}
		}
	}
	for _, id := range []string{"console", "auxiliary", "com", "com10", "lpt", "nul-check", "prn2x", "con_"} {
		if _, err := Parse([]byte(withCheck("id = \"" + id + "\"\nrun = [\"sh\"]\n"))); err != nil {
			t.Errorf("id %q is no device name, yet Parse refused it: %v", id, err)
		}
	}
	if _, err := Append([]byte(withCheck("id = \"unit\"\nrun = [\"sh\"]\n")), "aux", []string{"sh"}, nil, ""); err == nil || !strings.Contains(err.Error(), "Windows") {
		t.Errorf("Append of the id aux: error %v, want a refusal that names Windows", err)
	}
}
