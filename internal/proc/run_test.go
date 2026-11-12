//go:build linux || darwin

package proc_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// Oracle: stdout keeps its first StdoutLimit bytes and records the overflow,
// stderr keeps its last StderrLimit bytes, and the exit status is data in
// Code while Err is the *exec.ExitError os/exec reports for it.
func TestRunKeepsOutputHeadErrorTailAndExitStatus(t *testing.T) {
	got := proc.Run(t.Context(), proc.Spec{Path: "/bin/sh", Args: []string{"-c", "printf 0123456789; printf abcdefghij >&2; exit 3"},
		StdoutLimit: 4, StderrLimit: 4})
	if string(got.Stdout) != "0123" || !got.Overflow || string(got.Stderr) != "ghij" {
		t.Fatalf("output = %q overflow=%v, error output = %q; want %q, true, %q", got.Stdout, got.Overflow, got.Stderr, "0123", "ghij")
	}
	exit, isExit := errors.AsType[*exec.ExitError](got.Err)
	if got.Code != 3 || !isExit || exit.ExitCode() != 3 || got.Stopped || got.Cleanup != nil {
		t.Fatalf("Code=%d Err=%v Stopped=%v Cleanup=%v, want exit status 3 as data and as *exec.ExitError", got.Code, got.Err, got.Stopped, got.Cleanup)
	}
}

// Oracle: a nil Env starts the program with no environment at all, so env
// prints nothing although the test's own environment is not empty.
func TestRunGivesNilEnvAnEmptyEnvironment(t *testing.T) {
	t.Setenv("CW_PROC_INHERITED", "1")
	got := proc.Run(t.Context(), proc.Spec{Path: "env", StdoutLimit: 1024})
	if got.Err != nil || got.Code != 0 || len(got.Stdout) != 0 {
		t.Fatalf("env with a nil Env printed %q (Code=%d Err=%v), want no variables", got.Stdout, got.Code, got.Err)
	}
}

// cancelOnWrite cancels a run's context on the program's first output, so the
// stop happens while the program is known to be running.
type cancelOnWrite struct{ cancel context.CancelFunc }

func (w cancelOnWrite) Write(p []byte) (int, error) {
	w.cancel()
	return len(p), nil
}

// Oracle: Timeout stops a running program as out of time (Stopped, not
// Cancelled); cancelling ctx stops it, or does not start it, as Cancelled.
// No case has an exit status, and none waits for the program's own 60 s
// (ctx, the 20 s hang guard, is still live at the end).
func TestRunStopsAtItsTimeoutOrCancellation(t *testing.T) {
	script := []string{"-c", "echo started; exec sleep 60"}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	running, stop := context.WithCancel(ctx)
	defer stop()
	ended, end := context.WithCancel(ctx)
	end()
	for _, run := range []struct {
		name      string
		ctx       context.Context
		spec      proc.Spec
		cancelled bool
	}{
		{"timeout", ctx, proc.Spec{Path: "/bin/sh", Args: script, Timeout: 100 * time.Millisecond}, false},
		{"cancelled while running", running, proc.Spec{Path: "/bin/sh", Args: script, Stdout: cancelOnWrite{stop}}, true},
		{"cancelled before start", ended, proc.Spec{Path: "/bin/sh", Args: script}, true},
	} {
		got := proc.Run(run.ctx, run.spec)
		if !got.Stopped || got.Cancelled != run.cancelled || got.Code != -1 || got.Cleanup != nil || ctx.Err() != nil {
			t.Errorf("%s: Stopped=%v Cancelled=%v Code=%d Cleanup=%v guard=%v, want a stop with Cancelled=%v", run.name, got.Stopped, got.Cancelled, got.Code, got.Cleanup, ctx.Err(), run.cancelled)
		}
	}
}
