package cli

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests for `cw task abandon` (contract gate-tests-v1, requirements
// R-ABANDON-CLI C1-C3, and A2/A4 through the command), driven through Execute
// with the hook test harness.

// taRun runs cw with args and returns the output and error.
func (h *hkHarness) taRun(t *testing.T, args ...string) (string, error) {
	t.Helper()
	h.out.Reset()
	err := Execute(t.Context(), args, h.config)
	return h.out.String(), err
}

// taLoad loads the record at dir and requires success.
func taLoad(t *testing.T, dir string) taskflow.Record {
	t.Helper()
	record, err := taskflow.Load(dir)
	if err != nil {
		t.Fatalf("taskflow.Load(%s): %v", dir, err)
	}
	return record
}

// taExpectUnchanged requires that the record at want.Directory still has
// want's revision and abandonment state.
func taExpectUnchanged(t *testing.T, want taskflow.Record) {
	t.Helper()
	loaded := taLoad(t, want.Directory)
	if loaded.Revision != want.Revision {
		t.Fatalf("record revision %d, want %d (nothing written)", loaded.Revision, want.Revision)
	}
	if (loaded.Abandonment == nil) != (want.Abandonment == nil) {
		t.Fatalf("record abandonment %+v, want %+v", loaded.Abandonment, want.Abandonment)
	}
	if want.Abandonment != nil && loaded.Abandonment.Reason != want.Abandonment.Reason {
		t.Fatalf("record abandonment reason %q, want %q", loaded.Abandonment.Reason, want.Abandonment.Reason)
	}
}

// taOneLine requires exactly one newline-terminated line.
func taOneLine(t *testing.T, got string) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") || strings.Count(got, "\n") != 1 {
		t.Fatalf("output %q, want exactly one newline-terminated line", got)
	}
}

