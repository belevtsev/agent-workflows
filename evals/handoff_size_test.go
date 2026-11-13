package evals

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// The handoff-size grader of the reviewer suite (D15): the handoff a reviewer
// writes to artefacts/handoff.json holds at most taskflow.MaxResultBytes
// (6,144) bytes. A regex grader counts UTF-16 units, not bytes, so the
// pattern has two branches: an ASCII-only file of at most 6,144 units, or a
// file with at most handoffSizeNonASCII units outside ASCII and at most
// 6,144 - 2*handoffSizeNonASCII units in all. A unit outside ASCII adds at
// most two bytes to its own (three bytes for one unit, four for a surrogate
// pair), so neither branch passes a file over the cap. The second branch
// fails some files under it: more than 64 units outside ASCII, or over 6,016
// units with any.
const handoffSizeNonASCII = 64

// handoffSizeGrader is the grader file every reviewer case carries.
func handoffSizeGrader() string {
	limit := taskflow.MaxResultBytes
	return fmt.Sprintf("---\ntype: regex\npattern: '^(?:[\\x00-\\x7F]{0,%d}|(?=[\\s\\S]{0,%d}$)(?:[\\x00-\\x7F]*[^\\x00-\\x7F]){1,%d}[\\x00-\\x7F]*)$'\n"+
		"target: { source: file, path: artefacts/handoff.json }\n---\n", limit, limit-2*handoffSizeNonASCII, handoffSizeNonASCII)
}

// oracle: every case under evals/reviewer has graders/handoff-size.md, and
// its bytes are handoffSizeGrader(): a regex over artefacts/handoff.json
// whose bounds come from taskflow.MaxResultBytes, the cap cw task handoff
// enforces. A case without it, or with another bound, fails with its path.
func TestReviewerCasesGradeTheHandoffSize(t *testing.T) {
	entries, err := os.ReadDir("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	cases := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		cases++
		file := filepath.Join("reviewer", entry.Name(), "graders", "handoff-size.md")
		data, err := os.ReadFile(file)
		if err != nil {
			t.Errorf("%s: %v: every reviewer case grades the size of its handoff", file, err)
			continue
		}
		if string(data) != handoffSizeGrader() {
			t.Errorf("%s differs from the grader for a %d-byte cap:\n%s", file, taskflow.MaxResultBytes, data)
		}
	}
	if cases == 0 {
		t.Fatal("found no case under evals/reviewer")
	}
}

// handoffSizeAccepts models the grader's pattern on text: its first branch
// takes ASCII-only text of at most limit UTF-16 units, its second text with 1
// to nonASCII units outside ASCII and at most limit-2*nonASCII units. The
// JavaScript engine is not run offline; this model is what the pattern is
// written to mean.
func handoffSizeAccepts(text string, limit, nonASCII int) bool {
	units := utf16.Encode([]rune(text))
	outside := 0
	for _, unit := range units {
		if unit > 0x7F {
			outside++
		}
	}
	if outside == 0 {
		return len(units) <= limit
	}
	return outside <= nonASCII && len(units) <= limit-2*nonASCII
}

// oracle: at the edges of both branches the grader passes a handoff of
// exactly 6,144 bytes and fails one of 6,145; and over generated text mixing
// ASCII with two-, three- and four-byte characters it never passes text over
// 6,144 bytes.
func TestHandoffSizeGraderNeverPassesAnOversizeHandoff(t *testing.T) {
	limit := taskflow.MaxResultBytes
	accepts := func(text string) bool { return handoffSizeAccepts(text, limit, handoffSizeNonASCII) }
	ascii := func(n int) string { return strings.Repeat("a", n) }
	arrows := strings.Repeat("→", handoffSizeNonASCII) // three bytes each
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", true},
		{"ASCII at the cap", ascii(limit), true},
		{"ASCII one byte over", ascii(limit + 1), false},
		{"ASCII with a final newline at the cap", ascii(limit-1) + "\n", true},
		{"64 three-byte characters at the cap", ascii(limit-3*handoffSizeNonASCII) + arrows, true},
		{"64 three-byte characters one byte over", ascii(limit-3*handoffSizeNonASCII+1) + arrows, false},
		{"65 characters outside ASCII", ascii(100) + strings.Repeat("é", handoffSizeNonASCII+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := accepts(tc.text); got != tc.want {
				t.Errorf("accepts %d bytes = %v, want %v", len(tc.text), got, tc.want)
			}
			if tc.want && len(tc.text) > limit {
				t.Fatalf("the sample holds %d bytes, over the cap", len(tc.text))
			}
		})
	}

	random := rand.New(rand.NewPCG(1, 2))
	outside := []string{"é", "→", "中", "\U0001F600"}
	passed := 0
	for range 5000 {
		var text strings.Builder
		for range random.IntN(handoffSizeNonASCII + 8) {
			text.WriteString(ascii(random.IntN(200)))
			text.WriteString(outside[random.IntN(len(outside))])
		}
		text.WriteString(ascii(random.IntN(limit)))
		if accepts(text.String()) {
			passed++
			if text.Len() > limit {
				t.Fatalf("the grader passes %d bytes, over the cap of %d", text.Len(), limit)
			}
		}
	}
	if passed == 0 {
		t.Fatal("no generated text passed: the property checked nothing")
	}
}
