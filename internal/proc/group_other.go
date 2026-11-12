//go:build !linux && !darwin

package proc

import (
	"errors"
	"os/exec"
	"time"
)

func ownGroup(*exec.Cmd, bool) {}

// endGroup has nothing to kill: without process groups nothing else is known
// to belong to the run.
func endGroup(*exec.Cmd) error { return nil }

// stopGroup kills the command itself, at once, and returns its Wait result.
func stopGroup(command *exec.Cmd, _ time.Duration, done <-chan error) (waitErr, cleanup error) {
	_ = command.Process.Kill()
	return <-done, nil
}

// KillGroup is unavailable without process groups.
func KillGroup(int) error { return errors.ErrUnsupported }
