package taskflow

import (
	"strings"
	"testing"
)

// checkpointsDecodePlanText builds a minimal plan document whose checkpoints member
// is the given raw JSON fragment. An empty fragment omits the member.
func checkpointsDecodePlanText(fragment string) string {
	if fragment == "" {
		return `{"title": "t", "kind": "substantial"}`
	}
	return `{"title": "t", "kind": "substantial", "checkpoints": ` + fragment + `}`
}

// TestCheckpointsDecodeRejectsInvalidPolicy covers C1, C4 and C6: a checkpoints
// object must name "required" explicitly and may not carry unknown members.
func TestCheckpointsDecodeRejectsInvalidPolicy(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
	}{
		// C1: required omitted, only a reason given.
		{name: "C1 required omitted with reason", fragment: `{"reason": "x"}`},
		// C4: misspelled required member.
		{name: "C4 misspelled required", fragment: `{"requried": false, "reason": "x"}`}, //nolint:misspell // the misspelling is the fixture
		// C4: unknown member alongside an explicit required.
		{name: "C4 unknown member with explicit required", fragment: `{"required": true, "extra": "y"}`},
		// C6: empty checkpoints object.
		{name: "C6 empty object", fragment: `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := checkpointsDecodePlanText(tc.fragment)
			var plan Plan
			err := Decode(strings.NewReader(text), &plan)
			if err == nil {
				var got string
				if plan.Checkpoints == nil {
					got = "<nil>"
				} else {
					got = "required=" + checkpointsDecodeBoolText(plan.Checkpoints.Required) + " reason=" + plan.Checkpoints.Reason
				}
				t.Fatalf("Decode(%s) returned nil error; want an error (decoded checkpoints: %s)", text, got)
			}
		})
	}
}

// TestCheckpointsDecodeAcceptsExplicitPolicy covers C2 and C3: an explicit
// required value decodes to a non-nil policy carrying the given values.
func TestCheckpointsDecodeAcceptsExplicitPolicy(t *testing.T) {
	cases := []struct {
		name         string
		fragment     string
		wantRequired bool
		wantReason   string
	}{
		// C2
		{name: "C2 required false with reason", fragment: `{"required": false, "reason": "x"}`, wantRequired: false, wantReason: "x"},
		// C3
		{name: "C3 required true without reason", fragment: `{"required": true}`, wantRequired: true, wantReason: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := checkpointsDecodePlanText(tc.fragment)
			var plan Plan
			if err := Decode(strings.NewReader(text), &plan); err != nil {
				t.Fatalf("Decode(%s) error = %v; want nil", text, err)
			}
			if plan.Checkpoints == nil {
				t.Fatalf("Decode(%s) Checkpoints = nil; want non-nil", text)
			}
			if plan.Checkpoints.Required != tc.wantRequired {
				t.Errorf("Decode(%s) Checkpoints.Required = %v; want %v", text, plan.Checkpoints.Required, tc.wantRequired)
			}
			if plan.Checkpoints.Reason != tc.wantReason {
				t.Errorf("Decode(%s) Checkpoints.Reason = %q; want %q", text, plan.Checkpoints.Reason, tc.wantReason)
			}
		})
	}
}

// TestCheckpointsDecodeAbsentPolicy covers C5: omitting checkpoints or giving
// null decodes without error to a nil policy.
func TestCheckpointsDecodeAbsentPolicy(t *testing.T) {
	cases := []struct {
		name     string
		fragment string
	}{
		{name: "C5 checkpoints omitted", fragment: ""},
		{name: "C5 checkpoints null", fragment: `null`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := checkpointsDecodePlanText(tc.fragment)
			var plan Plan
			if err := Decode(strings.NewReader(text), &plan); err != nil {
				t.Fatalf("Decode(%s) error = %v; want nil", text, err)
			}
			if plan.Checkpoints != nil {
				t.Fatalf("Decode(%s) Checkpoints = %+v; want nil", text, *plan.Checkpoints)
			}
		})
	}
}

func checkpointsDecodeBoolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
