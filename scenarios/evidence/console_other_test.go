//go:build !windows

package evidence_test

import (
	"errors"
	"os/exec"
	"testing"
)

// newConsoleGroup is used only on Windows.
func newConsoleGroup(*exec.Cmd) {}

// sendCtrlBreak is used only on Windows.
func sendCtrlBreak(int) error { return errors.New("Ctrl+Break exists only on Windows") }

// watchProcess is used only on Windows.
func watchProcess(*testing.T, string, string) (running func() bool, found bool) {
	return func() bool { return false }, false
}
