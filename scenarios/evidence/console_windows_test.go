package evidence_test

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"
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

// watchProcess opens the process with the id pid when it runs a program
// whose file is named image. found says whether it did; running then says, at
// any later time, whether that process still runs, and the test's cleanup
// ends it if it does.
//
// A test never ends or reports a process by its id alone. Windows gives the
// id of a process that has ended to the next process that starts, and next
// to a test that is some other test's git. The handle taken here keeps the
// id from being given away, and the name of the program tells a process of
// the check from one that got the id later.
func watchProcess(t *testing.T, pid, image string) (running func() bool, found bool) {
	t.Helper()
	gone := func() bool { return false }
	id, err := strconv.ParseUint(pid, 10, 32)
	if err != nil {
		t.Fatalf("process id %q: %v", pid, err)
	}
	const queryLimitedInformation = 0x1000
	handle, err := syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_TERMINATE|queryLimitedInformation, false, uint32(id))
	if err != nil {
		return gone, false // no process has the id, or one that this user may not open
	}
	name := imageName(handle)
	if !strings.EqualFold(name[strings.LastIndexAny(name, `\/`)+1:], image) {
		_ = syscall.CloseHandle(handle)
		return gone, false // the process has ended, or the id went to another program
	}
	running = func() bool {
		event, _ := syscall.WaitForSingleObject(handle, 0)
		return event == syscall.WAIT_TIMEOUT
	}
	t.Cleanup(func() {
		if running() {
			_ = syscall.TerminateProcess(handle, 1)
		}
		_ = syscall.CloseHandle(handle)
	})
	return running, true
}

// imageName is the path of the program that the process runs, or "" when
// Windows does not tell, as for a process that has ended.
func imageName(handle syscall.Handle) string {
	buffer := make([]uint16, 32768)
	size := uint32(len(buffer))
	query := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	if ok, _, _ := query.Call(uintptr(handle), 0, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&size))); ok == 0 { //nolint:gosec // G103: the Win32 call takes the addresses of the buffer and of its size.
		return ""
	}
	return syscall.UTF16ToString(buffer[:size])
}

// watchProcess is what two scenarios rest on, so it is tried on a process
// the test starts itself: it must find a running program by its name, must
// not take it for another program, and must see that it has ended.
func TestWatchProcessTellsARunningProgramFromAnEndedOne(t *testing.T) {
	t.Parallel()
	ping := exec.Command("ping", "-n", "60", "127.0.0.1")
	if err := ping.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ping.Process.Kill(); _ = ping.Wait() })
	pid := strconv.Itoa(ping.Process.Pid)

	if _, found := watchProcess(t, pid, "another-program.exe"); found {
		t.Errorf("process %s runs ping, and was taken for another-program.exe", pid)
	}
	running, found := watchProcess(t, pid, "ping.exe")
	if !found {
		t.Fatalf("process %s runs ping.exe and was not found", pid)
	}
	if !running() {
		t.Errorf("process %s runs and is reported as ended", pid)
	}
	if err := ping.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = ping.Wait()
	if running() {
		t.Errorf("process %s has ended and is reported as running", pid)
	}
}
