//go:build !unix

package graphify

import "os/exec"

func supported() error {
	return refuse("platform", "The guarded Graphify runner currently supports macOS and Linux")
}

func isolate(*exec.Cmd)         {}
func killGroup(*exec.Cmd)       {}
func stopGroup(*exec.Cmd) error { return nil }
