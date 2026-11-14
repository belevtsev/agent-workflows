package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// Contract: token-levers revision 1, section C (conditional adviser checkpoints).

const checkpointsTestReason = "Single-file documentation fix; adviser checkpoints add no decision value here."

func checkpointsTestLimitation(reason string) string {
	return "adviser checkpoints not required: " + reason
}

// checkpointsTestPlan is the substantial helper plan with the given policy.
func checkpointsTestPlan(t *testing.T, policy *CheckpointPolicy) Plan {
	t.Helper()
	plan := substantialPlan(t)
	plan.Checkpoints = policy
	return plan
}

func checkpointsTestOptional(t *testing.T) Plan {
	t.Helper()
	return checkpointsTestPlan(t, &CheckpointPolicy{Required: false, Reason: checkpointsTestReason})
}

// checkpointsTestRefusedStart asserts that Start fails and leaves no task
// directory in the store.
func checkpointsTestRefusedStart(t *testing.T, label string, plan Plan) {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("first line\nsecond line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "tasks")
	if _, err := StartWithPacks(t.Context(), repo, store, plan, fixtureBinding(), SelectionExplicit, nil); err == nil {
		t.Fatalf("%s: Start accepted the plan", label)
	}
	if _, err := os.Stat(store); errors.Is(err, os.ErrNotExist) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	if names := taskArtifactNames(t, store); len(names) != 0 {
		t.Fatalf("%s: refused Start left task artifacts: %q", label, names)
	}
}

func checkpointsTestCount(lines []string, want string) int {
	count := 0
	for _, line := range lines {
		if line == want {
			count++
		}
	}
	return count
}

// checkpointsTestRecordFields reads one stored revision's top-level members
// and its plan members.
func checkpointsTestRecordFields(t *testing.T, directory string, revision string) (map[string]jsontext.Value, map[string]jsontext.Value) {
	t.Helper()
	data := productionTaskRead(t, filepath.Join(directory, "record-"+revision+".json"))
	fields := map[string]jsontext.Value{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	plan := map[string]jsontext.Value{}
	if err := json.Unmarshal(fields["plan"], &plan); err != nil {
		t.Fatal(err)
	}
	return fields, plan
}

// C1: the plan's optional "checkpoints" member is {"required", "reason"};
// strict decoding accepts it, rejects unknown members inside it, and an
// omitted member decodes as nil.
func TestPlanCheckpointsMemberDecodes(t *testing.T) {
	plan := checkpointsTestOptional(t)
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	want := `"checkpoints":{"required":false,"reason":"` + checkpointsTestReason + `"}`
	if !bytes.Contains(data, []byte(want)) {
		t.Fatalf("plan JSON lacks %s: %s", want, data)
	}
	var decoded Plan
	if err := Decode(bytes.NewReader(data), &decoded); err != nil {
		t.Fatalf("plan with checkpoints rejected: %v", err)
	}
	if decoded.Checkpoints == nil || decoded.Checkpoints.Required || decoded.Checkpoints.Reason != checkpointsTestReason {
		t.Fatalf("decoded checkpoints = %+v", decoded.Checkpoints)
	}
	extra := bytes.Replace(data, []byte(want), []byte(`"checkpoints":{"required":false,"reason":"`+checkpointsTestReason+`","skip":true}`), 1)
	if Decode(bytes.NewReader(extra), &Plan{}) == nil {
		t.Fatal("unknown member inside checkpoints accepted")
	}
	omitted, err := json.Marshal(substantialPlan(t))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(omitted, []byte(`"checkpoints"`)) {
		t.Fatalf("plan without a policy serializes a checkpoints member: %s", omitted)
	}
	var legacy Plan
	if err := Decode(bytes.NewReader(omitted), &legacy); err != nil || legacy.Checkpoints != nil {
		t.Fatalf("plan without checkpoints: %+v %v", legacy.Checkpoints, err)
	}
}

// C2: required:false needs a non-blank bounded reason, and a small plan may
// not carry checkpoints at all; a refused Start creates no task directory.
func TestStartRefusesInvalidCheckpointPolicy(t *testing.T) {
	for name, plan := range map[string]Plan{
		"empty_reason":     checkpointsTestPlan(t, &CheckpointPolicy{Required: false}),
		"blank_reason":     checkpointsTestPlan(t, &CheckpointPolicy{Required: false, Reason: " \t\n "}),
		"oversized_reason": checkpointsTestPlan(t, &CheckpointPolicy{Required: false, Reason: strings.Repeat("x", 1<<20)}),
		"small_not_required": func() Plan {
			p := helperPlan(t, "small", "pass")
			p.Checkpoints = &CheckpointPolicy{Required: false, Reason: checkpointsTestReason}
			return p
		}(),
		"small_required": func() Plan {
			p := helperPlan(t, "small", "pass")
			p.Checkpoints = &CheckpointPolicy{Required: true}
			return p
		}(),
	} {
		t.Run(name, func(t *testing.T) {
			checkpointsTestRefusedStart(t, name, plan)
		})
	}
	for name, plan := range map[string]Plan{
		"not_required_with_reason": checkpointsTestOptional(t),
		"required":                 checkpointsTestPlan(t, &CheckpointPolicy{Required: true}),
		"omitted":                  substantialPlan(t),
	} {
		t.Run("accepts_"+name, func(t *testing.T) {
			record, _ := fixtureTask(t, plan)
			if record.Directory == "" {
				t.Fatal("valid plan did not create a task")
			}
		})
	}
}

// C3 and C6: with required:false a substantial task dispatches without a
// before checkpoint; with required:true or the member omitted the dispatch
// refusal is unchanged.
func TestNotRequiredCheckpointsAllowDispatchWithoutBefore(t *testing.T) {
	record, _ := fixtureTask(t, checkpointsTestOptional(t))
	updated, err := AssignWorker(t.Context(), record.Directory, implementationBrief())
	if err != nil || len(updated.Assignments) != 1 {
		t.Fatalf("required:false dispatch without a before checkpoint: %v", err)
	}
	for name, plan := range map[string]Plan{
		"required": checkpointsTestPlan(t, &CheckpointPolicy{Required: true}),
		"omitted":  substantialPlan(t),
	} {
		t.Run(name, func(t *testing.T) {
			record, _ := fixtureTask(t, plan)
			if _, err := AssignWorker(t.Context(), record.Directory, implementationBrief()); err == nil || !strings.Contains(err.Error(), fixtureAssignRefusal) {
				t.Fatalf("%s checkpoints: dispatch before a before checkpoint: %v", name, err)
			}
		})
	}
}

// C4 and C6: with required:false verify completes without before or final
// checkpoints and adds exactly one limitation; with required:true or omitted
// the same flow stays incomplete and carries no such limitation.
func TestNotRequiredCheckpointsVerifyWithoutCheckpoints(t *testing.T) {
	t.Parallel()
	for _, flow := range []string{"unchanged_source", "changed_source_with_review"} {
		for bindingName, binding := range map[string]map[string]string{"bound_advisers": fixtureBinding(), "no_advisers": {}} {
			for policyName, policy := range map[string]*CheckpointPolicy{
				"not_required": {Required: false, Reason: checkpointsTestReason},
				"required":     {Required: true},
				"omitted":      nil,
			} {
				t.Run(flow+"/"+bindingName+"/"+policyName, func(t *testing.T) {
					record, ref := fixtureTaskBound(t, checkpointsTestPlan(t, policy), binding)
					if flow == "changed_source_with_review" {
						ref = changedWorkerSource(t, record)
					}
					mustAssess(t, record, ref)
					if flow == "changed_source_with_review" && policy != nil && !policy.Required {
						record = completeIndependentReview(t, record, ref, "review")
					}
					verified, err := Verify(t.Context(), record.Directory)
					limitation := checkpointsTestLimitation(checkpointsTestReason)
					if policy != nil && !policy.Required {
						if err != nil || !verified.Complete || len(verified.Problems) != 0 {
							t.Fatalf("required:false task without checkpoints: %+v %v", verified, err)
						}
						if got := checkpointsTestCount(verified.Limitations, limitation); got != 1 {
							t.Fatalf("limitation %q appears %d times: %q", limitation, got, verified.Limitations)
						}
						return
					}
					if verified.Complete {
						t.Fatalf("%s checkpoints: task completed without checkpoints: %+v", policyName, verified)
					}
					if limitationNaming(verified.Limitations, "adviser checkpoints not required") {
						t.Fatalf("%s checkpoints: not-required limitation reported: %q", policyName, verified.Limitations)
					}
				})
			}
		}
	}
}

// C4: with required:false every other gate is unchanged.
func TestNotRequiredCheckpointsKeepOtherGates(t *testing.T) {
	t.Parallel()
	t.Run("requirements", func(t *testing.T) {
		record, _ := fixtureTask(t, checkpointsTestOptional(t))
		if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
			t.Fatal(err)
		}
		if verified, _ := Verify(t.Context(), record.Directory); verified.Complete || len(verified.Problems) == 0 {
			t.Fatalf("unassessed requirement completed a required:false task: %+v", verified)
		}
	})
	t.Run("checks", func(t *testing.T) {
		plan := helperPlan(t, "substantial", "fail")
		plan.Checkpoints = &CheckpointPolicy{Required: false, Reason: checkpointsTestReason}
		record, ref := fixtureTask(t, plan)
		if run, err := RunCheck(t.Context(), record.Directory, "focused"); err == nil || run.Status != "failed" {
			t.Fatalf("failure fixture did not fail: %+v %v", run, err)
		}
		_, _ = Assess(t.Context(), record.Directory, assessment(ref, "focused"))
		if verified, _ := Verify(t.Context(), record.Directory); verified.Complete || len(verified.Problems) == 0 {
			t.Fatalf("failed check completed a required:false task: %+v", verified)
		}
	})
	t.Run("worker_records", func(t *testing.T) {
		record, ref := fixtureTask(t, checkpointsTestOptional(t))
		mustAssignWorker(t, record, workerBrief("reader", "/root/reader", "evidence", []string{"source.txt"}))
		mustAssess(t, record, ref)
		verified, _ := Verify(t.Context(), record.Directory)
		if verified.Complete || !strings.Contains(strings.Join(verified.Problems, " "), "no terminal handoff") {
			t.Fatalf("orphan assignment completed a required:false task: %+v", verified)
		}
	})
	t.Run("independent_review", func(t *testing.T) {
		record, _ := fixtureTask(t, checkpointsTestOptional(t))
		ref := changedWorkerSource(t, record)
		mustAssess(t, record, ref)
		if verified, _ := Verify(t.Context(), record.Directory); verified.Complete {
			t.Fatalf("changed source without review completed a required:false task: %+v", verified)
		}
		if len(autofillTestReviewGateProblems(t, record.Directory)) == 0 {
			t.Fatalf("missing review not reported as %q for a required:false task", autofillTestReviewGate)
		}
	})
}

// C5: with required:false a checkpoint attached anyway is validated as today.
func TestNotRequiredCheckpointsStillValidateAttachments(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*checkpointDraft){
		"pending_before":    pendingDecision,
		"unresolved_before": unresolvedDecision,
	} {
		t.Run(name, func(t *testing.T) {
			record, ref := fixtureTask(t, checkpointsTestOptional(t))
			before, ok := record.Contexts["before"]
			if !ok {
				t.Fatal("required:false task has no before context to attach to")
			}
			attachCheckpoint(t, record, "before", before, mutate)
			mustAssess(t, record, ref)
			if verified, _ := Verify(t.Context(), record.Directory); verified.Complete || len(verified.Problems) == 0 {
				t.Fatalf("%s attachment did not block a required:false task: %+v", name, verified)
			}
		})
	}
	for name, mutate := range map[string]func(*checkpointDraft){
		"pending_final":    pendingDecision,
		"unresolved_final": unresolvedDecision,
		"resolved_final":   nil,
	} {
		t.Run(name, func(t *testing.T) {
			record, ref := fixtureTask(t, checkpointsTestOptional(t))
			attachCheckpoint(t, record, "before", record.Contexts["before"], nil)
			mustAssess(t, record, ref)
			final, err := PrepareContext(t.Context(), record.Directory, "final")
			if err != nil {
				t.Fatal(err)
			}
			attachCheckpoint(t, record, "final", final, mutate)
			verified, err := Verify(t.Context(), record.Directory)
			if mutate == nil {
				if err != nil || !verified.Complete {
					t.Fatalf("resolved attached checkpoints blocked a required:false task: %+v %v", verified, err)
				}
				return
			}
			if verified.Complete || len(verified.Problems) == 0 {
				t.Fatalf("%s attachment did not block a required:false task: %+v", name, verified)
			}
		})
	}
	t.Run("invalid_record_refused", func(t *testing.T) {
		record, _ := fixtureTask(t, checkpointsTestOptional(t))
		bound := record.Contexts["before"]
		forged, verr := writeUncheckedConsultation(t, bound, func(draft *checkpointDraft) {
			draft.Forge = func(_ *testing.T, object *fixtureObject) { object.set("status", "success") }
		})
		if verr == nil {
			t.Fatal("fixture invariant: forged record passed consult.ValidateRecord")
		}
		crossTask := bound
		crossTask.TaskID = "different"
		for label, path := range map[string]string{"forged": forged, "cross_task": writeConsultation(t, crossTask, nil)} {
			before := productionTaskBytes(t, record.Directory)
			if _, err := AttachConsultation(t.Context(), record.Directory, "before", path); err == nil {
				t.Fatalf("%s before record attached to a required:false task", label)
			}
			if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s refused attachment changed task files", label)
			}
		}
	})
}

