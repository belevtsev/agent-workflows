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
// member is still alive after stopGrace.
func (t tool) run(ctx context.Context, stdin []byte, stdout io.Writer, args ...string) result {
	commandCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	if commandCtx.Err() != nil {
		return result{code: -1, stopped: true, cancelled: interrupted(ctx)}
	}
	command := exec.Command(t.path, args...) //nolint:gosec // G204: t.path is the git or gh binary resolved at start, no shell.
	command.Dir, command.Env = t.dir, t.env
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	out := &boundedBuffer{limit: maxStdout}
	command.Stdout = out
	if stdout != nil {
		command.Stdout = stdout
	}
	stderr := &tailBuffer{limit: maxStderr}
	command.Stderr = stderr
	command.WaitDelay = pipeDelay
	ownProcessGroup(command)
	if err := command.Start(); err != nil {
		return result{code: -1, err: err}
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	var waitErr error
	stopped := false
	select {
	case waitErr = <-done:
	case <-commandCtx.Done():
		stopped = true
		waitErr = stopProcessGroup(command, done)
	}
	got := result{stdout: out.data, stderr: stderr.data, code: -1, overflow: out.overflow, stopped: stopped}
	if stopped {
		got.cancelled = interrupted(ctx)
		if waitErr != nil && !isExitError(waitErr) {
			got.err = waitErr
		}
		return got
	}
	if state := command.ProcessState; state != nil && state.Exited() {
		got.code = state.ExitCode()
		// Output a descendant kept open past pipeDelay is not the command's
		// own; its exit status stands.
		if waitErr != nil && !isExitError(waitErr) && !errors.Is(waitErr, exec.ErrWaitDelay) {
			got.err = waitErr
		}
		return got
	}
	got.err = waitErr
	if got.err == nil {
		got.err = errors.New("the command ended without an exit status")
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

// boundedBuffer keeps the first limit bytes and records an overflow; it never
// fails a write, so the program is not stopped by a broken pipe.
type boundedBuffer struct {
	data     []byte
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	room := b.limit - len(b.data)
	if len(p) > room {
		b.overflow = true
		b.data = append(b.data, p[:max(room, 0)]...)
		return len(p), nil
	}
	b.data = append(b.data, p...)
	return len(p), nil
}

// tailBuffer keeps the last limit bytes.
type tailBuffer struct {
	data  []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	if extra := len(b.data) - b.limit; extra > 0 {
		b.data = append(b.data[:0], b.data[extra:]...)
	}
	return len(p), nil
}
