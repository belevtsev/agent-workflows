package taskflow

import (
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
)

// A wall clock that steps back (NTP, a VM resume, a manual change) must not
// lock a task: after a worker is assigned at the current time, the clock
// moves an hour back, before the task even started, and the worker's handoff
// and the coordinator's acceptance are written under that clock.
// Oracle: both writes succeed; every stamp is the later of the clock and the
// previous revision's updated_at, so the record's updated_at never decreases
// and submitted_at >= assigned_at, accepted_at >= submitted_at; the stored
// record loads (its time rules validate).
func TestRevisionsStayOrderedAfterTheClockStepsBack(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}))
	assigned := findAssignment(record, "implement").AssignedAt
	behind := Env{Clock: fixedClock(record.CreatedAt.Add(-time.Hour)), Consultation: consult.ValidateRecord}
	ref := changedWorkerSource(t, record)
	result := workerResult(t, record, "implement", "completed", ref)
	result.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	submitted, err := behind.SubmitWorkerResult(t.Context(), record.Directory, result)
	if err != nil {
		t.Fatalf("handoff after the clock stepped back: %v", err)
	}
	if bound := findWorkerResult(submitted, "implement"); bound.SubmittedAt.Before(assigned) || submitted.UpdatedAt.Before(record.UpdatedAt) {
		t.Fatalf("submitted_at %s, updated_at %s; want both at or after assigned_at %s and the previous updated_at %s", bound.SubmittedAt, submitted.UpdatedAt, assigned, record.UpdatedAt)
	}
	accepted, err := behind.AcceptWorkerResult(t.Context(), record.Directory, workerAcceptance(t, submitted, "implement", "accepted", ref))
	if err != nil {
		t.Fatalf("acceptance after the clock stepped back: %v", err)
	}
	last := accepted.WorkerAcceptances[len(accepted.WorkerAcceptances)-1]
	if last.AcceptedAt.Before(findWorkerResult(submitted, "implement").SubmittedAt) || accepted.UpdatedAt.Before(submitted.UpdatedAt) {
		t.Fatalf("accepted_at %s, updated_at %s; want both at or after the handoff's revision %s", last.AcceptedAt, accepted.UpdatedAt, submitted.UpdatedAt)
	}
	if _, err := Load(record.Directory); err != nil {
		t.Fatalf("the record does not load: %v", err)
	}
}

// When the clock is ahead of the previous revision, a stamp is the clock's
// own time: monotonic stamping adds nothing then.
// Oracle: the revision written an hour after the last one has updated_at
// equal to the clock, and the handoff's submitted_at equals it too.
func TestRevisionsTakeTheClockWhenItMovesForward(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}))
	later := record.UpdatedAt.Add(time.Hour)
	ahead := Env{Clock: fixedClock(later), Consultation: consult.ValidateRecord}
	ref := changedWorkerSource(t, record)
	result := workerResult(t, record, "implement", "completed", ref)
	result.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	submitted, err := ahead.SubmitWorkerResult(t.Context(), record.Directory, result)
	if err != nil {
		t.Fatal(err)
	}
	if bound := findWorkerResult(submitted, "implement"); !submitted.UpdatedAt.Equal(later) || !bound.SubmittedAt.Equal(later) {
		t.Fatalf("updated_at %s, submitted_at %s; want both %s", submitted.UpdatedAt, bound.SubmittedAt, later)
	}
}

// A check run under a clock that stepped back, while a worker assignment
// stamped at the current time is open.
// Oracle: the run is recorded and passes; its started_at and finished_at are
// no earlier than the previous revision's updated_at, finished_at is no
// earlier than started_at, and the record loads.
func TestCheckRunsStayOrderedAfterTheClockStepsBack(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
	behind := Env{Clock: fixedClock(record.CreatedAt.Add(-time.Hour)), Consultation: consult.ValidateRecord}
	run, err := behind.RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatalf("check after the clock stepped back: %v", err)
	}
	if run.StartedAt.Before(record.UpdatedAt) || run.FinishedAt == nil || run.FinishedAt.Before(run.StartedAt) {
		t.Fatalf("started_at %s, finished_at %v; want both at or after %s, in order", run.StartedAt, run.FinishedAt, record.UpdatedAt)
	}
	if _, err := Load(record.Directory); err != nil {
		t.Fatalf("the record does not load: %v", err)
	}
}
