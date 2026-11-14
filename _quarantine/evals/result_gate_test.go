package evals

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// Authored tests for checkResult (G1-G6).

type authoredResultCase struct {
	name     string
	score    float64
	armError string // empty: null
}

// authoredResultJSON renders an aggregate-result.json document with some
// fields the gate must ignore.
func authoredResultJSON(schemaVersion string, partial bool, reason string, cases []authoredResultCase) []byte {
	var b strings.Builder
	b.WriteString(`{"schemaVersion": ` + schemaVersion + `, "generatedAt": "2026-11-07T00:00:00Z", "unknownTop": {"x": [1, 2]}`)
	fmt.Fprintf(&b, `, "partial": %v, "partialReason": %q, "cases": [`, partial, reason)
	for i, c := range cases {
		if i > 0 {
			b.WriteString(", ")
		}
		armErr := "null"
		if c.armError != "" {
			armErr = strconv.Quote(c.armError)
		}
		fmt.Fprintf(&b, `{"name": %q, "extra": true, "aggregates": {"score": %s, "passRate": 1}, "arms": {"with": [{"run": 1, "error": null}, {"run": 2, "error": %s}], "without": [{"error": "ignored"}]}}`,
			c.name, strconv.FormatFloat(c.score, 'g', -1, 64), armErr)
	}
	b.WriteString("]}")
	return []byte(b.String())
}

// authoredScoreForms are renderings a summary may use for a score.
func authoredScoreForms(score float64) []string {
	return []string{
		strconv.FormatFloat(score, 'g', -1, 64),
		strconv.FormatFloat(score, 'f', 2, 64),
		strconv.FormatFloat(score, 'f', 3, 64),
		strconv.FormatFloat(score*100, 'f', 0, 64) + "%",
		strconv.FormatFloat(score*100, 'f', 1, 64) + "%",
	}
}

// authoredWantCaseLine checks that some summary line names the case and its score.
func authoredWantCaseLine(t *testing.T, summary, name string, score float64) {
	t.Helper()
	for _, line := range strings.Split(summary, "\n") {
		if !strings.Contains(line, name) {
			continue
		}
		for _, f := range authoredScoreForms(score) {
			if strings.Contains(line, f) {
				return
			}
		}
	}
	t.Fatalf("summary has no line naming %q with score %v (forms %q):\n%s", name, score, authoredScoreForms(score), summary)
}

var authoredPassingCases = []authoredResultCase{
	{name: "alpha-case", score: 0.9},
	{name: "beta-case", score: 0.75},
}

func TestAuthoredCheckResultPasses(t *testing.T) {
	summary, err := checkResult(authoredResultJSON("1", false, "", authoredPassingCases), 0.7)
	if err != nil {
		t.Fatalf("checkResult: unexpected error %v", err)
	}
	authoredWantCaseLine(t, summary, "alpha-case", 0.9)
	authoredWantCaseLine(t, summary, "beta-case", 0.75)
}

// G1: malformed JSON and schema versions other than 1.
func TestAuthoredCheckResultMalformed(t *testing.T) {
	inputs := map[string][]byte{
		"not json":             []byte("not json"),
		"truncated":            authoredResultJSON("1", false, "", authoredPassingCases)[:40],
		"schemaVersion 2":      authoredResultJSON("2", false, "", authoredPassingCases),
		"schemaVersion 0":      authoredResultJSON("0", false, "", authoredPassingCases),
		"schemaVersion 1.5":    authoredResultJSON("1.5", false, "", authoredPassingCases),
		"schemaVersion absent": []byte(`{"partial": false, "cases": [{"name": "alpha-case", "aggregates": {"score": 1}, "arms": {"with": []}}]}`),
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			if _, err := checkResult(data, 0.5); err == nil {
				t.Fatal("checkResult: want an error, got nil")
			}
		})
	}
}

// G2: a partial run fails and the error carries partialReason.
func TestAuthoredCheckResultPartial(t *testing.T) {
	reason := "cost ceiling of 20 USD reached after 3 cases"
	_, err := checkResult(authoredResultJSON("1", true, reason, authoredPassingCases), 0.5)
	if err == nil {
		t.Fatal("checkResult: want an error for a partial run, got nil")
	}
	if !strings.Contains(err.Error(), reason) {
		t.Fatalf("error %q does not contain partialReason %q", err, reason)
	}
}

