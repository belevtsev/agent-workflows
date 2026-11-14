package taskflow

import (
	"strings"
	"testing"
)

// A substantial plan must bind at least one check to a requirement; a small
// plan may still rely on non-execution alone.
func TestValidatePlanRequiresABoundCheckForSubstantialPlans(t *testing.T) {
	unit := Check{ID: "unit", Argv: []string{"go", "test", "./..."}}
	unbound := Requirement{ID: "source", Description: "Inspect actual source", CheckIDs: []string{}, AllowNonExecution: true}
	bound := Requirement{ID: "source", Description: "Inspect actual source", CheckIDs: []string{"unit"}, AllowNonExecution: true}
	for _, tc := range []struct {
		name    string
		kind    string
		checks  []Check
		require Requirement
		refused bool
	}{
		{"substantial without checks", "substantial", []Check{}, unbound, true},
		{"substantial with an unbound check", "substantial", []Check{unit}, unbound, true},
		{"substantial with a bound check", "substantial", []Check{unit}, bound, false},
		{"small without checks", "small", []Check{}, unbound, false},
		{"small with an unbound check", "small", []Check{unit}, unbound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := Plan{Title: "Plan", Kind: tc.kind, Requirements: []Requirement{tc.require}, Checks: tc.checks}
			err := ValidatePlan(&plan)
			if tc.refused != (err != nil) {
				t.Fatalf("ValidatePlan refused=%v, want %v: %v", err != nil, tc.refused, err)
			}
			if tc.refused && !strings.Contains(err.Error(), "needs at least one check bound to a requirement") {
				t.Fatalf("refusal does not name the rule: %v", err)
			}
		})
	}
}
