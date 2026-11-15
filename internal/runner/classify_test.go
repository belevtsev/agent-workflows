package runner

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/gatecub/v3/internal/decl"
	"github.com/belevtsev/gatecub/v3/internal/proc"
	"github.com/belevtsev/gatecub/v3/internal/store"
)

// How a run's end is classified, for every end that is decided before the
// snapshot after the run is taken. An end with exit status 0 is passed or
// source_changed by comparing that snapshot with the one before, which needs
// a repository and git; the scenarios in scenarios/evidence hold those two.
//
// Requirement "Outcome of a run" and "Interrupted runs" in
// openspec/changes/reduce-cw-to-evidence-gate/specs/check-evidence/spec.md.
func TestClassifyEndsDecidedWithoutASnapshot(t *testing.T) {
	t.Parallel()
	job := Job{Check: decl.Check{ID: "unit", Run: []string{"unit-program", "arg"}, Timeout: 90 * time.Second}}
	cases := []struct {
		name    string
		ended   proc.Result
		outcome string
		exit    *int   // nil: no exit status recorded
		detail  string // a part the detail holds; "" means the detail is empty
	}{
		{
			name:    "failed with its exit status",
			ended:   proc.Result{Code: 3, Err: &exec.ExitError{}},
			outcome: store.Failed, exit: intp(3),
		},
		{
			name:    "failed with exit status 1",
			ended:   proc.Result{Code: 1, Err: &exec.ExitError{}},
			outcome: store.Failed, exit: intp(1),
		},
		{
			name:    "timed out",
			ended:   proc.Result{Code: -1, Stopped: true},
			outcome: store.TimedOut, detail: "timeout of 1m30s",
		},
		{
			name:    "timed out after the program exited while a descendant held its output",
			ended:   proc.Result{Code: 0, Stopped: true, Err: exec.ErrWaitDelay},
			outcome: store.TimedOut, detail: "timeout of 1m30s",
		},
		{
			name:    "interrupted",
			ended:   proc.Result{Code: -1, Stopped: true, Cancelled: true},
			outcome: store.Interrupted,
		},
		{
			name:    "could not be started",
			ended:   proc.Result{Code: -1, Err: exec.ErrNotFound},
			outcome: store.Failed, detail: "the program `unit-program` could not be started: " + exec.ErrNotFound.Error(),
		},
		{
			name:    "could not be started, without an error",
			ended:   proc.Result{Code: -1},
			outcome: store.Failed, detail: "the program `unit-program` could not be started",
		},
		{
			name:    "ended by something outside gatecub",
			ended:   proc.Result{Code: -1, Err: &exec.ExitError{}},
			outcome: store.Failed, detail: "the program `unit-program` did not exit on its own",
		},
		{
			name:    "ended by something outside gatecub, wrapped",
			ended:   proc.Result{Code: -1, Err: wrapped{&exec.ExitError{}}},
			outcome: store.Failed, detail: "did not exit on its own",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			// No repository and no store: none of these ends may look at the
			// source. A nil dereference here means classify took a snapshot.
			end := classify(context.Background(), nil, nil, job, c.ended)
			if end.outcome != c.outcome {
				t.Errorf("outcome %q, want %q", end.outcome, c.outcome)
			}
			switch {
			case c.exit == nil && end.exit != nil:
				t.Errorf("exit status %d recorded, want none", *end.exit)
			case c.exit != nil && end.exit == nil:
				t.Errorf("no exit status recorded, want %d", *c.exit)
			case c.exit != nil && *end.exit != *c.exit:
				t.Errorf("exit status %d, want %d", *end.exit, *c.exit)
			}
			switch {
			case c.detail == "" && end.detail != "":
				t.Errorf("detail %q, want none", end.detail)
			case !strings.Contains(end.detail, c.detail):
				t.Errorf("detail %q, want it to hold %q", end.detail, c.detail)
			}
			if len(end.differ) != 0 || end.more != 0 {
				t.Errorf("differing paths %v (and %d more), want none", end.differ, end.more)
			}
		})
	}
}

// On Windows Ctrl+C reaches the check's program as well as gatecub, and the
// program may end before gatecub sees the signal. An end with the exit status the
// console gives, and a failure while the run is being cancelled, are
// therefore interrupted; a failure while the run goes on is failed (second
// challenge, finding P1).
func TestClassifyAnEndByTheConsoleOrDuringCancellation(t *testing.T) {
	t.Parallel()
	job := Job{Check: decl.Check{ID: "unit", Run: []string{"unit-program"}, Timeout: time.Minute}}
	live := context.Background()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	// Converted at run time, so that the test builds where int has 32 bits.
	console, near := int64(0xC000013A), int64(0xC000013B)
	cases := []struct {
		name    string
		ctx     context.Context
		ended   proc.Result
		outcome string
		exit    *int
	}{
		{
			name:  "the exit status of a program the Windows console ended, as Windows gives it",
			ctx:   live,
			ended: proc.Result{Code: int(console), Err: &exec.ExitError{}}, outcome: store.Interrupted,
		},
		{
			name:  "the same exit status as a negative 32-bit number",
			ctx:   live,
			ended: proc.Result{Code: -1073741510, Err: &exec.ExitError{}}, outcome: store.Interrupted,
		},
		{
			name:  "a failure while the run is being cancelled",
			ctx:   cancelled,
			ended: proc.Result{Code: 1, Err: &exec.ExitError{}}, outcome: store.Interrupted,
		},
		{
			name:  "an end by a signal while the run is being cancelled",
			ctx:   cancelled,
			ended: proc.Result{Code: -1, Err: &exec.ExitError{}}, outcome: store.Interrupted,
		},
		{
			name:  "a failure while the run goes on",
			ctx:   live,
			ended: proc.Result{Code: 1, Err: &exec.ExitError{}}, outcome: store.Failed, exit: intp(1),
		},
		{
			name:  "a failure with a status close to the console's",
			ctx:   live,
			ended: proc.Result{Code: int(near), Err: &exec.ExitError{}}, outcome: store.Failed, exit: intp(int(near)),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			end := classify(c.ctx, nil, nil, job, c.ended)
			if end.outcome != c.outcome {
				t.Errorf("outcome %q, want %q", end.outcome, c.outcome)
			}
			switch {
			case c.exit == nil && end.exit != nil:
				t.Errorf("exit status %d recorded, want none", *end.exit)
			case c.exit != nil && (end.exit == nil || *end.exit != *c.exit):
				t.Errorf("exit status %v recorded, want %d", end.exit, *c.exit)
			}
		})
	}
}

type wrapped struct{ err error }

func (w wrapped) Error() string { return "wrapped: " + w.err.Error() }
func (w wrapped) Unwrap() error { return w.err }

func intp(n int) *int { return &n }
