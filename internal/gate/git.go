package gate

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"
)

// gitTimeout bounds each git command the gate runs.
const gitTimeout = 5 * time.Second

// Git runs git with args in dir and returns its standard output. An error
// means git did not run or did not succeed; the gate then fails open for
// that repository.
type Git func(ctx context.Context, dir string, args ...string) (string, error)

// output runs git in dir and returns its output without trailing line
// ends; empty output is an error.
func (git Git) output(ctx context.Context, dir string, args ...string) (string, error) {
	output, err := git(ctx, dir, args...)
	if err != nil {
		return "", err
	}
	value := strings.TrimRight(output, "\r\n")
	if value == "" {
		return "", errors.New("git printed nothing")
	}
	return value, nil
}

// ExecGit is the Git that runs the git on PATH in dir, with the file system
// monitor off, without optional index writes and with a deadline, and
// returns its raw output.
func ExecGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-C", dir}, args...)...) //nolint:gosec // G204: fixed git binary, no shell; callers pass cw's own subcommands and revisions git resolved.
	command.Stderr = io.Discard
	output, err := command.Output()
	return string(output), err
}
