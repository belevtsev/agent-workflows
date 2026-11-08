//go:build linux

package main

import (
	"bytes"
	"os"
	"strconv"
	"strings"
	"sync"
)

// processGroupRunning reports whether a process other than a zombie is in the
// process group. The caller has just seen kill(-group, 0) succeed, so the group
// has a member. The answer comes from /proc only when /proc shows every
// process of this pid namespace; otherwise, and whenever /proc shows no member
// of the group at all, the group counts as running.
func processGroupRunning(group int) bool {
	if !procShowsAll() {
		return true
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return true
	}
	seen := false
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			continue // the process exited while the directory was read
		}
		state, pgrp, ok := parseProcStat(stat)
		if !ok || pgrp != group {
			continue
		}
		seen = true
		if state != 'Z' && state != 'X' {
			return true
		}
		// A leader that exited while its other threads run also shows Z.
		if tasks, err := os.ReadDir("/proc/" + entry.Name() + "/task"); err == nil {
			for _, task := range tasks {
				if task.Name() != strconv.Itoa(pid) {
					return true
				}
			}
		}
	}
	// kill found a member that /proc does not show, so it cannot be judged.
	return !seen
}

// procShowsAll reports whether /proc belongs to this process's pid namespace
// and is mounted without hidepid, so that it lists every process of the
// namespace. It is computed once.
var procShowsAll = sync.OnceValue(func() bool {
	self, err := os.Readlink("/proc/self")
	if err != nil || self != strconv.Itoa(os.Getpid()) {
		return false
	}
	mountinfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	return procMountShowsAll(string(mountinfo))
})

// procMountShowsAll reports whether the mount on top of /proc in a mountinfo
// listing is a proc mount whose hidepid option, if any, does not hide
// processes. Each line is
// "ID PARENT MAJ:MIN ROOT MOUNTPOINT OPTIONS [OPTIONAL...] - FSTYPE SOURCE SUPEROPTIONS".
// Mounts at /proc can be stacked, and mountinfo lists them in creation order,
// not stacking order, so the order of the lines decides nothing: a mount at
// /proc is a top when no other mount at /proc has it as its PARENT, and only a
// top decides. The answer is true only when there is exactly one top, of type
// proc, without a hiding hidepid; no mount at /proc, several tops, another
// filesystem type on top or a hiding top all give false. A mount is at /proc
// whatever its type when its MOUNTPOINT is exactly /proc; malformed lines and
// other mount points are ignored.
func procMountShowsAll(mountinfo string) bool {
	var atProc []procMountLine
	for _, line := range strings.Split(mountinfo, "\n") {
		if mount, ok := parseProcMountLine(line); ok {
			atProc = append(atProc, mount)
		}
	}
	var top procMountLine
	tops := 0
	for i, mount := range atProc {
		covered := false
		for j, other := range atProc {
			if j != i && other.parent == mount.id {
				covered = true
				break
			}
		}
		if !covered {
			top = mount
			tops++
		}
	}
	if tops != 1 || top.fstype != "proc" {
		return false
	}
	for _, option := range strings.Split(top.superoptions, ",") {
		if value, ok := strings.CutPrefix(option, "hidepid="); ok && value != "0" && value != "off" {
			return false
		}
	}
	return true
}

// procMountLine holds the fields of a mountinfo line that procMountShowsAll
// reads.
type procMountLine struct {
	id, parent, fstype, superoptions string
}

// parseProcMountLine returns the fields of a mountinfo line whose MOUNTPOINT
// is exactly /proc. It reports false for any other line, and for a malformed
// one: no " - " separator, fewer than 5 fields before it or fewer than 3 after.
func parseProcMountLine(line string) (procMountLine, bool) {
	before, after, ok := strings.Cut(line, " - ")
	fields, tail := strings.Fields(before), strings.Fields(after)
	if !ok || len(fields) < 5 || len(tail) < 3 || fields[4] != "/proc" {
		return procMountLine{}, false
	}
	return procMountLine{id: fields[0], parent: fields[1], fstype: tail[0], superoptions: tail[2]}, true
}

// parseProcStat returns the state and process group from the contents of
// /proc/PID/stat: "PID (COMM) STATE PPID PGRP ...". COMM may itself contain
// spaces and parentheses, so the fields are read after the last ')'.
func parseProcStat(stat []byte) (byte, int, bool) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, 0, false
	}
	fields := bytes.Fields(stat[end+1:])
	if len(fields) < 3 || len(fields[0]) != 1 {
		return 0, 0, false
	}
	pgrp, err := strconv.Atoi(string(fields[2]))
	if err != nil {
		return 0, 0, false
	}
	return fields[0][0], pgrp, true
}
