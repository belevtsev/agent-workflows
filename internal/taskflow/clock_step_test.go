package taskflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// clockAt is the Env of a test whose clock reads at.
func clockAt(at time.Time) Env {
	return Env{Clock: fixedClock(at), Consultation: consult.ValidateRecord}
}

// behindTask is the Env of a clock that stepped back to an hour before
// record started, so every stamp it gives record is record's updated_at.
func behindTask(record Record) Env { return clockAt(record.CreatedAt.Add(-time.Hour)) }

// clockStepTask starts a substantial task without checkpoints at noon, in a
// committed repository whose source.txt holds two lines. Its check "focused"
// passes.
func clockStepTask(t *testing.T) (record Record, store string, noon time.Time) {
	t.Helper()
	noon = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reuseGit(t, repo, "init", "-q")
	reuseGit(t, repo, "add", "-A")
	reuseGit(t, repo, "commit", "-qm", "base")
	plan := helperPlan(t, "substantial", "pass")
	plan.Checkpoints = &CheckpointPolicy{Required: false, Reason: "the clock tests need no checkpoint"}
	store = filepath.Join(t.TempDir(), "tasks")
	record, err := clockAt(noon).StartWithPacks(t.Context(), repo, store, plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	return record, store, noon
}

// A wall clock that steps back (NTP, a VM resume, a manual change) must not
// lock a task: after a worker is assigned at the current time, the clock
// moves an hour back, before the task even started, and the worker's handoff
// and the coordinator's acceptance are written under that clock.
// Oracle: both writes succeed; every stamp is the later of the clock and the
// previous revision's updated_at, which here is that updated_at: submitted_at,
// accepted_at and both revisions' updated_at equal the assignment revision's
// updated_at; the stored record loads (its time rules validate).
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
	if bound := findWorkerResult(submitted, "implement"); !bound.SubmittedAt.Equal(record.UpdatedAt) || !submitted.UpdatedAt.Equal(record.UpdatedAt) || bound.SubmittedAt.Before(assigned) {
		t.Fatalf("submitted_at %s, updated_at %s; want both the previous updated_at %s, at or after assigned_at %s", bound.SubmittedAt, submitted.UpdatedAt, record.UpdatedAt, assigned)
	}
	accepted, err := behind.AcceptWorkerResult(t.Context(), record.Directory, workerAcceptance(t, submitted, "implement", "accepted", ref))
	if err != nil {
		t.Fatalf("acceptance after the clock stepped back: %v", err)
	}
	last := accepted.WorkerAcceptances[len(accepted.WorkerAcceptances)-1]
	if !last.AcceptedAt.Equal(submitted.UpdatedAt) || !accepted.UpdatedAt.Equal(submitted.UpdatedAt) {
		t.Fatalf("accepted_at %s, updated_at %s; want both the handoff revision's updated_at %s", last.AcceptedAt, accepted.UpdatedAt, submitted.UpdatedAt)
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
// Oracle: the run is recorded and passes; its started_at is the later of the
// clock and the previous revision's updated_at, which is that updated_at; its
// finished_at, a clock reading before the start, is held at started_at; the
// terminal revision's updated_at is the same time; and the record loads.
func TestCheckRunsStayOrderedAfterTheClockStepsBack(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
	behind := Env{Clock: fixedClock(record.CreatedAt.Add(-time.Hour)), Consultation: consult.ValidateRecord}
	run, err := behind.RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatalf("check after the clock stepped back: %v", err)
	}
	if !run.StartedAt.Equal(record.UpdatedAt) || run.FinishedAt == nil || !run.FinishedAt.Equal(run.StartedAt) {
		t.Fatalf("started_at %s, finished_at %v; want both the previous updated_at %s", run.StartedAt, run.FinishedAt, record.UpdatedAt)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("the record does not load: %v", err)
	}
	if !loaded.UpdatedAt.Equal(record.UpdatedAt) {
		t.Fatalf("updated_at %s after the run; want the previous updated_at %s", loaded.UpdatedAt, record.UpdatedAt)
	}
}

// Every other time a revision adds, written under a clock that stepped back
// to before the task started.
// Oracle: each stamp is the later of the clock and the previous revision's
// updated_at, so it equals that updated_at, and so does the new revision's
// updated_at; the record loads. A stamp taken from the clock alone would lie
// before created_at.
func TestStampsAfterTheClockStepsBackEqualThePreviousRevision(t *testing.T) {
	// overStop stores 13 USD of usage at the current time: past lite's stop
	// (12 of 15 USD), so new work needs the owner's cost-budget words.
	overStop := func(t *testing.T, record Record) Record {
		t.Helper()
		result, err := productionEnv.RecordUsage(t.Context(), record.Directory, budgetSample("s1", 13))
		if err != nil || !result.Stored {
			t.Fatalf("precondition: usage stored %v, %v", result.Stored, err)
		}
		return result.Record
	}
	expect := func(t *testing.T, previous Record, name string, got time.Time) {
		t.Helper()
		loaded, err := Load(previous.Directory)
		if err != nil {
			t.Fatalf("the record does not load: %v", err)
		}
		if loaded.Revision <= previous.Revision {
			t.Fatalf("revision %d; want one after %d", loaded.Revision, previous.Revision)
		}
		if !got.Equal(previous.UpdatedAt) || !loaded.UpdatedAt.Equal(previous.UpdatedAt) {
			t.Fatalf("%s %s, updated_at %s; want both the previous updated_at %s", name, got, loaded.UpdatedAt, previous.UpdatedAt)
		}
	}
	t.Run("assignment and its cost approval", func(t *testing.T) {
		record := overStop(t, committedTask(t, helperPlan(t, "small", "pass")))
		assigned, err := behindTask(record).AssignApprovedWorker(t.Context(), record.Directory, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}), []string{"go on"})
		if err != nil {
			t.Fatalf("assign after the clock stepped back: %v", err)
		}
		if len(assigned.OwnerApprovals) != 1 {
			t.Fatalf("owner approvals %+v; want the one cost-budget approval", assigned.OwnerApprovals)
		}
		expect(t, record, "assigned_at", findAssignment(assigned, "implement").AssignedAt)
		expect(t, record, "the approval's at", assigned.OwnerApprovals[0].At)
	})
	t.Run("cost approval of a check", func(t *testing.T) {
		record := overStop(t, committedTask(t, helperPlan(t, "small", "pass")))
		if _, _, err := behindTask(record).RunApprovedCheck(t.Context(), record.Directory, "focused", false, []string{"go on"}); err != nil {
			t.Fatalf("approved check after the clock stepped back: %v", err)
		}
		loaded, err := Load(record.Directory)
		if err != nil || len(loaded.OwnerApprovals) != 1 {
			t.Fatalf("owner approvals %+v, %v; want the one cost-budget approval", loaded.OwnerApprovals, err)
		}
		expect(t, record, "the approval's at", loaded.OwnerApprovals[0].At)
	})
	t.Run("usage sample", func(t *testing.T) {
		record := committedTask(t, helperPlan(t, "small", "pass"))
		result, err := behindTask(record).RecordUsage(t.Context(), record.Directory, budgetSample("s1", 1))
		if err != nil || !result.Stored || len(result.Record.Usage) != 1 {
			t.Fatalf("usage after the clock stepped back: stored %v, %v", result.Stored, err)
		}
		expect(t, record, "the returned sample's at", result.Sample.At)
		expect(t, record, "the stored sample's at", result.Record.Usage[0].At)
	})
	t.Run("abandonment", func(t *testing.T) {
		record := committedTask(t, helperPlan(t, "small", "pass"))
		abandoned, err := behindTask(record).Abandon(record.Directory, "given up")
		if err != nil || abandoned.Abandonment == nil {
			t.Fatalf("abandon after the clock stepped back: %v", err)
		}
		expect(t, record, "the abandonment's at", abandoned.Abandonment.At)
	})
	t.Run("refused check", func(t *testing.T) {
		// The go check is refused before it runs (its go env cannot be read),
		// so one revision holds both its start and its finish.
		plan := helperPlan(t, "small", "pass")
		plan.Checks = append(plan.Checks, Check{ID: "unit", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 10})
		plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, "unit")
		record := committedTask(t, plan)
		program := filepath.Join(t.TempDir(), "go")
		if err := os.WriteFile(program, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		resolveGo = func(context.Context, string, string, []string) (langpack.GoTool, error) {
			return langpack.GoTool{Path: program, Version: "go1.27.4", GOROOT: filepath.Dir(program)}, nil
		}
		readGoEnv = func(context.Context, langpack.GoTool, string, []string) (langpack.GoEnv, []string, map[string]string, error) {
			return langpack.GoEnv{}, nil, nil, errors.New("cannot read the configuration of the go command: test reader")
		}
		t.Cleanup(func() { resolveGo, readGoEnv = langpack.ResolveGo, langpack.ReadGoEnv })
		run, err := behindTask(record).RunCheck(t.Context(), record.Directory, "unit")
		if run.Failure != "untrusted_toolchain" || !errors.Is(err, ErrCheckFailed) || run.FinishedAt == nil {
			t.Fatalf("RunCheck = %s/%s, %v; want a finished run refused as untrusted_toolchain", run.Status, run.Failure, err)
		}
		expect(t, record, "started_at", run.StartedAt)
		expect(t, record, "finished_at", *run.FinishedAt)
	})
}

