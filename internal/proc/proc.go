// Package proc runs one external program directly, never through a shell, in
// a process group of its own, and stops the whole group when the run ends.
//
// One kill(-pgid, SIGKILL) is not enough on macOS: a member that is forking
// when the signal is sent can produce a child that joins the group after the
// kernel has walked it, and that child survives. Linux redirects or restarts
// such a fork, but the same loop is correct there too. KillGroup therefore
// kills until the group has no live member, and Run calls it whenever a run
// ends, after a normal exit as well, so no descendant outlives its run.
//
// Process groups are used on Linux and macOS. On Windows the program starts
// suspended in a job object of its own, and Run ends the whole job; see
// group_windows.go for how that differs. Elsewhere Run stops only the program
// itself, at once. KillGroup fails with errors.ErrUnsupported on every
// platform but Linux and macOS.
package proc

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

// DefaultWaitDelay is how long a bounded run's output may still arrive after
// the program exits or is stopped when Spec.WaitDelay is zero.
const DefaultWaitDelay = 100 * time.Millisecond

// Spec says what to run and how to bound it.
type Spec struct {
	// Path is the program. A name without a path separator is looked up in
	// cw's own PATH, as exec.Command does; a program the caller resolved
	// itself, such as a verified toolchain, is passed as an absolute path and
	// executed as given.
	Path string
	// Args are the arguments after the program name.
	Args []string
	// Dir is the working directory; empty means cw's own.
	Dir string
	// Env is the program's complete environment, such as a filtered copy of
	// cw's own. nil gives it an empty environment, never cw's.
	Env []string
	// Stdin is the program's input; nil gives it none.
	Stdin io.Reader
	// Stdout, when set, receives the output. Otherwise Result.Stdout keeps
	// the first StdoutLimit bytes.
	Stdout      io.Writer
	StdoutLimit int
	// Stderr, when set, receives the error output. Otherwise Result.Stderr
	// keeps the last StderrLimit bytes.
	Stderr      io.Writer
	StderrLimit int
	// Timeout bounds the run; zero leaves the bound to ctx. A stop kills the
	// group at once.
	Timeout time.Duration
	// WaitDelay bounds how long output may still arrive after the program
	// exits or is stopped, for a descendant that keeps the pipes open
	// (exec.Cmd.WaitDelay); a descendant that left the group survives the
	// kill. Zero means DefaultWaitDelay when the run has a bound (Timeout, or
	// a ctx that can end), and the end of the output when it has none.
	WaitDelay time.Duration
}

// Result is how a run ended.
type Result struct {
	// Stdout and Stderr are the output Run kept; see Spec.
	Stdout, Stderr []byte
	// Code is the exit status, or -1 when the program did not exit on its
	// own: it never started, or a signal ended it, such as the kill when the
	// run was stopped. A program that exited before the stop, while a
	// descendant held its output open, keeps its status.
	Code int
	// Stopped is set when ctx ended or Timeout passed, so the process group
	// was killed or the program was never started.
	Stopped bool
	// Cancelled is set when ctx was cancelled, as opposed to running out of
	// time.
	Cancelled bool
	// Overflow is set when the output exceeded StdoutLimit.
	Overflow bool
	// Err is what starting or waiting for the program returned, as os/exec
	// reports it: a start failure, *exec.ExitError for a non-zero status or a
	// signal, or exec.ErrWaitDelay when a descendant held the output open
	// past WaitDelay after the program exited (Code is then still set).
	Err error
	// Cleanup is set when a member of the process group was still alive
	// after it had been killed repeatedly for a bound.
	Cleanup error
}

