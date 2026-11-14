//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no process groups and no signals. Run instead starts the program
// suspended, puts it in a job object of its own and lets it continue; the
// processes it then starts join the job too, and ending the job ends them all.
//
// Differences from Linux and macOS:
//   - The job is created with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so if cw
//     itself dies, Windows ends the run's processes when the handle closes.
//   - A stop (timeout or cancelled ctx) terminates the job.
//   - A terminated program reports the exit code TerminateJobObject gave it
//     (terminatedCode), not a signal, so Result.Code is 1 rather than -1
//     after a stop.
//   - KillGroup is not available.

// terminatedCode is the exit code of a process that ending the job killed,
// the same that os.Process.Kill gives on Windows.
const terminatedCode = 1

// bound limits how long ending a job waits for its processes to exit.
const bound = 5 * time.Second

// jobs holds the job object of every command between joinGroup and the
// endGroup or stopGroup that ends its run.
var jobs sync.Map // *exec.Cmd -> windows.Handle

// ntResumeProcess continues a suspended process. Go keeps no handle to the first thread, which ResumeThread needs.
// A call that passes the address of a structure goes through Call of kernel32: Call keeps the structure in place.
var ntResumeProcess, kernel32 = windows.NewLazySystemDLL("ntdll.dll").NewProc("NtResumeProcess"), windows.NewLazySystemDLL("kernel32.dll")

// ownGroup makes the program start suspended; joinGroup lets it run once it is in its job object.
// It also starts it in a console process group of its own, so that Ctrl+C in cw's console reaches cw alone.
func ownGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_SUSPENDED | windows.CREATE_NEW_PROCESS_GROUP}
}

// joinGroup puts a started command in a new job object that kills its members when its last handle closes,
// and lets it run. When that fails, it kills and reaps the command and returns the error, so no process of
// the run is left outside a job.
func joinGroup(command *exec.Cmd) error {
	job, err := newJob(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		return fmt.Errorf("put process %d in a job object: %w", command.Process.Pid, err)
	}
	jobs.Store(command, job)
	return nil
}

// newJob creates a kill-on-close job object, assigns the suspended process pid to it and continues it.
// The pid names the command's process: os.Process holds a handle to it until Wait, so the id cannot have been reused.
func newJob(pid int) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	if err := setKillOnClose(job); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_SUSPEND_RESUME, false, uint32(pid)) //nolint:gosec // G115: a Windows process id fits in 32 bits.
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	if err = windows.AssignProcessToJobObject(job, process); err == nil {
		if status, _, _ := ntResumeProcess.Call(uintptr(process)); status != 0 {
			err = windows.NTStatus(status) //nolint:gosec // G115: an NTSTATUS has 32 bits.
		}
	}
	_ = windows.CloseHandle(process)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// setKillOnClose sets JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, the job's only limit.
func setKillOnClose(job windows.Handle) error {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if ok, _, err := kernel32.NewProc("SetInformationJobObject").Call(uintptr(job), windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info)); ok == 0 { //nolint:gosec // G103: the Win32 call takes the structure's address and size.
		return err
	}
	return nil
}

// takeJob removes and returns the command's job object.
func takeJob(command *exec.Cmd) (windows.Handle, bool) {
	job, _ := jobs.LoadAndDelete(command)
	handle, ok := job.(windows.Handle)
	return handle, ok
}

// endGroup ends every process left in the job of a command that has exited
// and waits until none is alive.
func endGroup(command *exec.Cmd) error {
	job, ok := takeJob(command)
	if !ok {
		return nil
	}
	return endJob(job)
}

// stopGroup ends the job of a started command whose Wait result arrives on
// done, and returns that result and any cleanup failure.
func stopGroup(command *exec.Cmd, done <-chan error) (waitErr, cleanup error) {
	job, ok := takeJob(command)
	if !ok {
		_ = command.Process.Kill()
		return <-done, nil
	}
	cleanup = endJob(job)
	return <-done, cleanup
}

// endJob terminates every process in job, waits until each has gone, or for
// bound, and closes the handle. It opens the processes first: once the job is
// terminated it counts them as gone, while they are still ending.
func endJob(job windows.Handle) error {
	members := jobMembers(job)
	defer func() {
		for _, handle := range append(members, job) {
			_ = windows.CloseHandle(handle)
		}
	}()
	if err := windows.TerminateJobObject(job, terminatedCode); err != nil {
		return fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(bound)
	for _, process := range members {
		left := uint32(max(0, time.Until(deadline).Milliseconds())) //nolint:gosec // G115: no more than bound.
		if event, _ := windows.WaitForSingleObject(process, left); event != windows.WAIT_OBJECT_0 {
			return fmt.Errorf("a process of the job was still running %v after the job was terminated", bound)
		}
	}
	return nil
}

// jobMembers opens the processes of job, so that endJob can wait for each; of more than 1024 the rest is not waited for.
func jobMembers(job windows.Handle) []windows.Handle {
	var list struct {
		assigned, listed uint32
		ids              [1024]uintptr
	}
	_, _, _ = kernel32.NewProc("QueryInformationJobObject").Call(uintptr(job), windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0) //nolint:gosec // G103: the Win32 call takes the structure's address and size.
	var members []windows.Handle
	for _, id := range list.ids[:min(int(list.listed), len(list.ids))] {
		if process, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(id)); err == nil { //nolint:gosec // G115: a Windows process id fits in 32 bits.
			members = append(members, process)
		}
	}
	return members
}

// KillGroup is unavailable on Windows, which has no process groups; Run ends
// a run's processes through its job object instead.
func KillGroup(int) error { return errors.ErrUnsupported }
