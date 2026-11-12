package cli

import (
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

// Every Graphify unavailable report exits 4, whatever its code, with the
// report on stdout and no error line: a Graphify refusal (code repository)
// and an error of cw's own (code filesystem, here a home directory that
// cannot be resolved for "~/repo") alike.
func TestGraphifyUnavailableReportExitsRefusedForEveryCode(t *testing.T) {
	for _, test := range []struct {
		name, repo, code string
		home             func() (string, error)
	}{
		{"graphify_refusal", filepath.Join(t.TempDir(), "missing"), "repository", nil},
		{"filesystem_error", "~/repo", "filesystem", func() (string, error) { return "", errors.New("home directory unavailable") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			config, out := testConfig(t)
			if test.home != nil {
				config.HomeDir = test.home
			}
			err := Execute(t.Context(), []string{"graphify", "build", "--repo", test.repo}, config)
			if code := ExitCode(err); code != 4 || err.Error() != "" {
				t.Fatalf("graphify build --repo %s: exit %d, error %v; want 4 (refused) and no error line", test.repo, code, err)
			}
			var report graphifyFailure
			if decodeErr := json.Unmarshal(out.Bytes(), &report); decodeErr != nil || report.Status != "unavailable" || report.Code != test.code {
				t.Fatalf("report = %+v (%v), want status unavailable with code %s: %s", report, decodeErr, test.code, out)
			}
		})
	}
}

// rewriteStartedRecord starts a task through the CLI and rewrites the members
// of its one revision, which chains to nothing, so only the change is tested.
func rewriteStartedRecord(t *testing.T, change func(members map[string]jsontext.Value)) *summaryCLIHarness {
	t.Helper()
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	if _, err := os.Stat(filepath.Join(h.task, "record-000002.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a started task holds more than one revision: %v", err)
	}
	path := filepath.Join(h.task, "record-000001.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	members := map[string]jsontext.Value{}
	if err := json.Unmarshal(data, &members); err != nil {
		t.Fatal(err)
	}
	change(members)
	if data, err = json.Marshal(members, json.Deterministic(true)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return h
}

// requireTaskCommandsRefused requires task show and task verify to exit 4
// with message in the error and nothing on stdout.
func requireTaskCommandsRefused(t *testing.T, h *summaryCLIHarness, message string) {
	t.Helper()
	for _, command := range []string{"show", "verify"} {
		got := compactExecute(h, nil, "task", command, "--task", h.task)
		if code := ExitCode(got.err); code != 4 || got.stdout != "" || got.err == nil || !strings.Contains(got.err.Error(), message) {
			t.Errorf("task %s: exit %d, stdout %q, error %v; want 4 (refused), no output and an error containing %q", command, code, got.stdout, got.err, message)
		}
	}
}

// A task record with a revision newer than this cw reads is refused, not
// failed: the task commands exit 4 and say to upgrade cw.
func TestTaskCommandsExitRefusedForARecordFromANewerCw(t *testing.T) {
	h := rewriteStartedRecord(t, func(members map[string]jsontext.Value) { members["version"] = jsontext.Value("5") })
	requireTaskCommandsRefused(t, h, "task record version 5 was written by a newer cw; this cw reads versions 3 to 4; upgrade cw to read it")
}

// A task record without packs or test_rules, which cw v2.0.66 to v2.0.96
// wrote, is refused, not failed: the task commands exit 4 and say to start a
// new task.
func TestTaskCommandsExitRefusedForALegacyRecord(t *testing.T) {
	h := rewriteStartedRecord(t, func(members map[string]jsontext.Value) {
		delete(members, "packs")
		delete(members, "test_rules")
	})
	requireTaskCommandsRefused(t, h, "task record has no packs or test_rules, so cw v2.0.66 to v2.0.96 or a v3 build before v3.0.0-rc.1 wrote it; this cw cannot read it: move the task directory out of the task store, then start a new task with 'cw task start'")
}
