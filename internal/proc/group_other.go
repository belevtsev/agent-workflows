//go:build !linux && !darwin && !windows

package proc

import (
	"errors"
	"os/exec"
)

func ownGroup(*exec.Cmd) {}

// joinGroup does nothing: there is no group to join.
func joinGroup(*exec.Cmd) error { return nil }

// endGroup has nothing to kill: without process groups nothing else is known
// to belong to the run.
func endGroup(*exec.Cmd) error { return nil }

// stopGroup kills the command itself, at once, and returns its Wait result.
func stopGroup(command *exec.Cmd, done <-chan error) (waitErr, cleanup error) {
	_ = command.Process.Kill()
	return <-done, nil
}

// KillGroup is unavailable without process groups.
func KillGroup(int) error { return errors.ErrUnsupported }