// A verified task and its successor after the clock stepped back: task A has
// its last revision at 12:30, the clock steps back, A is verified at 12:21,
// task B starts at 12:22 from the bytes A verified, and work under B changes
// the source. The gate runs at 12:40, when the clock has passed every stored
// time.
// Oracle: checked_at is the clock's reading (12:21), not a monotonic stamp,
// because the gate compares it with B's created_at (12:22), which is the
// clock too; the verify revision's updated_at is still 12:30. B was created
// after A was verified, from A's verified bytes, so A is finished: FindOpen
// returns B alone, gating. With checked_at stamped 12:30, A would gate with
// "its source changed after the last passing cw task verify".
func TestVerifiedTaskIsFinishedByASuccessorStartedAfterTheClockSteppedBack(t *testing.T) {
	a, store, noon := clockStepTask(t)
	repo := a.Baseline.Root
	data, err := os.ReadFile(filepath.Join(repo, "source.txt"))
	if err != nil {
		t.Fatal(err)
	}
	ref := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(data)}
	at1230 := clockAt(noon.Add(30 * time.Minute))
	if _, err := at1230.RunCheck(t.Context(), a.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	if _, err := at1230.Assess(t.Context(), a.Directory, assessment(ref, "focused")); err != nil {
		t.Fatal(err)
	}
	verifiedAt := noon.Add(21 * time.Minute)
	verified, err := clockAt(verifiedAt).Verify(t.Context(), a.Directory)
	if err != nil || !verified.Complete {
		t.Fatalf("verify A: %+v, %v", verified, err)
	}
	loaded, err := Load(a.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if !verified.CheckedAt.Equal(verifiedAt) || !loaded.Verification.CheckedAt.Equal(verifiedAt) || !loaded.UpdatedAt.Equal(noon.Add(30*time.Minute)) {
		t.Fatalf("checked_at %s (stored %s), updated_at %s; want checked_at the clock's 12:21 and updated_at the previous 12:30", verified.CheckedAt, loaded.Verification.CheckedAt, loaded.UpdatedAt)
	}
	b, err := clockAt(noon.Add(22*time.Minute)).StartWithPacks(t.Context(), repo, store, loaded.Plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("first line\nsecond line\nthird line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	open, err := FindOpen(t.Context(), store, foAtRoot(repo), noon.Add(40*time.Minute), SourceDigest)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].Record.ID != b.ID || !open[0].Gates() {
		t.Fatalf("FindOpen = %s; want only the successor %s, gating", foDescribe(open), b.ID)
	}
}

// The timeline of check runs made while the clock is behind the newest
// revision: one run at 13:00, then three runs under a clock that reads
// 12:10:30 and moves 30 seconds on every reading.
// Oracle: the three later runs are stamped at the previous updated_at, 13:00,
// so all four entries have the same start; entries with equal starts are
// listed in the record's order, the order the runs were made in, not by run
// ID. Each later run shows that start and duration 0s, as the stored times
// say.
func TestTimelineListsRunsInTheOrderTheyRanWhileTheClockIsBehind(t *testing.T) {
	record, _, noon := clockStepTask(t)
	first, err := clockAt(noon.Add(time.Hour)).RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	ran := []string{first.ID}
	tick := noon.Add(10 * time.Minute)
	ticking := Env{Clock: func() time.Time { tick = tick.Add(30 * time.Second); return tick }, Consultation: consult.ValidateRecord}
	for range 3 {
		run, err := ticking.RunCheck(t.Context(), record.Directory, "focused")
		if err != nil {
			t.Fatal(err)
		}
		ran = append(ran, run.ID)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	entries := BuildTimeline(loaded).Entries
	var listed []string
	for _, entry := range entries {
		listed = append(listed, entry.ID)
	}
	if !slices.Equal(listed, ran) {
		t.Fatalf("timeline lists %v; want the order the runs were made in, %v", listed, ran)
	}
	start := timelineInstant(noon.Add(time.Hour))
	for _, entry := range entries[1:] {
		if entry.StartedAt != start || entry.Duration != "0s" {
			t.Errorf("run %s: start %s, duration %s; want %s and 0s", entry.ID, entry.StartedAt, entry.Duration, start)
		}
	}
}

// A check whose finish time is one wrong reading a day ahead: the clock reads
// a minute after the task's last revision, except for the first reading after
// the run's start revision is stored, which is the finish reading, taken
// outside the task lock.
// Oracle: finished_at stores that reading. No record rule orders finished_at
// against updated_at, so the terminal revision's updated_at is the clock's
// reading at its own stamp, and a write a minute later is stamped by the
// clock. Were updated_at raised to finished_at, every later stamp would be
// held a day ahead.
func TestCheckFinishReadAheadDoesNotHoldLaterStamps(t *testing.T) {
	record := committedTask(t, helperPlan(t, "small", "pass"))
	at := record.UpdatedAt.Add(time.Minute)
	ahead := at.Add(24 * time.Hour)
	stored := func() int {
		names, err := filepath.Glob(filepath.Join(record.Directory, "record-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		return len(names)
	}
	started, read := stored()+1, false
	clock := func() time.Time {
		if !read && stored() == started {
			read = true
			return ahead
		}
		return at
	}
	run, err := Env{Clock: clock, Consultation: consult.ValidateRecord}.RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	if run.FinishedAt == nil || !run.FinishedAt.Equal(ahead) || !run.StartedAt.Equal(at) {
		t.Fatalf("precondition: started_at %s, finished_at %v; want %s and the reading ahead, %s", run.StartedAt, run.FinishedAt, at, ahead)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("the record does not load: %v", err)
	}
	if !loaded.UpdatedAt.Equal(at) {
		t.Fatalf("updated_at %s after the run; want the clock's %s", loaded.UpdatedAt, at)
	}
	later := at.Add(time.Minute)
	abandoned, err := clockAt(later).Abandon(record.Directory, "given up")
	if err != nil {
		t.Fatal(err)
	}
	if !abandoned.Abandonment.At.Equal(later) || !abandoned.UpdatedAt.Equal(later) {
		t.Fatalf("abandonment at %s, updated_at %s; want both the clock's %s", abandoned.Abandonment.At, abandoned.UpdatedAt, later)
	}
}
