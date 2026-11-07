//go:build unix

package procgroup

import (
	"errors"
	"fmt"
	"syscall"
	"time"
)

// Kill sends SIGKILL to every process in group pgid and repeats it until the
// group has no live member. It returns nil when the kill reports ESRCH (the
// group is empty) or when the group's member list shows only processes that
// can no longer run or fork (onlyDead). Zombies stay in the group until their
// parent reaps them, and a parent that never reaps, such as a container's PID
// 1, must not hold every call for the whole bound. EPERM alone proves nothing:
// macOS returns it while every member is exiting, even when one of them is
// still completing a fork whose child will live, and Linux returns it for a
// live member this process may not signal. Kill returns an error when a live
// member remains after the bound.
//
// A further kill is sent only after the previous one found a member (0 or
// EPERM), so the group, and with it the id, was still in use; kernels do not
// hand out an id that is a live process group or session id. A stray signal
// would need the group to empty during one pause and the id to be reallocated
// and made a group leader within it. The first kill after the caller reaped
// the leader carries the same tiny exposure as any kill after a reap.
func Kill(pgid int) error {
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
