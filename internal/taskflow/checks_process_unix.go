//go:build linux || darwin

package taskflow

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

func configureCheckProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		// One kill makes the leader exit so Wait can return; cleanupCheckProcess
		// then stops whatever that kill missed.
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
}

// cleanupCheckProcess stops the owned process group before a terminal check
// result is recorded. A normally exiting wrapper can leave redirected
// descendants in the group, and one kill can miss a member that another member
// was forking at that moment (seen on macOS), so kill until it is empty.
func cleanupCheckProcess(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return proc.KillGroup(command.Process.Pid)
}
