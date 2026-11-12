//go:build linux || darwin

package graphify

import (
	"os/exec"
	"syscall"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

func supported() error { return nil }

func isolate(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// killGroup sends one SIGKILL to the group so a running leader exits and Wait
// can return. It does not wait for the group to empty; stopGroup does.
func killGroup(command *exec.Cmd) {
	if command.Process != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
}

// stopGroup kills the group until no live member remains.
func stopGroup(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return proc.KillGroup(command.Process.Pid)
}
