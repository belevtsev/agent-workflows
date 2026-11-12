package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/publish"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle for this file: the exit table in docs/operations.md#exit-statuses.
// 0 ok, 1 failure, 2 usage, 3 denied, 4 refused, 5 negative result,
// 6 publish outcome unknown, 7 publish mismatch, 130 cancelled. The numbers
// are literal, so a change to the table must change this file too.

// An agent tells "the task is not complete yet" from "cw could not verify"
// by the exit status alone: an incomplete verification exits 5 and prints the
// verification; an I/O error exits 1 and prints nothing.
func TestTaskVerifyExitStatusTellsIncompleteFromIOError(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory

	incomplete := compactExecute(h, nil, "task", "verify", "--task", h.task)
	if code := ExitCode(incomplete.err); code != 5 {
		t.Fatalf("verify of an unassessed task: exit %d, want 5 (negative result): %s", code, incomplete.text())
	}
	var gate taskflow.Verification
	if err := json.Unmarshal([]byte(incomplete.stdout), &gate); err != nil || gate.Complete || len(gate.Problems) == 0 {
		t.Fatalf("verify of an unassessed task printed no incomplete verification: %v: %s", err, incomplete.text())
	}

	// The task directory cannot be read: the record is gone.
	missing := compactExecute(h, nil, "task", "verify", "--task", filepath.Join(filepath.Dir(h.task), "absent-task"))
	if code := ExitCode(missing.err); code != 1 || missing.stdout != "" {
		t.Fatalf("verify of a missing task directory: exit %d, stdout %q; want 1 (failure) and no output: %s", code, missing.stdout, missing.text())
	}

	// The selected source cannot be read: the repository directory is gone.
	repo, err := h.config.WorkingDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(repo); err != nil {
		t.Fatal(err)
	}
	unreadable := compactExecute(h, nil, "task", "verify", "--task", h.task)
	if code := ExitCode(unreadable.err); code != 1 || unreadable.stdout != "" {
		t.Fatalf("verify with the source directory removed: exit %d, stdout %q; want 1 (failure) and no output: %s", code, unreadable.stdout, unreadable.text())
	}
}

// A planned check that runs and fails is an answer, exit 5, with the run
// printed; a check ID outside the plan is invalid input, exit 1, with nothing
// printed and nothing run.
func TestTaskRunExitStatusTellsFailedCheckFromInvalidCheck(t *testing.T) {
	h := compactCheckHarness(t, nil, []string{"cat", "exit-status-missing-input"})

	failed := compactExecute(h, nil, "task", "run", "--task", h.task, "--check", "probe")
	if code := ExitCode(failed.err); code != 5 {
		t.Fatalf("failed check run: exit %d, want 5 (negative result): %s", code, failed.text())
	}
	if stored := lastCheckRunForTest(t, h.load(), "probe"); stored.Status != "failed" || stored.ExitCode == nil || *stored.ExitCode == 0 {
		t.Fatalf("fixture check did not fail: %+v", stored)
	}
	var printed taskflow.CheckRun
	if err := json.Unmarshal([]byte(failed.stdout), &printed); err != nil || printed.Status != "failed" {
		t.Fatalf("failed check run printed no failed run: %v: %s", err, failed.text())
	}

	unplanned := compactExecute(h, nil, "task", "run", "--task", h.task, "--check", "absent")
	if code := ExitCode(unplanned.err); code != 1 || unplanned.stdout != "" {
		t.Fatalf("run of a check outside the plan: exit %d, stdout %q; want 1 (failure) and no output: %s", code, unplanned.stdout, unplanned.text())
	}
}

// cw publish keeps a distinct status for each outcome, on the shared table:
// an unknown step outcome and a mismatch sit above the negative result.
func TestPublishStatusesExitOnTheSharedTable(t *testing.T) {
	want := map[string]int{
		publish.StatusPublished: 0, publish.StatusFailed: 1, publish.StatusDenied: 3, publish.StatusRefused: 4,
		publish.StatusUnknown: 6, publish.StatusMismatch: 7, publish.StatusCancelled: 130,
	}
	for status, code := range want {
		if got := (publish.Report{Status: status}).ExitCode(); got != code {
			t.Errorf("publish status %q exits %d, want %d", status, got, code)
		}
	}
}