func TestTaskAbandonCommand(t *testing.T) {
	t.Run("prints the full record", func(t *testing.T) {
		// C1: the full record JSON on one line, with abandonment.reason TEXT;
		// the stored record agrees. Oracle for the time: Config.Now is the
		// clock of both commands, so created_at, updated_at and
		// abandonment.at are its time exactly, in UTC.
		repo := hkRepo(t)
		h := newHKHarness(t)
		now := time.Date(2031, time.February, 3, 4, 5, 6, 7, time.FixedZone("UTC+3", 3*60*60))
		h.config.Now = func() time.Time { return now }
		record := h.start(t, repo, hkSubstantialPlan)
		got, err := h.taRun(t, "task", "abandon", "--task", record.Directory, "--reason", "superseded by a new plan")
		if err != nil {
			t.Fatalf("task abandon: %v", err)
		}
		taOneLine(t, got)
		var printed taskflow.Record
		if err := json.Unmarshal([]byte(got), &printed); err != nil {
			t.Fatalf("output %q is not a record: %v", got, err)
		}
		if printed.ID != record.ID || printed.Revision != record.Revision+1 {
			t.Fatalf("printed record %s revision %d, want %s revision %d", printed.ID, printed.Revision, record.ID, record.Revision+1)
		}
		if printed.Abandonment == nil || printed.Abandonment.Reason != "superseded by a new plan" {
			t.Fatalf("printed abandonment %+v, want reason %q", printed.Abandonment, "superseded by a new plan")
		}
		for name, at := range map[string]time.Time{"created_at": printed.CreatedAt, "updated_at": printed.UpdatedAt, "abandonment.at": printed.Abandonment.At} {
			if !at.Equal(now) || at.Location() != time.UTC {
				t.Errorf("printed %s %v, want %v", name, at, now.UTC())
			}
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(got), &raw); err != nil {
			t.Fatal(err)
		}
		abandonment, ok := raw["abandonment"].(map[string]any)
		if !ok || abandonment["reason"] != "superseded by a new plan" {
			t.Fatalf("output member abandonment %v, want reason %q", raw["abandonment"], "superseded by a new plan")
		}
		taExpectUnchanged(t, printed)
	})

	t.Run("summary", func(t *testing.T) {
		// C1: --summary prints one JSON line with task, task_id and status
		// "abandoned".
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		got, err := h.taRun(t, "task", "abandon", "--task", record.Directory, "--reason", "no longer needed", "--summary")
		if err != nil {
			t.Fatalf("task abandon --summary: %v", err)
		}
		taOneLine(t, got)
		var summary map[string]any
		if err := json.Unmarshal([]byte(got), &summary); err != nil {
			t.Fatalf("summary %q is not a JSON object: %v", got, err)
		}
		if summary["task"] != record.Directory || summary["task_id"] != record.ID || summary["status"] != "abandoned" {
			t.Fatalf("summary %v, want task %q, task_id %q, status abandoned", summary, record.Directory, record.ID)
		}
		if loaded := taLoad(t, record.Directory); loaded.Abandonment == nil || loaded.Abandonment.Reason != "no longer needed" {
			t.Fatalf("stored abandonment %+v, want reason %q", loaded.Abandonment, "no longer needed")
		}
	})

	t.Run("usage errors write nothing", func(t *testing.T) {
		// C2: missing --task or --reason, or an extra positional argument,
		// errors and leaves the record unchanged. A2: a blank reason errors.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		tests := []struct {
			name string
			args []string
		}{
			{"missing --task", []string{"task", "abandon", "--reason", "x"}},
			{"missing --reason", []string{"task", "abandon", "--task", record.Directory}},
			{"extra positional argument", []string{"task", "abandon", "--task", record.Directory, "--reason", "x", "extra"}},
			{"blank reason", []string{"task", "abandon", "--task", record.Directory, "--reason", "   "}},
			{"empty reason", []string{"task", "abandon", "--task", record.Directory, "--reason", ""}},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				// The working directory is the repository, so a command that
				// guessed the task from it would still be caught.
				h.config.WorkingDir = func() (string, error) { return repo, nil }
				if _, err := h.taRun(t, test.args...); err == nil {
					t.Fatalf("cw %v returned nil", test.args)
				}
				taExpectUnchanged(t, record)
			})
		}
	})

	t.Run("second abandon errors", func(t *testing.T) {
		// C2: abandoning an abandoned record errors and writes nothing.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		if _, err := h.taRun(t, "task", "abandon", "--task", record.Directory, "--reason", "first"); err != nil {
			t.Fatalf("first abandon: %v", err)
		}
		abandoned := taLoad(t, record.Directory)
		if _, err := h.taRun(t, "task", "abandon", "--task", record.Directory, "--reason", "second"); err == nil {
			t.Fatal("second abandon returned nil")
		}
		taExpectUnchanged(t, abandoned)
	})

	t.Run("dry-run is rejected", func(t *testing.T) {
		// C3: the root --dry-run flag is rejected and nothing is written.
		repo := hkRepo(t)
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		if _, err := h.taRun(t, "--dry-run", "task", "abandon", "--task", record.Directory, "--reason", "x"); err == nil {
			t.Fatal("--dry-run task abandon returned nil")
		}
		taExpectUnchanged(t, record)
	})

	t.Run("nonexistent task directory", func(t *testing.T) {
		// A6 through the command: a task directory that does not exist errors.
		h := newHKHarness(t)
		missing := filepath.Join(hkCanonicalTempDir(t), "missing")
		if _, err := h.taRun(t, "task", "abandon", "--task", missing, "--reason", "x"); err == nil {
			t.Fatal("task abandon of a missing directory returned nil")
		}
		if _, err := os.Stat(missing); !os.IsNotExist(err) {
			t.Fatalf("task abandon created %s: %v", missing, err)
		}
	})

	t.Run("verify is refused after abandonment", func(t *testing.T) {
		// A4 through the commands: a record assessed so that verify could
		// pass is abandoned; `cw task verify` then errors and the abandoned
		// revision stays the latest.
		repo := hkRepo(t)
		source := []byte("verified source\n")
		hkWriteFile(t, repo, "source.txt", string(source))
		h := newHKHarness(t)
		record := h.start(t, repo, hkSubstantialPlan)
		h.assess(t, record, source)
		if _, err := h.taRun(t, "task", "abandon", "--task", record.Directory, "--reason", "given up"); err != nil {
			t.Fatalf("task abandon: %v", err)
		}
		abandoned := taLoad(t, record.Directory)
		if _, err := h.taRun(t, "task", "verify", "--task", record.Directory); err == nil {
			t.Fatal("task verify of an abandoned record returned nil")
		}
		taExpectUnchanged(t, abandoned)
	})
}
