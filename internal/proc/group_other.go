//go:build !linux && !darwin

package proc

import (
	"errors"
	"os/exec"
)

func ownGroup(*exec.Cmd) {}

// endGroup kills the command itself when the run is stopped. Without process
// groups nothing else is known to belong to the run.
func endGroup(command *exec.Cmd, stopped bool) error {
	if stopped {
		_ = command.Process.Kill()
	}
	return nil
}

// KillGroup is unavailable without process groups.
func KillGroup(int) error { return errors.ErrUnsupported }
