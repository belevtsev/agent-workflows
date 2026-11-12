//go:build linux || darwin

package proc_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

// sessionHelperEnv names the FIFO through which the test binary, started as
// TestRunSessionHelper, reports that it has left the run's process group.
const sessionHelperEnv = "CW_PROC_SESSION_HELPER_READY"

// TestRunSessionHelper does nothing in a normal test run. Started by
// TestRunStopsAtItsTimeoutWhileAnotherSessionHoldsItsOutput, it moves into a
// session of its own, which the group kill cannot reach, writes its pid to the
// FIFO and keeps the run's output pipe open until the test kills it.
func TestRunSessionHelper(t *testing.T) {
	ready := os.Getenv(sessionHelperEnv)
	if ready == "" {
		return
	}
	if _, err := syscall.Setsid(); err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(ready, []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		os.Exit(3)
	}
	// Bounds the life of a helper the test failed to kill.
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

// Oracle: with WaitDelay unset, a descendant in another session that keeps
// the output pipe open does not hold Run past its Timeout: Run returns as
// stopped (not cancelled) within 10 s, while the descendant would hold the
// pipe for 30 s. The control is the descendant's pid, which it writes only
// after leaving the group, so it held the pipe when the group was killed.
func TestRunStopsAtItsTimeoutWhileAnotherSessionHoldsItsOutput(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ready := filepath.Join(t.TempDir(), "ready")
	if err := syscall.Mkfifo(ready, 0o600); err != nil {
		t.Fatal(err)
	}
	helper := 0
	t.Cleanup(func() {
		// Release a reader still waiting, then end the helper.
		if writer, err := os.OpenFile(ready, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
		if helper > 0 {
			_ = syscall.Kill(helper, syscall.SIGKILL)
		}
	})
	results := make(chan proc.Result, 1)
	go func() {
		results <- proc.Run(t.Context(), proc.Spec{Path: "/bin/sh",
			Args:    []string{"-c", `"$1" -test.run='^TestRunSessionHelper$' & exec sleep 60`, "sh", binary},
			Env:     []string{"PATH=" + os.Getenv("PATH"), sessionHelperEnv + "=" + ready},
			Timeout: 2 * time.Second})
	}()
	pids := make(chan int, 1)
	go func() {
		line, _ := os.ReadFile(ready)
		pid, _ := strconv.Atoi(strings.TrimSpace(string(line)))
		pids <- pid
	}()
	select {
	case helper = <-pids:
	case <-time.After(10 * time.Second):
		t.Fatal("the helper did not report leaving the process group")
	}
	if helper <= 0 {
		t.Fatal("the helper did not report its pid")
	}
	select {
	case got := <-results:
		if !got.Stopped || got.Cancelled || got.Code != -1 {
			t.Fatalf("Stopped=%v Cancelled=%v Code=%d, want a stop at the timeout", got.Stopped, got.Cancelled, got.Code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run (Timeout 2 s) had not returned 10 s after a process in another session took hold of its output")
	}
}
