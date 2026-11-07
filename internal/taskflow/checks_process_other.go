//go:build !linux && !darwin

package taskflow

import "os/exec"

func configureCheckProcess(command *exec.Cmd)     {}
func cleanupCheckProcess(command *exec.Cmd) error { return nil }
