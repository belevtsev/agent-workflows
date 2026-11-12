package publish

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

const (
	maxStdout = 8 << 20
	maxStderr = 64 << 10
	// stderrDetail is how much of a failed command's stderr the step detail
	// keeps.
	stderrDetail = 2 << 10
	// stopGrace is how long a stopped command's process group may take to
	// exit after SIGTERM before SIGKILL.
	stopGrace = 5 * time.Second
	// pipeDelay bounds how long output pipes may stay open after the command
	// exits, for a descendant that keeps them.
	pipeDelay = 2 * time.Second
)

// tool runs one external program (git or gh) directly, never through a
// shell, in the worktree with a fixed environment and a per-command bound.
type tool struct {
	path    string
	dir     string
	env     []string
	timeout time.Duration
}

// result is how a command ended.
type result struct {
	stdout []byte
	stderr []byte
	// code is the exit status, or -1 when the command did not exit on its own.
	code int
	// stopped is set when the command was stopped (section 2) or never
	// started because the run was already cancelled or out of time.
	stopped bool
	// cancelled is set when the run itself was interrupted.
	cancelled bool
	// overflow is set when stdout exceeded its bound.
	overflow bool
	err      error
}

// succeeded reports whether the command exited 0 on its own.
func (r result) succeeded() bool { return r.err == nil && !r.stopped && r.code == 0 }

// exited reports whether the command ran to an exit status of its own.
func (r result) exited() bool { return r.err == nil && !r.stopped && r.code >= 0 }

// why describes a command that did not succeed, with the tail of its stderr.
func (r result) why() string {
	var text string
	switch {
	case r.cancelled:
		text = "interrupted"
	case r.stopped:
		text = "timed out"
	case r.err != nil:
		text = r.err.Error()
	case r.overflow:
		text = "output exceeded its limit"
	default:
		text = "exit status " + strconv.Itoa(r.code)
	}
	if tail := stderrTail(r.stderr); tail != "" {
		text += ": " + tail
	}
	return text
}

func stderrTail(stderr []byte) string {
	if len(stderr) > stderrDetail {
		stderr = stderr[len(stderr)-stderrDetail:]
	}
	return strings.Join(strings.Fields(string(stderr)), " ")
}

// run starts the program with args and waits for it. stdin may be nil
// (empty input); stdout, when not nil, receives the output instead of the
// bounded buffer. When ctx is done or the command's bound passes, the command
// is stopped: SIGTERM to its process group, then SIGKILL to the group if a
// member is still alive after stopGrace. A command that exits on its own
// leaves the rest of its group running, such as a hook's background job.
func (t tool) run(ctx context.Context, stdin []byte, stdout io.Writer, args ...string) result {
	spec := proc.Spec{Path: t.path, Args: args, Dir: t.dir, Env: t.env, Stdout: stdout, StdoutLimit: maxStdout, StderrLimit: maxStderr,
		Timeout: t.timeout, Grace: stopGrace, KeepGroupOnExit: true, WaitDelay: pipeDelay}
	if stdin != nil {
		spec.Stdin = bytes.NewReader(stdin)
	}
	ran := proc.Run(ctx, spec)
	got := result{stdout: ran.Stdout, stderr: ran.Stderr, code: ran.Code, stopped: ran.Stopped, cancelled: ran.Cancelled, overflow: ran.Overflow}
	switch {
	case ran.Stopped:
		// A stopped command has no exit status of its own here, even one it
		// gave in answer to the SIGTERM.
		got.code = -1
		if err := errors.Join(ran.Err, ran.Cleanup); err != nil && !isExitError(err) {
			got.err = err
		}
	case ran.Code >= 0:
		// Output a descendant kept open past pipeDelay is not the command's
		// own; its exit status stands.
		if ran.Err != nil && !isExitError(ran.Err) && !errors.Is(ran.Err, exec.ErrWaitDelay) {
			got.err = ran.Err
		}
	default:
		got.err = ran.Err
		if got.err == nil {
			got.err = errors.New("the command ended without an exit status")
		}
	}
	return got
}

// interrupted reports whether the run itself was cancelled by a signal, as
// opposed to running out of time.
func interrupted(ctx context.Context) bool { return errors.Is(ctx.Err(), context.Canceled) }

func isExitError(err error) bool {
	_, ok := errors.AsType[*exec.ExitError](err)
	return ok
}
