package harness

import (
	"testing"
	"time"
)

func TestReplacedBriefNamesOnlyAVerificationThatHadNotPassed(t *testing.T) {
	old := &FeatureBrief{Version: 1, Goal: "Export", Scope: "One format", Criteria: []Criterion{{ID: "a", Expected: "A", Check: "check:unit"}}}
	revised := *old
	revised.Scope = "Less"
	passed := Proof{Run: "run-1", Outcome: "passed", FeatureRevision: old.Revision(), CheckedAt: time.Now()}
	failedCheck, failed, unfinished, earlier := passed, passed, passed, passed
	failedCheck.GateFailure, failedCheck.FailedCheck = &RunResult{}, "unit"
	failed.Outcome = "failed"
	unfinished.CheckedAt = time.Time{}
	earlier.Outcome, earlier.FeatureRevision = "failed", "another revision"
	for name, c := range map[string]struct {
		proof   *Proof
		brief   *FeatureBrief
		replace bool
	}{
		"a failed behavior":                   {&failed, &revised, true},
		"a failed repository check":           {&failedCheck, &revised, true},
		"a verification that did not finish":  {&unfinished, &revised, true},
		"criteria that had passed":            {&passed, &revised, false},
		"the same brief recorded again":       {&failed, old, false},
		"a failure under an earlier revision": {&earlier, &revised, false},
		"no verification yet":                 {nil, &revised, false},
	} {
		task := Task{Feature: old, Proof: c.proof}
		if got := task.ReplacedBrief(c.brief); (got != nil) != c.replace || got != nil && got.Run != "run-1" {
			t.Errorf("%s: replaced = %+v, want a trace: %v", name, got, c.replace)
		}
	}
	if (&Task{}).ReplacedBrief(old) != nil {
		t.Error("a first brief replaced nothing")
	}
}
