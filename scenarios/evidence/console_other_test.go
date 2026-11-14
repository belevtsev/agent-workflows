//go:build !windows

package evidence_test

import (
	"errors"
	"os/exec"
)

// newConsoleGroup is used only on Windows.
func newConsoleGroup(*exec.Cmd) {}

// sendCtrlBreak is used only on Windows.
func sendCtrlBreak(int) error { return errors.New("Ctrl+Break exists only on Windows") }
