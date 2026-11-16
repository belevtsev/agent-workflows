package proc

import (
	"context"
	"io"
	"os/exec"
)

// ForegroundSpec describes an explicitly requested interactive program. Its
// streams are inherited directly from the caller; nil streams discard I/O.
type ForegroundSpec struct {
	Path   string
	Args   []string
	Env    []string // nil means an empty environment; callers opt into inheritance.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// RunForeground runs a program in the caller's foreground process group so it
// can read the terminal. Cancellation stops the program itself; descendants
// remain the interactive host's responsibility. Bounded, noninteractive work
// uses Run and its process-group cleanup instead.
func RunForeground(ctx context.Context, spec ForegroundSpec) Result {
	command := exec.CommandContext(ctx, spec.Path, spec.Args...) //nolint:gosec // G204: explicit executable and literal argv, no shell.
	command.Env = spec.Env
	if command.Env == nil {
		command.Env = []string{}
	}
	command.Stdin, command.Stdout, command.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	command.WaitDelay = DefaultWaitDelay
	err := command.Run()
	result := Result{Code: -1, Err: err, Stopped: ctx.Err() != nil, Cancelled: interrupted(ctx)}
	if command.ProcessState != nil && command.ProcessState.Exited() {
		result.Code = command.ProcessState.ExitCode()
	}
	return result
}
