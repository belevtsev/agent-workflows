package evidence_test

import (
	"os/exec"
	"syscall"
)

// newConsoleGroup starts the program in a process group of its own, so that
// a Ctrl+Break sent to that group reaches only it.
func newConsoleGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// sendCtrlBreak sends Ctrl+Break to the process group that pid leads.
func sendCtrlBreak(pid int) error {
	const ctrlBreakEvent = 1
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GenerateConsoleCtrlEvent")
	if ok, _, err := proc.Call(ctrlBreakEvent, uintptr(pid)); ok == 0 {
		return err
	}
	return nil
}
