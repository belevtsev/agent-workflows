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

// The handoff-size grader of the reviewer suite (D15): the handoff a spec,
// design or test reviewer writes to artefacts/handoff.json holds at most
// taskflow.MaxResultBytes (6,144) bytes. A regex grader counts UTF-16 units,
// not bytes, so the pattern has one branch per row of handoffSizeTiers: at
// most nonASCII units outside ASCII and at most 6,144 - 2*nonASCII units in
// all. A unit outside ASCII adds at most two bytes to its own (three bytes
// for one unit, four for a surrogate pair), so no branch passes a file over
// the cap. The last branch puts no bound on the units outside ASCII: 2,048
// units are at most 6,144 bytes. A file under the cap still fails when it
// holds text outside ASCII and more units than its tier allows.
//
// The code-review cases carry no such grader: cw task handoff measures the
// cap without changed_files, reach_runs and requirement_map
// (internal/taskflow/report.go, handoffCaps), a code reviewer's result holds
// reach_runs, and a regex cannot leave a member out of the count.
var handoffSizeTiers = []int{0, 64, 512}

// handoffSizeUnbounded is the most units a file may hold whatever they are.
const handoffSizeUnbounded = taskflow.MaxResultBytes / 3

// handoffSizeGrader is the grader file of every reviewer case whose role
// returns no reach_runs.
func handoffSizeGrader() string {
	var branches []string
	for _, nonASCII := range handoffSizeTiers {
		units := taskflow.MaxResultBytes - 2*nonASCII
		if nonASCII == 0 {
			branches = append(branches, fmt.Sprintf(`[\x00-\x7F]{0,%d}`, units))
			continue
		}
		branches = append(branches, fmt.Sprintf(`(?=[\s\S]{0,%d}$)(?:[\x00-\x7F]*[^\x00-\x7F]){1,%d}[\x00-\x7F]*`, units, nonASCII))
	}
	branches = append(branches, fmt.Sprintf(`[\s\S]{0,%d}`, handoffSizeUnbounded))
	return "---\ntype: regex\npattern: '^(?:" + strings.Join(branches, "|") + ")$'\n" +
		"target: { source: file, path: artefacts/handoff.json }\n---\n"
}

// oracle: every case under evals/reviewer whose role returns no reach_runs
// (the spec, design and tests reviews) has graders/handoff-size.md, and its
// bytes are handoffSizeGrader(): a regex over artefacts/handoff.json whose
// bounds come from taskflow.MaxResultBytes, the cap cw task handoff enforces.
// A code-review case has no such file, since its valid result can exceed the
// cap as a file. A case that breaks either rule fails with its path.
func TestReviewerCasesGradeTheHandoffSize(t *testing.T) {
	entries, err := os.ReadDir("reviewer")
	if err != nil {
		t.Fatal(err)
	}
	graded, code := 0, 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		file := filepath.Join("reviewer", name, "graders", "handoff-size.md")
		data, err := os.ReadFile(file)
		switch {
		case strings.HasPrefix(name, "code-"):
			code++
			if err == nil {
				t.Errorf("%s exists: a code review returns reach_runs, which the 6 KiB cap leaves out, so its file may be larger", file)
			}
		case !strings.HasPrefix(name, "spec-") && !strings.HasPrefix(name, "design-") && !strings.HasPrefix(name, "tests-"):
			t.Errorf("reviewer/%s is not a spec, design, tests or code case: say whether its role returns reach_runs", name)
		case err != nil:
			graded++
			t.Errorf("%s: %v: every spec, design and tests review case grades the size of its handoff", file, err)
		default:
			graded++
			if string(data) != handoffSizeGrader() {
				t.Errorf("%s differs from the grader for a %d-byte cap:\n%s", file, taskflow.MaxResultBytes, data)
			}
		}
	}
	if graded == 0 || code == 0 {
		t.Fatalf("found %d graded cases and %d code-review cases under evals/reviewer, want both", graded, code)
	}
}

// handoffSizeAccepts models the grader's pattern on text: a tier takes text
// with at most its count of UTF-16 units outside ASCII and at most
// limit-2*count units, and text of at most limit/3 units passes whatever it
// holds. The JavaScript engine is not run offline; this model is what the
// pattern is written to mean.
func handoffSizeAccepts(text string, limit int, tiers []int) bool {
	units := utf16.Encode([]rune(text))
	outside := 0
	for _, unit := range units {
		if unit > 0x7F {
			outside++
		}
	}
	for _, nonASCII := range tiers {
		if outside <= nonASCII && len(units) <= limit-2*nonASCII {
			return true
		}
	}
	return len(units) <= limit/3
}

// oracle: at the edge of every branch the grader passes a handoff of exactly
// 6,144 bytes and fails one a byte or a unit over; and over generated text
// mixing ASCII with two-, three- and four-byte characters it never passes
// text over 6,144 bytes.
func TestHandoffSizeGraderNeverPassesAnOversizeHandoff(t *testing.T) {
	limit := taskflow.MaxResultBytes
	accepts := func(text string) bool { return handoffSizeAccepts(text, limit, handoffSizeTiers) }
	ascii := func(n int) string { return strings.Repeat("a", n) }
	arrows := func(n int) string { return strings.Repeat("→", n) } // three bytes each
	for _, tc := range []struct {
		name string
		text string
		want bool
	}{
		{"empty", "", true},
		{"ASCII at the cap", ascii(limit), true},
		{"ASCII one byte over", ascii(limit + 1), false},
		{"ASCII with a final newline at the cap", ascii(limit-1) + "\n", true},
		{"64 three-byte characters at the cap", ascii(limit-3*64) + arrows(64), true},
		{"64 three-byte characters one byte over", ascii(limit-3*64+1) + arrows(64), false},
		{"65 characters outside ASCII in 5,120 units", ascii(5120-65) + arrows(65), true},
		{"65 characters outside ASCII in 5,121 units", ascii(5121-65) + arrows(65), false},
		{"512 three-byte characters at the cap", ascii(limit-3*512) + arrows(512), true},
		{"512 three-byte characters one byte over", ascii(limit-3*512+1) + arrows(512), false},
		{"2,048 three-byte characters at the cap", arrows(2048), true},
		{"2,049 three-byte characters", arrows(2049), false},
		{"513 characters outside ASCII in 2,049 units", ascii(2049-513) + arrows(513), false},
		{"1,024 surrogate pairs at 4,096 bytes", strings.Repeat("\U0001F600", 1024), true},
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
	passed := map[bool]int{}
	for range 20000 {
		var text strings.Builder
		for range random.IntN([]int{8, 72, 520, 2100}[random.IntN(4)]) {
			text.WriteString(ascii(random.IntN(12)))
			text.WriteString(outside[random.IntN(len(outside))])
		}
		text.WriteString(ascii(random.IntN(limit)))
		ok := accepts(text.String())
		passed[ok]++
		if ok && text.Len() > limit {
			t.Fatalf("the grader passes %d bytes, over the cap of %d", text.Len(), limit)
		}
	}
	if passed[true] == 0 || passed[false] == 0 {
		t.Fatalf("generated text passed %d times and failed %d: the property needs both", passed[true], passed[false])
	}
}
