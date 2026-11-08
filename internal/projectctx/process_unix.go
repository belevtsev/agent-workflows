//go:build unix

package projectctx

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func configureProbeProcess(command *exec.Cmd) {
	// A timed-out wrapper must not leave child tools running. Each probe owns
	// its process group; cancellation and the MCP completion stop that group.
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error { return killProbeProcess(command) }
}

// killProbeProcess kills the probe's whole process group, which also closes
// every pipe a member still holds.
func killProbeProcess(command *exec.Cmd) error {
	err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}

func cleanupProbeProcess(command *exec.Cmd) {
	if command.Process != nil {
		_ = killProbeProcess(command)
	}
}
