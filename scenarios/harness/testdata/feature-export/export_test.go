package export

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"
)

func TestTextExport(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"first", "two words"}, &out); err != nil || out.String() != "first\ntwo words\n" {
		t.Fatalf("text export = %q, %v", out.String(), err)
	}
}

func TestJSONExport(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"--format=json"}, &out); err != nil || out.String() != "[]\n" {
		t.Fatalf("empty JSON export = %q, %v", out.String(), err)
	}
	out.Reset()
	labels := []string{`The "quoted" label`, "two words"}
	if err := run(append([]string{"--format=json"}, labels...), &out); err != nil {
		t.Fatal(err)
	}
	var got []string
	if err := json.Unmarshal(out.Bytes(), &got); err != nil || !slices.Equal(got, labels) {
		t.Fatalf("JSON export = %q, %v", out.String(), err)
	}
}
