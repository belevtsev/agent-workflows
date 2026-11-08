//go:build unix

package main

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// ownProcessGroup starts command in a new process group, so the browser or
// node and every helper they start can be stopped together.
func ownProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup kills the process group ownProcessGroup created. A group
// that has already exited is not an error.
func killProcessGroup(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// waitProcessGroupGone waits, at most limit, until no running process is left
// in the group ownProcessGroup created. A zombie does not count where the
// system can tell it apart: without a reaping init, as when cwdev is PID 1 in
// a container, the killed helpers stay zombies and the wait would otherwise
// always run to the limit.
func waitProcessGroupGone(command *exec.Cmd, limit time.Duration) {
	if command.Process == nil {
		return
	}
	group := command.Process.Pid
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-group, 0); errors.Is(err, syscall.ESRCH) {
			return
		}
		if !processGroupRunning(group) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
