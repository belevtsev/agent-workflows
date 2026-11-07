package procgroup

import (
	"bytes"
	"os"
	"strconv"
)

// onlyDead reports whether /proc lists at least one member of group pgid and
// every member is a zombie (or dead). A process that is still exiting counts
// as live. (A zombie thread-group leader can briefly have other threads left,
// with SIGKILL pending; they cannot fork.) Linux delivers a group-wide SIGKILL
// to a child whose fork is in progress and refuses a fork once a fatal signal
// is pending, so after a kill no new live member can appear behind this scan.
// Listing no member at all is not proof: the group may have just emptied (the
// next kill reports ESRCH) or /proc may show another PID namespace. An
// unreadable /proc or stat line counts as a live member.
func onlyDead(pgid int) bool {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	group := []byte(strconv.Itoa(pgid))
	members := 0
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			// The process was reaped after the listing.
			continue
		}
		// pid (comm) state ppid pgrp ...; comm may contain spaces or ')'.
		end := bytes.LastIndexByte(stat, ')')
		if end < 0 {
			return false
		}
		fields := bytes.Fields(stat[end+1:])
		if len(fields) < 3 {
			return false
		}
		if !bytes.Equal(fields[2], group) {
			continue
		}
		members++
		if state := string(fields[0]); state != "Z" && state != "X" {
			return false
		}
	}
	return members > 0
}
