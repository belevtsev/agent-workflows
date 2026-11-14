//go:build windows

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no process groups and no signals. Run instead puts the program
// in a job object of its own as soon as it has started; the processes it then
// starts join the job too, and ending the job ends them all.
//
// Differences from Linux and macOS:
//   - The program is assigned to the job just after it starts, not before: a
//     process it starts in that instant is not in the job and escapes.
//   - The job is created with JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, so if cw
//     itself dies, Windows ends the run's processes when the handle closes.
//   - A stop (timeout or cancelled ctx) terminates the job at once: there is
//     no SIGTERM, so Spec.Grace is ignored.
//   - A terminated program reports the exit code TerminateJobObject gave it
//     (terminatedCode), not a signal, so Result.Code is 1 rather than -1
//     after a stop.
//   - Spec.Session has no meaning: there is no controlling terminal, and the
//     program shares cw's console.
//   - KillGroup is not available.

// terminatedCode is the exit code of a process that ending the job killed,
// the same that os.Process.Kill gives on Windows.
const terminatedCode = 1

const (
	// bound limits how long ending a job waits for its processes to exit.
	bound = 5 * time.Second
	// firstPause and maxPause space the looks at the job's process count.
	firstPause = 100 * time.Microsecond
	maxPause   = 10 * time.Millisecond
)

// jobs holds the job object of every command between joinGroup and the
// endGroup, stopGroup or leaveGroup that ends its run.
var jobs sync.Map // *exec.Cmd -> windows.Handle

// ownGroup does nothing: a job object can only be joined once the process
// exists (joinGroup).
func ownGroup(*exec.Cmd, bool) {}

// joinGroup puts a started command in a new job object that kills its
// members when its last handle closes. When that fails, it kills and reaps
// the command and returns the error, so no process of the run is left
// outside a job.
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

// newJob creates a kill-on-close job object and assigns process pid to it.
// The pid names the command's process: os.Process holds a handle to it until
// Wait, so the id cannot have been reused.
func newJob(pid int) (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	if err := setKillOnClose(job, true); err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid)) //nolint:gosec // G115: a Windows process id fits in 32 bits.
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	err = windows.AssignProcessToJobObject(job, process)
	_ = windows.CloseHandle(process)
	if err != nil {
		_ = windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

// setKillOnClose sets or clears JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, the
// job's only limit.
func setKillOnClose(job windows.Handle, kill bool) error {
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	if kill {
		info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	}
	_, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))) //nolint:gosec // G103: the Win32 call takes the structure's address and size.
	return err
}

// takeJob removes and returns the command's job object.
func takeJob(command *exec.Cmd) (windows.Handle, bool) {
	job, ok := jobs.LoadAndDelete(command)
	if !ok {
		return 0, false
	}
	return job.(windows.Handle), true //nolint:forcetypeassert // jobs holds only handles.
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

// leaveGroup lets the job's other processes keep running after the command
// exited (Spec.KeepGroupOnExit): it clears the kill-on-close limit and closes
// the handle. Should clearing fail, closing the handle ends those processes.
func leaveGroup(command *exec.Cmd) {
	job, ok := takeJob(command)
	if !ok {
		return
	}
	_ = setKillOnClose(job, false)
	_ = windows.CloseHandle(job)
}

// stopGroup ends the job of a started command whose Wait result arrives on
// done, at once, and returns that result and any cleanup failure. Windows
// has no SIGTERM, so the grace is skipped.
func stopGroup(command *exec.Cmd, _ time.Duration, done <-chan error) (waitErr, cleanup error) {
	job, ok := takeJob(command)
	if !ok {
		_ = command.Process.Kill()
		return <-done, nil
	}
	cleanup = endJob(job)
	return <-done, cleanup
}

// endJob terminates every process in job, waits until the job has no active
// process, or for bound, and closes the handle.
func endJob(job windows.Handle) error {
	defer func() { _ = windows.CloseHandle(job) }()
	if err := windows.TerminateJobObject(job, terminatedCode); err != nil {
		return fmt.Errorf("terminate job object: %w", err)
	}
	deadline := time.Now().Add(bound)
	pause := firstPause
	for {
		active, err := activeProcesses(job)
		if err != nil {
			return fmt.Errorf("query job object: %w", err)
		}
		if active == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("job object still had %d active processes %v after it was terminated", active, bound)
		}
		time.Sleep(pause)
		pause = min(2*pause, maxPause)
	}
}

// jobAccounting is JOBOBJECT_BASIC_ACCOUNTING_INFORMATION, which
// golang.org/x/sys/windows does not define.
type jobAccounting struct {
	TotalUserTime             int64
	TotalKernelTime           int64
	ThisPeriodTotalUserTime   int64
	ThisPeriodTotalKernelTime int64
	TotalPageFaultCount       uint32
	TotalProcesses            uint32
	ActiveProcesses           uint32
	TotalTerminatedProcesses  uint32
}

// activeProcesses returns how many processes in job have not exited.
func activeProcesses(job windows.Handle) (uint32, error) {
	var info jobAccounting
	err := windows.QueryInformationJobObject(job, windows.JobObjectBasicAccountingInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), nil) //nolint:gosec // G103: the Win32 call takes the structure's address and size.
	return info.ActiveProcesses, err
}

// KillGroup is unavailable on Windows, which has no process groups; Run ends
// a run's processes through its job object instead.
func KillGroup(int) error { return errors.ErrUnsupported }
