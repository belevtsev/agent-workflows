//go:build !unix

package main

import (
	"os/exec"
	"time"
)

// ownProcessGroup is a no-op where process groups are unavailable;
// diagram-svg refuses to run on such systems.
func ownProcessGroup(*exec.Cmd) {}

// killProcessGroup kills only the process itself.
func killProcessGroup(command *exec.Cmd) error {
	if command.Process == nil {
		return nil
	}
	return command.Process.Kill()
}

// waitProcessGroupGone has no group to wait for.
func waitProcessGroupGone(*exec.Cmd, time.Duration) {}