// C7: a checkpoints policy needs no record version of its own: records carry
// the current Version; a plan without a policy stores no checkpoints member,
// loads with a nil policy and verifies as before; a plan with a policy
// round-trips through the stored record.
func TestCheckpointPolicyRecordsKeepTheCurrentVersion(t *testing.T) {
	t.Run("omitted", func(t *testing.T) {
		record, ref := fixtureTask(t, substantialPlan(t))
		fields, plan := checkpointsTestRecordFields(t, record.Directory, "000001")
		if string(fields["version"]) != strconv.Itoa(Version) {
			t.Fatalf("stored version = %s", fields["version"])
		}
		if _, ok := plan["checkpoints"]; ok {
			t.Fatalf("plan without a policy stored a checkpoints member: %s", plan["checkpoints"])
		}
		loaded, err := Load(record.Directory)
		if err != nil || loaded.Version != Version || loaded.Plan.Checkpoints != nil {
			t.Fatalf("load without checkpoints: version=%d checkpoints=%+v %v", loaded.Version, loaded.Plan.Checkpoints, err)
		}
		mustAssess(t, record, ref)
		if verified, _ := Verify(t.Context(), record.Directory); verified.Complete {
			t.Fatal("record without checkpoints completed without adviser checkpoints")
		}
	})
	t.Run("not_required", func(t *testing.T) {
		record, _ := fixtureTask(t, checkpointsTestOptional(t))
		fields, plan := checkpointsTestRecordFields(t, record.Directory, "000001")
		if string(fields["version"]) != strconv.Itoa(Version) {
			t.Fatalf("stored version = %s", fields["version"])
		}
		var stored CheckpointPolicy
		if err := json.Unmarshal(plan["checkpoints"], &stored); err != nil || stored.Required || stored.Reason != checkpointsTestReason {
			t.Fatalf("stored checkpoints = %s (%+v) %v", plan["checkpoints"], stored, err)
		}
		loaded, err := Load(record.Directory)
		if err != nil || loaded.Version != Version || loaded.Plan.Checkpoints == nil || loaded.Plan.Checkpoints.Required || loaded.Plan.Checkpoints.Reason != checkpointsTestReason {
			t.Fatalf("load with checkpoints: %+v %v", loaded.Plan.Checkpoints, err)
		}
	})
}
