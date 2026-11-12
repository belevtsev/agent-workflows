//go:build unix

package taskflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// pgGroupHasLiveMember reports whether process group pgid has a member that
// is not a zombie. It only ever sends signal 0, and only to a group led by a
// process this test started. kill(-pgid, 0) failing with ESRCH (no member) or
// EPERM (macOS: only zombies remain) means no live member. On Linux a zombie
// still accepts signal 0, so a success there is checked against /proc.
func pgGroupHasLiveMember(pgid int) (bool, error) {
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
		return pgLinuxGroupHasNonZombie(pgid)
	}
	return true, nil
}

// pgLinuxGroupHasNonZombie scans /proc for a member of pgid whose state is not
// zombie (Z) or dead (X).
func pgLinuxGroupHasNonZombie(pgid int) (bool, error) {
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

// pgLeaderGroupLive reads the pid the storm leader recorded and reports
// whether its process group still has a live member. The check runs in its
// own process group, which a process creates with its own pid as the id.
func pgLeaderGroupLive(pidFile string) (bool, error) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false, err
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false, err
	}
	return pgGroupHasLiveMember(pgid)
}

// pgRequireStormDescendantScript proves the storm descendant reports a
// survivor whose forker is gone once RunCheck has returned, and stays silent while
// its forker is alive.
func pgRequireStormDescendantScript(t *testing.T, root string) {
	t.Helper()
	gone := exec.Command("/bin/sh", "-c", "exit 0")
	if err := gone.Run(); err != nil {
		t.Fatal(err)
	}
	returned := filepath.Join(root, "control-returned")
	if err := os.WriteFile(returned, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	survivor, owned := filepath.Join(root, "control-survivor"), filepath.Join(root, "control-owned")
	orphan := exec.Command("/bin/sh", "-c", pgStormDescendantScript, "sh", strconv.Itoa(gone.ProcessState.Pid()), returned, survivor)
	live := exec.Command("/bin/sh", "-c", pgStormDescendantScript, "sh", strconv.Itoa(os.Getpid()), returned, owned)
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range []*exec.Cmd{orphan, live} {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(survivor); err != nil {
		t.Fatalf("storm descendant cannot report a survivor: %v", err)
	}
	if _, err := os.Stat(owned); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("storm descendant reported a survivor while its forker was alive: %v", err)
	}
}

// TestNormalCheckExitKillsForkingBackgroundGroupUntilEmpty (TF-1) runs a
// check that starts one redirected storm forker in its process group, waits
// until it is forking and exits 0. On macOS one group kill after the command
// finishes can miss a child being forked at that moment. The forker matches
// the measured graphify fork-storm (64 descendants that live 0.2 s, ready
// after 8). Measured with that forker against a single kill after a normal
// exit, 7, 9 and 10 of 40 trials left a survivor; one Go forker has at most
// one fork in flight, so each survivor marks a separate trial. At the lowest
// measured rate (17.5%) a single-kill cleanup survives all 36 trials with
// probability 0.825^36 < 0.1%; if the check runner's own timing halved that
// rate (8.75%), 0.9125^36 < 4%. Each trial starts a fresh task and runs the
// check once; with Start, RunCheck and a helper leader and forker per trial the
// test is sized for about 0.3-0.8 s per trial (11-29 s) plus the observation.
// The check, right after each return, that no member of the check's group is
// alive only adds detection.
func TestNormalCheckExitKillsForkingBackgroundGroupUntilEmpty(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("owned process-group cleanup is available on native Linux and macOS builds")
	}
	const trials, observation = 36, 1500 * time.Millisecond
	root := t.TempDir()
	pgRequireStormDescendantScript(t, root)
	survivors := filepath.Join(root, "survivors")
	if err := os.Mkdir(survivors, 0o700); err != nil {
		t.Fatal(err)
	}
	for trial := range trials {
		directory := filepath.Join(root, fmt.Sprintf("trial-%d", trial))
		if err := os.Mkdir(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		record, _ := fixtureTask(t, helperPlan(t, "small", "storm-leader", directory, survivors))
		run, err := RunCheck(t.Context(), record.Directory, "focused")
		live, probeErr := pgLeaderGroupLive(filepath.Join(directory, "leader-pid"))
		// From here on, any descendant of this trial still running outlived RunCheck.
		if writeErr := os.WriteFile(filepath.Join(directory, "returned"), nil, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
		if err != nil || run.Status != "passed" {
			t.Fatalf("trial %d: forking check: %+v %v", trial, run, err)
		}
		if _, err := os.Stat(filepath.Join(directory, "running")); err != nil {
			t.Fatalf("trial %d: forker never reported running: %v", trial, err)
		}
		if probeErr != nil {
			t.Fatalf("trial %d: %v", trial, probeErr)
		}
		if live {
			t.Fatalf("trial %d: a member of the check's process group was alive after RunCheck returned", trial)
		}
	}
	// A survivor reports within about 0.3 s of its trial's return marker.
	time.Sleep(observation)
	entries, err := os.ReadDir(survivors)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("%d storm descendants outlived a passed check: %v", len(names), names)
	}
}
