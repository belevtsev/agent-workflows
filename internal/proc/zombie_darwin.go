package proc

import "golang.org/x/sys/unix"

// sZomb is the macOS p_stat value of a zombie.
const sZomb = 5

// onlyDead reports whether the kernel lists at least one member of group pgid
// and every member is a zombie. A member that is exiting is not enough: it was
// seen (state SRUN after SIGKILL) to complete a fork whose child then lived.
// The rule relies on XNU behaviour observed here and reasoned from its design,
// not on a documented guarantee: a forking process is listed as non-zombie
// until its fork call returns, and by then the child is in the group. Listing
// no member is not proof either; the next kill reports ESRCH once the group is
// empty.
func onlyDead(pgid int) bool {
	procs, err := unix.SysctlKinfoProcSlice("kern.proc.pgrp", pgid)
	if err != nil || len(procs) == 0 {
		return false
	}
	for _, proc := range procs {
		if proc.Eproc.Pgid != int32(pgid) || proc.Proc.P_stat != sZomb { //nolint:gosec // G115: KillGroup just signalled this live group, so pgid fits a Darwin pid_t (int32).
			return false
		}
	}
	return true
}
