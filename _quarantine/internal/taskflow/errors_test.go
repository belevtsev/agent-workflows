package taskflow

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

// Oracle: each refusal matches its exported sentinel with errors.Is, and its
// message is the text cw printed before the sentinels were exported, quoted
// here from the parent commit.
func TestRefusalsMatchTheirSentinelsWithUnchangedMessages(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	if _, err := Abandon(record.Directory, "superseded"); err != nil {
		t.Fatal(err)
	}
	_, err := Abandon(record.Directory, "again")
	if !errors.Is(err, ErrAbandoned) || err.Error() != "task was abandoned; start a new task with 'cw task start'" {
		t.Errorf("abandoning an abandoned task: %v, want ErrAbandoned", err)
	}
	if !errors.Is(errTaskBusy, ErrTaskLocked) || errTaskBusy.Error() != "task is locked: another cw kept writing it for 10 seconds; retry when it finishes" {
		t.Errorf("a busy writer's error %q does not match ErrTaskLocked", errTaskBusy)
	}
	if ErrTaskLocked.Error() != "task is locked; inspect an interrupted writer before removing its .lock directory" ||
		ErrRevisionLimit.Error() != "task revision limit reached; start a new task" {
		t.Errorf("sentinel messages changed: %q, %q", ErrTaskLocked, ErrRevisionLimit)
	}
}

// Oracle: input that a public use case refuses before it writes matches
// ErrInvalidInput, keeps its message, and the task gains no revision.
func TestInvalidInputMatchesErrInvalidInput(t *testing.T) {
	t.Parallel()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	var plan Plan
	for _, row := range []struct {
		name, want string
		call       func() error
	}{
		{"unknown JSON member", "invalid task JSON: ", func() error {
			return Decode(strings.NewReader(`{"extra":1}`), &plan)
		}},
		{"plan without a title", "plan needs a title and kind small or substantial", func() error {
			return ValidatePlan(&Plan{})
		}},
		{"blank abandonment reason", "an abandonment reason of 1 to 16384 bytes is required", func() error {
			_, err := Abandon(record.Directory, " ")
			return err
		}},
		{"unknown phase", "phase must be before or final", func() error {
			_, err := PrepareContext(t.Context(), record.Directory, "after")
			return err
		}},
		{"check outside the plan", "check is not in the explicit coordinator plan", func() error {
			_, err := RunCheck(t.Context(), record.Directory, "absent")
			return err
		}},
		{"unknown worker result", "worker result references an unknown assignment", func() error {
			_, err := SubmitWorkerResult(t.Context(), record.Directory, WorkerResult{AssignmentID: "absent"})
			return err
		}},
		{"repeated contract symbol", "symbol Run is repeated", func() error {
			_, err := ParseContractSymbols("Run,Run")
			return err
		}},
	} {
		err := row.call()
		if !errors.Is(err, ErrInvalidInput) || !strings.HasPrefix(err.Error(), row.want) {
			t.Errorf("%s: %v, want ErrInvalidInput with message %q", row.name, err, row.want)
		}
	}
	if stored, err := Load(record.Directory); err != nil || stored.Revision != record.Revision {
		t.Fatalf("refused input wrote a revision: revision %d, %v", stored.Revision, err)
	}
}

// Oracle: a read error and a damaged stored record are failures, not invalid
// input: neither matches ErrInvalidInput, and the read error keeps its cause.
func TestFailuresDoNotMatchErrInvalidInput(t *testing.T) {
	t.Parallel()
	var plan Plan
	if err := Decode(iotest.ErrReader(fs.ErrPermission), &plan); !errors.Is(err, fs.ErrPermission) || errors.Is(err, ErrInvalidInput) {
		t.Errorf("Decode of an unreadable input: %v, want fs.ErrPermission only", err)
	}
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000001.json"), []byte(`{"extra":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err == nil || errors.Is(err, ErrInvalidInput) {
		t.Errorf("Load of a damaged record: %v, want a failure that is not ErrInvalidInput", err)
	}
}
