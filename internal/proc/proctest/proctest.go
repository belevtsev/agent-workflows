//go:build linux || darwin

// Package proctest lets tests observe a process group that a test started.
package proctest

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// GroupHasLiveMember reports whether process group pgid has a member that is
// not a zombie. It only ever sends signal 0, and only to a group led by a
// process the test started. kill(-pgid, 0) failing with ESRCH (no member) or
// EPERM (macOS: only zombies remain) means no live member. On Linux a zombie
// still accepts signal 0, so a success there is checked against /proc.
func GroupHasLiveMember(pgid int) (bool, error) {
	if pgid <= 1 || pgid == os.Getpid() || pgid == syscall.Getpgrp() {
		return false, fmt.Errorf("refusing to probe process group %d", pgid)
	}
	err := syscall.Kill(-pgid, 0)
	switch {
	case errors.Is(err, syscall.ESRCH), errors.Is(err, syscall.EPERM):
		return false, nil
	case err != nil:
		return false, err
	case runtime.GOOS == "linux":
		return linuxGroupHasNonZombie(pgid)
	}
	return true, nil
}

// linuxGroupHasNonZombie scans /proc for a member of pgid whose state is not
// zombie (Z) or dead (X).
func linuxGroupHasNonZombie(pgid int) (bool, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	want := strconv.Itoa(pgid)
	for _, entry := range entries {
		if _, err := strconv.Atoi(entry.Name()); err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		stat := string(data)
		end := strings.LastIndexByte(stat, ')')
		if end < 0 {
			continue
		}
		// After the command name: state, parent pid, process group.
		fields := strings.Fields(stat[end+1:])
		if len(fields) < 3 || fields[2] != want {
			continue
		}
		if fields[0] != "Z" && fields[0] != "X" {
			return true, nil
		}
	}
	return false, nil
}