// G3: no cases is an error.
func TestAuthoredCheckResultNoCases(t *testing.T) {
	inputs := map[string][]byte{
		"empty cases":  authoredResultJSON("1", false, "", nil),
		"absent cases": []byte(`{"schemaVersion": 1, "partial": false}`),
		"null cases":   []byte(`{"schemaVersion": 1, "partial": false, "cases": null}`),
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			if _, err := checkResult(data, 0.5); err == nil {
				t.Fatal("checkResult: want an error, got nil")
			}
		})
	}
}

// G4: every case below the floor is named; a score equal to the floor passes.
func TestAuthoredCheckResultBelowFloor(t *testing.T) {
	cases := []authoredResultCase{
		{name: "high-case", score: 0.9},
		{name: "low-one-case", score: 0.5},
		{name: "edge-case", score: 0.8},
		{name: "low-two-case", score: 0.79},
	}
	summary, err := checkResult(authoredResultJSON("1", false, "", cases), 0.8)
	if err == nil {
		t.Fatal("checkResult: want an error, got nil")
	}
	for _, name := range []string{"low-one-case", "low-two-case"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name %s", err, name)
		}
	}
	// G6: the summary still lists every case and its score.
	for _, c := range cases {
		authoredWantCaseLine(t, summary, c.name, c.score)
	}
}

func TestAuthoredCheckResultFloorEquality(t *testing.T) {
	cases := []struct {
		floor float64
		score float64
	}{
		{0.8, 0.8},
		{0, 0},
		{1, 1},
		{0.5, 0.75},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("floor=%v/score=%v", tc.floor, tc.score), func(t *testing.T) {
			data := authoredResultJSON("1", false, "", []authoredResultCase{{name: "only-case", score: tc.score}})
			if _, err := checkResult(data, tc.floor); err != nil {
				t.Fatalf("checkResult: unexpected error %v", err)
			}
		})
	}
	t.Run("just below", func(t *testing.T) {
		data := authoredResultJSON("1", false, "", []authoredResultCase{{name: "only-case", score: 0.7999}})
		if _, err := checkResult(data, 0.8); err == nil {
			t.Fatal("checkResult: want an error for 0.7999 < 0.8, got nil")
		}
	})
}

// G5: a floor outside [0, 1] is an error even for a passing run.
func TestAuthoredCheckResultFloorRange(t *testing.T) {
	for _, floor := range []float64{-0.01, 1.01, 2} {
		t.Run(fmt.Sprint(floor), func(t *testing.T) {
			if _, err := checkResult(authoredResultJSON("1", false, "", authoredPassingCases), floor); err == nil {
				t.Fatalf("checkResult(floor=%v): want an error, got nil", floor)
			}
		})
	}
}

// G6: the summary names a case whose with-arm run errored, so it differs
// from the summary of the same run without the error.
func TestAuthoredCheckResultSummaryNamesErroredRuns(t *testing.T) {
	clean := []authoredResultCase{
		{name: "steady-case", score: 1},
		{name: "flaky-case", score: 1},
	}
	errored := []authoredResultCase{
		{name: "steady-case", score: 1},
		{name: "flaky-case", score: 1, armError: "agent timed out"},
	}
	cleanSummary, _ := checkResult(authoredResultJSON("1", false, "", clean), 0.5)
	erroredSummary, _ := checkResult(authoredResultJSON("1", false, "", errored), 0.5)
	authoredWantCaseLine(t, erroredSummary, "steady-case", 1)
	authoredWantCaseLine(t, erroredSummary, "flaky-case", 1)
	if cleanSummary == erroredSummary {
		t.Fatalf("summary does not report the errored with-arm run of flaky-case:\n%s", erroredSummary)
	}
}

// G6: a partial run's summary still names every case and score.
func TestAuthoredCheckResultSummaryOnPartial(t *testing.T) {
	summary, err := checkResult(authoredResultJSON("1", true, "stopped", authoredPassingCases), 0.5)
	if err == nil {
		t.Fatal("checkResult: want an error for a partial run, got nil")
	}
	authoredWantCaseLine(t, summary, "alpha-case", 0.9)
	authoredWantCaseLine(t, summary, "beta-case", 0.75)
}
