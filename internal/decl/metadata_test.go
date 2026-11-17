package decl

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func TestVerificationMetadata(t *testing.T) {
	t.Parallel()
	old := []byte("# preserved spacing\r\nversion = 1\r\n")
	out, err := Append(old, "behavior", []string{"go", "test"}, []string{"evidence/**"}, "", Metadata{When: []string{"src/**"}, Expect: "reject invalid input", Verify: true})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(out, old) {
		t.Fatal("append changed existing bytes")
	}
	file, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	c := file.Checks[0]
	if !c.Verify || c.Expect != "reject invalid input" || !slices.Equal(c.When, []string{"src/**"}) || !slices.Equal(c.Paths, []string{"evidence/**"}) {
		t.Fatalf("metadata or evidence paths changed: %+v", c)
	}
}

func TestVerificationMetadataRefusals(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, field, key string }{
		{"empty when", "when = []", "when"},
		{"string when", `when = "src/**"`, "when"},
		{"number when", "when = [1]", "when"},
		{"absolute when", `when = ["/src/**"]`, "when"},
		{"empty pattern", `when = [""]`, "when"},
		{"empty segment", `when = ["src//file"]`, "when"},
		{"empty expect", `expect = " "`, "expect"},
		{"number expect", "expect = 3", "expect"},
		{"string verify", `verify = "true"`, "verify"},
		{"missing expect", "verify = true", "expect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(withCheck("id = \"test\"\nrun = [\"true\"]\n" + tc.field + "\n")))
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("accepted invalid metadata or missing diagnostic: %v", err)
			}
		})
	}
}
