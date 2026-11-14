package evals

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestEvalResult gates a finished `claude plugin eval --json` run. It is
// skipped unless EVAL_RESULT_JSON names the run's result document; the floor
// is EVAL_PASS_FLOOR (default 0.8), the minimum with-arm score of every case.
func TestEvalResult(t *testing.T) {
	file := os.Getenv("EVAL_RESULT_JSON")
	if file == "" {
		t.Skip("EVAL_RESULT_JSON is not set; it names a finished run's aggregate-result.json")
	}
	floor := 0.8
	if v := os.Getenv("EVAL_PASS_FLOOR"); v != "" {
		parsed, err := strconv.ParseFloat(v, 64)
		if err != nil {
			t.Fatalf("EVAL_PASS_FLOOR %q: %v", v, err)
		}
		floor = parsed
	}
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read the eval result: %v (did the run write --json output?)", err)
	}
	summary, err := checkResult(data, floor)
	t.Log("\n" + summary)
	if err != nil {
		t.Fatal(err)
	}
}

type evalResult struct {
	SchemaVersion *int   `json:"schemaVersion"`
	Partial       bool   `json:"partial"`
	PartialReason string `json:"partialReason"`
	Cases         []struct {
		Name       string `json:"name"`
		Aggregates struct {
			Score *float64 `json:"score"`
		} `json:"aggregates"`
		Arms struct {
			With []struct {
				Error *string `json:"error"`
			} `json:"with"`
		} `json:"arms"`
	} `json:"cases"`
}

// checkResult validates an aggregate-result.json document. It returns a
// summary listing every case with its score, and an error when the run is not
// a complete schema-1 result or any case scored below floor.
func checkResult(data []byte, floor float64) (string, error) {
	if floor < 0 || floor > 1 {
		return "", fmt.Errorf("pass floor %v must be between 0 and 1", floor)
	}
	var result evalResult
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("eval result is not valid JSON: %w", err)
	}
	if result.SchemaVersion == nil || *result.SchemaVersion != 1 {
		return "", errors.New("eval result: schemaVersion is not 1; update checkResult for the new format")
	}
	var summary strings.Builder
	var below []string
	for _, c := range result.Cases {
		runErrors := 0
		first := ""
		for _, run := range c.Arms.With {
			if run.Error != nil {
				if runErrors == 0 {
					first = *run.Error
				}
				runErrors++
			}
		}
		score := "none"
		if c.Aggregates.Score != nil {
			score = strconv.FormatFloat(*c.Aggregates.Score, 'f', 3, 64)
		}
		fmt.Fprintf(&summary, "%-32s %s", c.Name, score)
		if runErrors > 0 {
			fmt.Fprintf(&summary, "  (%d run error(s), first: %s)", runErrors, first)
		}
		summary.WriteString("\n")
		if c.Aggregates.Score == nil || *c.Aggregates.Score < floor {
			below = append(below, fmt.Sprintf("%s (%s)", c.Name, score))
		}
	}
	switch {
	case result.Partial:
		return summary.String(), fmt.Errorf("eval run is partial (%s): not every run started, so the scores are incomplete", result.PartialReason)
	case len(result.Cases) == 0:
		return summary.String(), errors.New("eval result has no cases")
	case len(below) > 0:
		return summary.String(), fmt.Errorf("%d case(s) scored below the pass floor %v: %s", len(below), floor, strings.Join(below, ", "))
	}
	return summary.String(), nil
}
