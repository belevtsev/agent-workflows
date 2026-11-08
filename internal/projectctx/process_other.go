//go:build !unix

package projectctx

import "os/exec"

func configureProbeProcess(_ *exec.Cmd) {
	// CommandContext owns and reaps the direct subprocess. Fixed doctor probes
	// do not start background services; Windows process trees are not probed.
}

// killProbeProcess kills the direct subprocess; Windows process trees are
// not probed.
func killProbeProcess(command *exec.Cmd) error {
	return command.Process.Kill()
}

func cleanupProbeProcess(_ *exec.Cmd) {}
