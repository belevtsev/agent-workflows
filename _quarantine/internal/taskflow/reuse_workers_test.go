package taskflow

import (
	"slices"
	"strings"
	"testing"
	"time"
)

// TestEvidenceWithoutRunsStaysTiedToItsSnapshot assesses a requirement by
// non-execution reasoning, then moves the snapshot over the same bytes, as a
// branch switch does. Oracle: verify is complete before and reports the
// requirement and the decision stale after: only evidence that binds a still
// credited run survives a snapshot change.
func TestEvidenceWithoutRunsStaysTiedToItsSnapshot(t *testing.T) {
	plan := Plan{Title: "Read-only source check", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "Read actual source", AllowNonExecution: true}}}
	record, ref, source, env := fakeSourceTask(t, plan, time.Date(2031, time.February, 3, 4, 5, 6, 0, time.UTC))
	input := assessment(ref)
	input.Requirements[0].NonExecutionReason = "Read-only change; inspected the complete referenced file."
	input.Decision.NonExecutionReason = input.Requirements[0].NonExecutionReason
	if _, err := env.Assess(t.Context(), record.Directory, input); err != nil {
		t.Fatal(err)
	}
	if result, err := env.Verify(t.Context(), record.Directory); err != nil || !result.Complete {
		t.Fatalf("verify at the assessed snapshot: %+v %v", result, err)
	}
	source.snapshot.Branch, source.snapshot.Fingerprint = "different", "sha256:"+strings.Repeat("b", 64)
	result, _ := env.Verify(t.Context(), record.Directory)
	for _, want := range []string{"requirement behavior evidence is stale", "coordinator decision is stale"} {
		if !slices.Contains(result.Problems, want) {
			t.Errorf("after the snapshot moved: problems %q lack %q", result.Problems, want)
		}
	}
}

// TestWorkerBindsAReusedRunAfterStaging runs the worker's check, stages the
// change (same bytes, another snapshot) and submits. Oracle: cw task run
// reuses the pass; the completed result and the coordinator's acceptance
// bind that run; the stored record loads again (its bindings validate) and
// records the acceptance's source digest.
func TestWorkerBindsAReusedRunAfterStaging(t *testing.T) {
	t.Parallel()
	record := committedTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
	ref := changedWorkerSource(t, record)
	first, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	reuseGit(t, record.Baseline.Root, "add", "-A")
	if run, reused, err := productionEnv.RunOrReuseCheck(t.Context(), record.Directory, "focused"); err != nil || reused == "" || run.ID != first.ID {
		t.Fatalf("cw task run after staging: run %s, reused %q, %v; want run %s reused", run.ID, reused, err, first.ID)
	}
	result := workerResult(t, record, "implement", "completed", ref)
	result.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	record = mustSubmitWorker(t, record, result)
	if bound := findWorkerResult(record, "implement"); bound == nil || len(bound.CheckRuns) != 1 || bound.CheckRuns[0] != first.ID {
		t.Fatalf("result binds %+v, want run %s", bound, first.ID)
	}
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "implement", "accepted", ref))
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("the record binding a reused run does not load: %v", err)
	}
	accepted := loaded.WorkerAcceptances[len(loaded.WorkerAcceptances)-1]
	if len(accepted.CheckRuns) != 1 || accepted.CheckRuns[0] != first.ID || accepted.SourceSHA256 != first.SourceSHA256 {
		t.Fatalf("acceptance %+v, want run %s at source %s", accepted, first.ID, first.SourceSHA256)
	}
}
