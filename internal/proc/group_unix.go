//go:build linux || darwin

package proc

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

// KillGroup sends SIGKILL to every process in group pgid and repeats it until the
// group has no live member. It returns nil when the kill reports ESRCH (the
// group is empty) or when the group's member list shows only processes that
// can no longer run or fork (onlyDead). Zombies stay in the group until their
// parent reaps them, and a parent that never reaps, such as a container's PID
// 1, must not hold every call for the whole bound. EPERM alone proves nothing:
// macOS returns it while every member is exiting, even when one of them is
// still completing a fork whose child will live, and Linux returns it for a
// live member this process may not signal. KillGroup returns an error when a live
// member remains after the bound.
//
// A further kill is sent only after the previous one found a member (0 or
// EPERM), so the group, and with it the id, was still in use; kernels do not
// hand out an id that is a live process group or session id. A stray signal
// would need the group to empty during one pause and the id to be reallocated
// and made a group leader within it. The first kill after the caller reaped
// the leader carries the same tiny exposure as any kill after a reap.
func KillGroup(pgid int) error {
	if pgid <= 1 || pgid == syscall.Getpgrp() {
		return fmt.Errorf("refusing to kill process group %d", pgid)
	}
	deadline := time.Now().Add(bound)
	pause := firstPause
	for {
		err := syscall.Kill(-pgid, syscall.SIGKILL)
		switch {
		case errors.Is(err, syscall.ESRCH):
			return nil
		case err != nil && !errors.Is(err, syscall.EPERM):
			return fmt.Errorf("kill process group %d: %w", pgid, err)
		}
		// Members killed a moment ago are still exiting; look after a pause.
		time.Sleep(pause)
		pause = min(2*pause, maxPause)
		if onlyDead(pgid) {
			return nil
		}
		if !time.Now().Before(deadline) {
			return fmt.Errorf("process group %d still had a live member after %v of repeated SIGKILL", pgid, bound)
		}
	}
}

const (
	// bound limits how long KillGroup waits for a group to empty after
	// SIGKILL. A killed group normally empties within a few milliseconds;
	// reaching the bound means a member could not be stopped.
	bound = 5 * time.Second
	// firstPause and maxPause space the repeated kills: short at first, as a
	// killed group usually empties in well under a millisecond, then capped.
	firstPause = 100 * time.Microsecond
	maxPause   = 10 * time.Millisecond
)

// ownGroup puts the command in a new process group whose id is its pid, so a
// terminal's interrupt reaches cw alone and Run can stop the whole group.
func ownGroup(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// endGroup kills the started command's process group until no live member
// remains, whether the run was stopped or the command exited.
func endGroup(command *exec.Cmd, _ bool) error {
	return KillGroup(command.Process.Pid)
}
