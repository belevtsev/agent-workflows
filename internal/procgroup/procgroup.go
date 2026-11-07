// Package procgroup stops every process in an owned process group.
//
// One kill(-pgid, SIGKILL) is not enough on macOS: a member that is forking
// when the signal is sent can produce a child that joins the group after the
// kernel has walked it, and that child survives. Linux redirects or restarts
// such a fork, but the same loop is correct there too.
package procgroup

import "time"

const (
	// bound limits how long Kill waits for a group to empty after SIGKILL. A
	// killed group normally empties within a few milliseconds; reaching the
	// bound means a member could not be stopped.
	bound = 5 * time.Second
	// firstPause and maxPause space the repeated kills: short at first, as a
	// killed group usually empties in well under a millisecond, then capped.
	firstPause = 100 * time.Microsecond
	maxPause   = 10 * time.Millisecond
)
