//go:build linux || darwin

package publish

import (
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// ownProcessGroup puts the command in a process group of its own, so a
// terminal's interrupt reaches cw alone and cw stops the whole group.
func ownProcessGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// stopProcessGroup stops a started command whose Wait result arrives on done:
// SIGTERM to its process group, so git can remove its own lock files, then,
// if a member is still alive stopGrace later, SIGKILL to the group until it
// is empty. It returns the command's Wait error.
func stopProcessGroup(command *exec.Cmd, done <-chan error) error {
	pgid := command.Process.Pid
	_ = syscall.Kill(-pgid, syscall.SIGTERM)
	grace := time.NewTimer(stopGrace)
	defer grace.Stop()
	poll := time.NewTicker(20 * time.Millisecond)
	defer poll.Stop()
	var waitErr error
	waited := false
	for {
		select {
		case waitErr = <-done:
			waited = true
		case <-poll.C:
		case <-grace.C:
			killErr := proc.KillGroup(pgid)
			if !waited {
				waitErr = <-done
			}
			return errors.Join(waitErr, killErr)
		}
		if waited && groupGone(pgid) {
			return waitErr
		}
	}
}

// groupGone reports whether process group pgid has no member left.
func groupGone(pgid int) bool {
	return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
}
