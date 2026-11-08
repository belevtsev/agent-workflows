//go:build !linux && !darwin

package publish

import "os/exec"

func ownProcessGroup(*exec.Cmd) {}

// stopProcessGroup kills the command where process groups are unavailable.
func stopProcessGroup(command *exec.Cmd, done <-chan error) error {
	_ = command.Process.Kill()
	return <-done
}