// Run starts the program in a process group of its own and waits for it.
// When the program exits, Run kills whatever is left in the group; when ctx
// ends or Timeout passes first, Run stops the group. Either way Run returns
// only once no member of the group is alive, or with Result.Cleanup set.
func Run(ctx context.Context, spec Spec) Result {
	runCtx := ctx
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}
	if runCtx.Err() != nil {
		return Result{Code: -1, Stopped: true, Cancelled: interrupted(ctx)}
	}
	command, out := prepare(ctx, spec)
	ownGroup(command)
	if err := command.Start(); err != nil {
		return Result{Code: -1, Err: err}
	}
	// joinGroup has reaped the program when it fails.
	if err := joinGroup(command); err != nil {
		return Result{Code: -1, Err: err}
	}
	got := await(ctx, runCtx, command)
	got.Stdout, got.Overflow, got.Stderr = out.stdout.Bytes(), out.stdout.Overflow(), out.stderr.Bytes()
	return got
}

// streams are where Run keeps a run's output.
type streams struct {
	stdout *HeadBuffer
	stderr *TailBuffer
}

// prepare builds the command for spec, without starting it.
func prepare(ctx context.Context, spec Spec) (*exec.Cmd, streams) {
	command := exec.Command(spec.Path, spec.Args...) //nolint:gosec // G204: the caller names a fixed program and argv; no shell is involved.
	command.Dir, command.Env, command.Stdin, command.WaitDelay = spec.Dir, spec.Env, spec.Stdin, spec.WaitDelay
	if command.Env == nil {
		command.Env = []string{}
	}
	// Without a delay, a descendant in another session that keeps a pipe
	// open would hold Run past its bound for as long as it lives.
	if command.WaitDelay == 0 && (spec.Timeout > 0 || ctx.Done() != nil) {
		command.WaitDelay = DefaultWaitDelay
	}
	out := streams{stdout: &HeadBuffer{Limit: spec.StdoutLimit}, stderr: &TailBuffer{Limit: spec.StderrLimit}}
	command.Stdout = out.stdout
	if spec.Stdout != nil {
		command.Stdout = spec.Stdout
	}
	command.Stderr = out.stderr
	if spec.Stderr != nil {
		command.Stderr = spec.Stderr
	}
	return command, out
}

// await waits for a started command until it exits or runCtx ends, and ends
// or stops its group accordingly.
func await(ctx, runCtx context.Context, command *exec.Cmd) Result {
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	got := Result{Code: -1}
	select {
	case got.Err = <-done:
		got.Cleanup = endGroup(command)
	case <-runCtx.Done():
		got.Stopped, got.Cancelled = true, interrupted(ctx)
		// Killing the group closes every pipe a member holds. A descendant
		// that left the group can still hold one; WaitDelay bounds that wait.
		got.Err, got.Cleanup = stopGroup(command, done)
	}
	// After a stop, the program may have exited on its own before it, while
	// a descendant held its output open; it keeps its status.
	if state := command.ProcessState; state != nil && state.Exited() {
		got.Code = state.ExitCode()
	}
	return got
}

// interrupted reports whether the run itself was cancelled, as opposed to
// running out of time.
func interrupted(ctx context.Context) bool { return errors.Is(ctx.Err(), context.Canceled) }

// HeadBuffer keeps the first Limit bytes written to it and records whether
// more arrived. A write never fails, so a program is not stopped by a broken
// pipe.
type HeadBuffer struct {
	Limit    int
	data     []byte
	overflow bool
}

func (b *HeadBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if room := max(b.Limit-len(b.data), 0); n > room {
		b.overflow = true
		p = p[:room]
	}
	b.data = append(b.data, p...)
	return n, nil
}

// Bytes returns the bytes kept.
func (b *HeadBuffer) Bytes() []byte { return b.data }

// Overflow reports whether more than Limit bytes were written.
func (b *HeadBuffer) Overflow() bool { return b.overflow }

// TailBuffer keeps the last Limit bytes written to it. A write never fails.
type TailBuffer struct {
	Limit int
	data  []byte
}

func (b *TailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if extra := len(b.data) - max(b.Limit, 0); extra > 0 {
		b.data = append(b.data[:0], b.data[extra:]...)
	}
	return len(p), nil
}

// Bytes returns the bytes kept.
func (b *TailBuffer) Bytes() []byte { return b.data }
