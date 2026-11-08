//go:build unix

package main

// Direct tests of the R8 process-group helpers (contract diagram-svg-v5):
// ownProcessGroup, killProcessGroup and waitProcessGroupGone. These tests,
// not the end-to-end TMPDIR test, guard the wait for a killed group.
//
// The processes in the groups are this test binary started with pgChildEnv
// set; init runs that mode and exits before TestMain. A "leader" child
// starts a "member" child that inherits its process group, so a group can
// hold a running member that is not its leader.
//
// Contract diagram-svg-v6 limits the zombie rule to a full /proc (this
// process's own pid namespace and no hidepid option that hides processes),
// so the zombie test skips elsewhere. It adds the rule that a zombie leader
// with live threads still counts as running: a "threadexit" child ends only
// its main thread. And it adds the rule that a member /proc cannot show
// still counts as running; that is tested only where the test may build a
// /proc from another pid namespace (root, with util-linux unshare and
// mount), and skips elsewhere. Contract diagram-svg-v8 (R9) makes the
// hidepid gate the top of the /proc stack: only a line at /proc that no
// other line at /proc covers decides, and there must be exactly one, a
// plain proc mount. The zombie test's own mountinfo reading follows that
// rule, and proc_mount_linux_test.go tests procMountShowsAll itself and
// checks this reading against the same table.

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

const (
	// pgChildEnv selects the child mode of the test binary: "sleep" (run
	// for a minute), "exit" (exit at once), "sleepfor" (sleep for
	// pgSleepForEnv, record the time in pgExitRecordEnv, exit) or "leader"
	// (start a member in mode pgMemberModeEnv, print its pid, then exit, or
	// with pgLeaderStayEnv=1 run for a minute). The v6 modes are
	// "threadexit" (Linux: end the main thread and keep the other threads
	// running for a minute) and "foreignproc" (the waiter of the
	// foreign-/proc test, pgForeignProcWaiter).
	pgChildEnv      = "CWDEV_PG_CHILD"
	pgMemberModeEnv = "CWDEV_PG_MEMBER_MODE"
	pgLeaderStayEnv = "CWDEV_PG_LEADER_STAY"
	pgSleepForEnv   = "CWDEV_PG_SLEEP_FOR"
	pgExitRecordEnv = "CWDEV_PG_EXIT_RECORD"

	// pgGoneBound is how soon waitProcessGroupGone must return once the
	// group is gone. R8 says about 0.5 s; the rest is slack for a loaded
	// -race runner. A call that waits to pgLongLimit exceeds it by far.
	pgGoneBound = 3 * time.Second
	// pgLongLimit is the limit passed when the group is (or becomes) gone.
	pgLongLimit = 20 * time.Second
	// pgLiveLimit and pgLiveTolerance: while a member runs, the call lasts
	// at least pgLiveLimit minus the scheduling tolerance R8 allows.
	pgLiveLimit     = 2 * time.Second
	pgLiveTolerance = 200 * time.Millisecond
	// pgReturnSlack is how long after its limit the test still waits for
	// the call before it gives up on it.
	pgReturnSlack = 10 * time.Second
	// pgSetupBound bounds every wait for the test's own setup.
	pgSetupBound = 20 * time.Second
)

func init() {
	if mode := os.Getenv(pgChildEnv); mode != "" {
		os.Exit(pgChild(mode))
	}
}

// pgChild is the body of a child process; it returns the exit code.
func pgChild(mode string) int {
	switch mode {
	case "sleep":
		time.Sleep(60 * time.Second)
		return 0
	case "exit":
		return 0
	case "sleepfor":
		duration, err := time.ParseDuration(os.Getenv(pgSleepForEnv))
		if err != nil {
			fmt.Fprintln(os.Stderr, "pg child:", err)
			return 91
		}
		time.Sleep(duration)
		record := os.Getenv(pgExitRecordEnv)
		// Written under another name and renamed, so a reader never sees
		// a partial record.
		if err := os.WriteFile(record+".tmp", []byte(strconv.FormatInt(time.Now().UnixNano(), 10)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "pg child:", err)
			return 92
		}
		if err := os.Rename(record+".tmp", record); err != nil {
			fmt.Fprintln(os.Stderr, "pg child:", err)
			return 92
		}
		return 0
	case "threadexit":
		// R8 v6, zombie leader with live threads. init runs on the main
		// goroutine, which the runtime locks to the main OS thread during
		// initialization, and LockOSThread keeps it there. The exit
		// system call (exit, not exit_group) then ends that thread alone,
		// so the leader shows Z in /proc/PID/stat while the goroutine
		// below keeps the process's other threads running, for a minute
		// at most.
		if runtime.GOOS != "linux" {
			fmt.Fprintln(os.Stderr, "pg child: threadexit needs Linux")
			return 94
		}
		runtime.LockOSThread()
		go func() {
			time.Sleep(60 * time.Second)
			os.Exit(0)
		}()
		syscall.Syscall(syscall.SYS_EXIT, 0, 0, 0)
		fmt.Fprintln(os.Stderr, "pg child: the main thread did not exit")
		return 95
	case "foreignproc":
		return pgForeignProcWaiter()
	case "leader":
		executable, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, "pg leader:", err)
			return 93
		}
		// No SysProcAttr: the member stays in the leader's process group.
		// Its standard streams are /dev/null, so it holds no pipe of the
		// leader's.
		member := exec.Command(executable)
		member.Env = append(os.Environ(), pgChildEnv+"="+os.Getenv(pgMemberModeEnv))
		if err := member.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "pg leader:", err)
			return 93
		}
		fmt.Println(member.Process.Pid)
		if os.Getenv(pgLeaderStayEnv) == "1" {
			time.Sleep(60 * time.Second)
		}
		return 0
	}
	fmt.Fprintf(os.Stderr, "pg child: unknown mode %q\n", mode)
	return 90
}

// pgCommand returns the test binary in child mode, made the leader of a new
// process group by ownProcessGroup (called before Start, as R8 requires).
func pgCommand(t *testing.T, mode string, env ...string) *exec.Cmd {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable)
	command.Env = append(append(os.Environ(), pgChildEnv+"="+mode), env...)
	ownProcessGroup(command)
	return command
}

// pgCleanupGroup kills, when the test ends, the group led by the started
// command and every pid in members, and reaps the command if nothing did.
func pgCleanupGroup(t *testing.T, command *exec.Cmd, members ...int) {
	t.Helper()
	leader := command.Process.Pid
	t.Cleanup(func() {
		for _, pid := range members {
			if dsvgProcessAlive(pid) {
				syscall.Kill(pid, syscall.SIGKILL)
			}
		}
		syscall.Kill(-leader, syscall.SIGKILL)
		syscall.Kill(leader, syscall.SIGKILL)
		if command.ProcessState == nil {
			command.Wait()
		}
	})
}

// pgParsePid returns the pid a leader printed.
func pgParsePid(t *testing.T, line []byte) int {
	t.Helper()
	pid, err := strconv.Atoi(strings.TrimSpace(string(line)))
	if err != nil || pid <= 0 {
		t.Fatalf("the leader printed %q, not a member pid", line)
	}
	return pid
}

// pgStartStayingLeader starts a leader that keeps running and a member in
// its group that runs for a minute, and returns the member's pid.
func pgStartStayingLeader(t *testing.T) (*exec.Cmd, int) {
	t.Helper()
	command := pgCommand(t, "leader", pgMemberModeEnv+"=sleep", pgLeaderStayEnv+"=1")
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pgCleanupGroup(t, command)
	// The leader prints the pid or exits; either ends the read. The second
	// cleanup below adds the member.
	line, err := bufio.NewReader(stdout).ReadBytes('\n')
	if err != nil {
		t.Fatalf("read the member pid: %v", err)
	}
	member := pgParsePid(t, line)
	pgCleanupGroup(t, command, member)
	return command, member
}

// pgRunExitedLeader runs a leader that starts a member in mode memberMode
// and exits, waits for the leader (so it is reaped), and returns the
// member's pid. Only the member is then left in the group.
func pgRunExitedLeader(t *testing.T, memberMode string, env ...string) (*exec.Cmd, int) {
	t.Helper()
	command := pgCommand(t, "leader", append([]string{pgMemberModeEnv + "=" + memberMode}, env...)...)
	output, err := command.Output()
	if err != nil {
		var stderr []byte
		if exit, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = exit.Stderr
		}
		t.Fatalf("leader: %v\n%s", err, stderr)
	}
	member := pgParsePid(t, output)
	pgCleanupGroup(t, command, member)
	return command, member
}

// pgAwait polls condition until it holds or pgSetupBound passes.
func pgAwait(condition func() bool) bool {
	for deadline := time.Now().Add(pgSetupBound); ; time.Sleep(20 * time.Millisecond) {
		if condition() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
	}
}

// pgTimedWait calls waitProcessGroupGone(command, limit) and returns how
// long it took and when it returned. If the call has not returned
// pgReturnSlack after its limit, the test fails (R8: it returns once limit
// has passed) and the group is killed so the call can end.
func pgTimedWait(t *testing.T, command *exec.Cmd, limit time.Duration) (time.Duration, time.Time) {
	t.Helper()
	done := make(chan time.Time, 1)
	started := time.Now()
	go func() {
		waitProcessGroupGone(command, limit)
		done <- time.Now()
	}()
	select {
	case returned := <-done:
		return returned.Sub(started), returned
	case <-time.After(limit + pgReturnSlack):
	}
	t.Errorf("waitProcessGroupGone(command, %v) has not returned %v after its limit", limit, pgReturnSlack)
	if command.Process != nil {
		syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	select {
	case returned := <-done:
		return returned.Sub(started), returned
	case <-time.After(pgReturnSlack):
		t.Fatalf("waitProcessGroupGone still has not returned after its group was killed")
	}
	return 0, time.Time{}
}

// failures (R8, ownProcessGroup and killProcessGroup): a command prepared
// with ownProcessGroup leads a new process group (its group id is its pid,
// unlike the test's), a process it starts is in that group, and
// killProcessGroup returns nil and ends the leader with SIGKILL and the
// member too. A second call on the group that is gone also returns nil.
// Fails if ownProcessGroup leaves the command in the caller's group, if
// killProcessGroup signals only the leader, or if it reports a gone group
// as an error.
func TestProcessGroupOwnAndKillReachEveryMember(t *testing.T) {
	command, member := pgStartStayingLeader(t)
	leader := command.Process.Pid
	if group, err := syscall.Getpgid(leader); err != nil || group != leader {
		t.Fatalf("the started command is in process group %d (%v), want its own group %d", group, err, leader)
	}
	if syscall.Getpgrp() == leader {
		t.Fatalf("the command's group is the test's own group")
	}
	if group, err := syscall.Getpgid(member); err != nil || group != leader {
		t.Fatalf("setup: member %d is in group %d (%v), want %d", member, group, err, leader)
	}

	if err := killProcessGroup(command); err != nil {
		t.Fatalf("killProcessGroup on a running group: %v", err)
	}
	// Bounded: a leader that was not killed exits on its own after a
	// minute.
	started := time.Now()
	command.Wait()
	if elapsed := time.Since(started); elapsed > pgSetupBound {
		t.Errorf("the leader ran %v after killProcessGroup", elapsed.Round(time.Millisecond))
	}
	if status, ok := command.ProcessState.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Errorf("the leader ended with %v, want SIGKILL", command.ProcessState)
	}
	if !pgAwait(func() bool { return !dsvgProcessAlive(member) }) {
		t.Fatalf("member %d still runs %v after killProcessGroup; only the leader was signalled", member, pgSetupBound)
	}
	if err := killProcessGroup(command); err != nil {
		t.Errorf("killProcessGroup on a group that is gone: %v, want nil", err)
	}
}

// failures (R8, nil process): for a command that was never started,
// killProcessGroup is a no-op returning nil and waitProcessGroupGone
// returns at once, whatever its limit. Fails if either reports an error,
// panics, or waits.
func TestProcessGroupHelpersWithoutProcess(t *testing.T) {
	command := pgCommand(t, "sleep")
	if err := killProcessGroup(command); err != nil {
		t.Errorf("killProcessGroup with a nil Process: %v, want nil", err)
	}
	if elapsed, _ := pgTimedWait(t, command, pgLongLimit); elapsed > time.Second {
		t.Errorf("waitProcessGroupGone with a nil Process took %v, want an immediate return", elapsed.Round(time.Millisecond))
	}
}

// failures (R8, live member): while a process of the group runs,
// waitProcessGroupGone lasts until its limit, both when the leader itself
// runs and when the leader has exited and been reaped and only a member
// that is not the leader runs. Fails if the call is a no-op, or if it
// returns early because the leader is gone although a member remains.
func TestProcessGroupWaitLastsWhileAMemberRuns(t *testing.T) {
	for _, c := range []struct {
		name  string
		start func(t *testing.T) (*exec.Cmd, int)
	}{
		{"leader-running", func(t *testing.T) (*exec.Cmd, int) {
			command := pgCommand(t, "sleep")
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			pgCleanupGroup(t, command)
			return command, command.Process.Pid
		}},
		{"only-a-member-running", func(t *testing.T) (*exec.Cmd, int) {
			command, member := pgRunExitedLeader(t, "sleep")
			leader := command.Process.Pid
			if group, err := syscall.Getpgid(member); err != nil || group != leader {
				t.Fatalf("setup: member %d is in group %d (%v), want the leader's group %d", member, group, err, leader)
			}
			return command, member
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			command, running := c.start(t)
			elapsed, _ := pgTimedWait(t, command, pgLiveLimit)
			if !dsvgProcessAlive(running) {
				t.Fatalf("setup: process %d ended during the wait, so the case was not exercised", running)
			}
			if elapsed < pgLiveLimit-pgLiveTolerance {
				t.Errorf("waitProcessGroupGone returned after %v while process %d of the group ran, want about the %v limit", elapsed.Round(time.Millisecond), running, pgLiveLimit)
			}
		})
	}
}

// pgReadTimeRecord waits for the time record a "sleepfor" child writes
// just before it exits, and returns that time.
func pgReadTimeRecord(t *testing.T, path string) time.Time {
	t.Helper()
	var data []byte
	if !pgAwait(func() bool {
		var err error
		data, err = os.ReadFile(path)
		return err == nil
	}) {
		t.Fatalf("setup: the member wrote no exit record at %s", path)
	}
	ns, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		t.Fatalf("exit record %q: %v", data, err)
	}
	return time.Unix(0, ns)
}

// failures (R8, group gone): once every process of the group has exited and
// been reaped, waitProcessGroupGone returns within about 0.5 s however large
// its limit: a leader that exited and was reaped; a group killed with
// killProcessGroup whose leader was reaped and whose member was reaped
// after reparenting; and a member that is still running when the call
// starts and exits during it, where the call returns after the member's
// exit and soon after it. Fails if the call waits to its limit once the
// group is gone, or returns before the member exits.
func TestProcessGroupWaitEndsSoonAfterTheGroupIsGone(t *testing.T) {
	t.Run("leader-exited-and-reaped", func(t *testing.T) {
		command := pgCommand(t, "exit")
		if err := command.Run(); err != nil {
			t.Fatal(err)
		}
		pgCleanupGroup(t, command)
		if elapsed, _ := pgTimedWait(t, command, pgLongLimit); elapsed > pgGoneBound {
			t.Errorf("waitProcessGroupGone took %v for a group that is gone, want under %v (limit %v)", elapsed.Round(time.Millisecond), pgGoneBound, pgLongLimit)
		}
	})

	t.Run("killed-and-reaped", func(t *testing.T) {
		command, member := pgStartStayingLeader(t)
		if err := killProcessGroup(command); err != nil {
			t.Fatal(err)
		}
		// Reap the leader; bounded by its own minute if it was not killed.
		command.Wait()
		if elapsed, _ := pgTimedWait(t, command, pgLongLimit); elapsed > pgGoneBound {
			t.Errorf("waitProcessGroupGone took %v after the group was killed, want under %v (limit %v)", elapsed.Round(time.Millisecond), pgGoneBound, pgLongLimit)
		}
		if !pgAwait(func() bool { return !dsvgProcessAlive(member) }) {
			t.Errorf("setup: member %d survived killProcessGroup", member)
		}
	})

	t.Run("member-exits-during-the-wait", func(t *testing.T) {
		record := filepath.Join(t.TempDir(), "member-exit")
		command, _ := pgRunExitedLeader(t, "sleepfor", pgSleepForEnv+"=1500ms", pgExitRecordEnv+"="+record)
		_, returned := pgTimedWait(t, command, pgLongLimit)
		exited := pgReadTimeRecord(t, record)
		if returned.Before(exited) {
			t.Errorf("waitProcessGroupGone returned %v before the member exited", exited.Sub(returned).Round(time.Millisecond))
		} else if after := returned.Sub(exited); after > pgGoneBound {
			t.Errorf("waitProcessGroupGone returned %v after the member exited, want under %v (limit %v)", after.Round(time.Millisecond), pgGoneBound, pgLongLimit)
		}
	})
}

// pgLinuxState returns the state letter of pid from /proc, or "" when it
// cannot be read.
func pgLinuxState(pid int) string {
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ""
	}
	if i := bytes.LastIndexByte(stat, ')'); i >= 0 && i+2 < len(stat) {
		return string(stat[i+2])
	}
	return ""
}

// pgLinuxTasks returns how many tasks (threads, an exited main thread
// included) /proc lists for pid, or 0 when it cannot be read.
func pgLinuxTasks(pid int) int {
	entries, err := os.ReadDir(fmt.Sprintf("/proc/%d/task", pid))
	if err != nil {
		return 0
	}
	return len(entries)
}

// pgProcIsForeign returns why /proc does not belong to this process's own
// pid namespace (R8 v6: /proc/self must name os.Getpid()), or "" when it
// does.
func pgProcIsForeign() string {
	self, err := os.Readlink("/proc/self")
	if err != nil {
		return fmt.Sprintf("/proc/self cannot be read (%v), so /proc is not from this pid namespace", err)
	}
	if self != strconv.Itoa(os.Getpid()) {
		return fmt.Sprintf("/proc is from another pid namespace (/proc/self names %q, this process is %d)", self, os.Getpid())
	}
	return ""
}

// pgProcMountRestriction reads a /proc/self/mountinfo text by the v8 R9
// top-of-stack rule and returns why /proc does not show every process, or ""
// when it does. Each line reads "id parent dev root mountpoint options
// [optional fields] - fstype source superoptions"; a line with fewer than
// five fields before the "-" or three after it is ignored. A line is at
// /proc when its mount point is exactly /proc, whatever its filesystem type.
// Mounts at /proc stack, and mountinfo lists them in creation order, so
// neither the last line nor any hiding line decides: a line at /proc is a
// top when no other line at /proc names its id as its parent, and /proc
// shows all only when there is exactly one top, it is a proc mount, and it
// has no hiding option (hidepid=V, V neither 0 nor off, among the
// superoptions). A covered line is ignored, so a plain proc mount over a
// hidepid one (unshare --mount-proc) shows all, and a hidepid mount moved
// over a plain one does not. This reader is the test's own, independent of
// the code under test, so a wrong procMountShowsAll cannot turn the zombie
// test into a skip; proc_mount_linux_test.go checks it against R9's table.
func pgProcMountRestriction(mountinfo string) string {
	type procLine struct{ id, parent, fstype, superoptions string }
	var atProc []procLine
	for line := range strings.SplitSeq(mountinfo, "\n") {
		fields := strings.Fields(line)
		separator := slices.Index(fields, "-")
		if separator < 5 || len(fields) < separator+4 || fields[4] != "/proc" {
			continue
		}
		atProc = append(atProc, procLine{fields[0], fields[1], fields[separator+1], fields[separator+3]})
	}
	if len(atProc) == 0 {
		return "/proc/self/mountinfo shows no mount at /proc"
	}
	var tops []procLine
	for i, line := range atProc {
		covered := false
		for j, other := range atProc {
			if j != i && other.parent == line.id {
				covered = true
			}
		}
		if !covered {
			tops = append(tops, line)
		}
	}
	if len(tops) != 1 {
		return fmt.Sprintf("/proc/self/mountinfo shows %d mounts on top at /proc, not one", len(tops))
	}
	top := tops[0]
	if top.fstype != "proc" {
		return "the top mount at /proc is " + top.fstype + ", not proc"
	}
	for option := range strings.SplitSeq(top.superoptions, ",") {
		// hidepid=0 (off) hides nothing; 1 (noaccess), 2 (invisible) and
		// 4 (ptraceable) hide processes.
		if value, ok := strings.CutPrefix(option, "hidepid="); ok && value != "0" && value != "off" {
			return "the top proc mount at /proc has " + option
		}
	}
	return ""
}

// pgProcRestriction returns why the R8 zombie rule does not apply here
// (v6: /proc is from another pid namespace, or is mounted with a hidepid
// option that hides processes, so zombies count as running), or "" when
// /proc is full.
func pgProcRestriction() string {
	if reason := pgProcIsForeign(); reason != "" {
		return reason
	}
	mountinfo, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return fmt.Sprintf("/proc/self/mountinfo cannot be read (%v)", err)
	}
	return pgProcMountRestriction(string(mountinfo))
}

// failures (R8, zombies, Linux only): a group whose only process has exited
// but has not been reaped (a zombie leader: the test never waits for it)
// counts as gone, so waitProcessGroupGone returns within about 0.5 s
// however large its limit. Fails if a zombie counts as running, for example
// when the check is a signal 0 to the group, which succeeds for zombies.
// v6: the rule holds only with a full /proc, so the test skips when /proc
// is from another pid namespace or hides processes with hidepid.
func TestProcessGroupWaitTreatsZombiesAsGoneOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("R8 says nothing about zombies on " + runtime.GOOS)
	}
	if _, err := os.Stat("/proc/self/stat"); errors.Is(err, fs.ErrNotExist) {
		t.Skip("no /proc to observe the zombie")
	}
	if reason := pgProcRestriction(); reason != "" {
		t.Skip("R8 v6: zombies count as running here: " + reason)
	}
	command := pgCommand(t, "exit")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	// Reaped only in cleanup (or by the helper itself, which R8 allows).
	pgCleanupGroup(t, command)
	leader := command.Process.Pid
	if !pgAwait(func() bool { return pgLinuxState(leader) == "Z" }) {
		t.Fatalf("setup: process %d never became a zombie (state %q)", leader, pgLinuxState(leader))
	}
	if elapsed, _ := pgTimedWait(t, command, pgLongLimit); elapsed > pgGoneBound {
		t.Errorf("waitProcessGroupGone took %v for a group holding only a zombie, want under %v (limit %v)", elapsed.Round(time.Millisecond), pgGoneBound, pgLongLimit)
	}
}

// failures (R8 v6, zombie leader with live threads, Linux only): the group
// leader's main thread has exited, so /proc/PID/stat shows it as Z, while
// other threads of the same process still run. The group counts as running,
// so waitProcessGroupGone lasts until its limit of about 2 s. Fails if the
// wait judges the process by the leader's state alone and treats it as a
// zombie that is gone.
func TestProcessGroupWaitLastsWhileAZombieLeaderHasLiveThreads(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the live-thread rule is about Linux /proc; it does not apply on " + runtime.GOOS)
	}
	if _, err := os.Stat("/proc/self/stat"); errors.Is(err, fs.ErrNotExist) {
		t.Skip("no /proc to confirm that the leader shows Z with live threads")
	}
	if reason := pgProcIsForeign(); reason != "" {
		t.Skip("/proc cannot show the leader to confirm the setup: " + reason)
	}
	command := pgCommand(t, "threadexit")
	var stderr pgSyncBuffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	pgCleanupGroup(t, command)
	leader := command.Process.Pid
	exercised := func() bool { return pgLinuxState(leader) == "Z" && pgLinuxTasks(leader) > 1 }
	if !pgAwait(exercised) {
		t.Fatalf("setup: process %d never showed a zombie leader with live threads (state %q, %d tasks)\nstderr: %s", leader, pgLinuxState(leader), pgLinuxTasks(leader), stderr.String())
	}
	elapsed, _ := pgTimedWait(t, command, pgLiveLimit)
	if !exercised() {
		t.Fatalf("setup: process %d no longer has live threads after the wait (state %q, %d tasks), so the case was not exercised", leader, pgLinuxState(leader), pgLinuxTasks(leader))
	}
	if elapsed < pgLiveLimit-pgLiveTolerance {
		t.Errorf("waitProcessGroupGone returned after %v while the zombie leader %d still had %d tasks, want about the %v limit", elapsed.Round(time.Millisecond), leader, pgLinuxTasks(leader), pgLiveLimit)
	}
}

// pgForeignProcWaiter is the "foreignproc" child, started by
// TestProcessGroupWaitCountsMembersProcCannotShow in a private mount
// namespace (unshare --mount). It starts a member in its own process group
// under the original /proc, then mounts on /proc the proc filesystem of a
// fresh pid namespace (unshare --pid --fork mount -t proc), so /proc can no
// longer show this process or the member. It prints "member <pid>" once the
// setup is confirmed, calls waitProcessGroupGone(member, pgLiveLimit), and
// prints "elapsed <nanoseconds> alive <bool>", where alive says whether the
// member was still running after the wait. A setup it cannot make prints
// "skip <reason>". It kills the member's group before it returns.
func pgForeignProcWaiter() int {
	skip := func(format string, args ...any) int {
		fmt.Printf("skip "+format+"\n", args...)
		return 0
	}
	// Resolved first: os.Executable reads /proc/self/exe.
	executable, err := os.Executable()
	if err != nil {
		return skip("executable: %v", err)
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		return skip("%v", err)
	}
	mount, err := exec.LookPath("mount")
	if err != nil {
		return skip("%v", err)
	}
	member := exec.Command(executable)
	member.Env = append(os.Environ(), pgChildEnv+"=sleep")
	ownProcessGroup(member)
	if err := member.Start(); err != nil {
		return skip("start the member: %v", err)
	}
	defer func() {
		killProcessGroup(member)
		member.Wait()
	}()
	pid := member.Process.Pid
	if output, err := exec.Command(unshare, "--pid", "--fork", "--", mount, "-t", "proc", "proc", "/proc").CombinedOutput(); err != nil {
		return skip("mount a proc filesystem of a new pid namespace: %v: %s", err, bytes.TrimSpace(output))
	}
	if pgProcIsForeign() == "" {
		return skip("/proc still belongs to this pid namespace after the mount")
	}
	if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); err == nil {
		return skip("/proc still shows a process %d after the mount", pid)
	}
	fmt.Printf("member %d\n", pid)
	started := time.Now()
	waitProcessGroupGone(member, pgLiveLimit)
	elapsed := time.Since(started)
	var status syscall.WaitStatus
	reaped, _ := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
	fmt.Printf("elapsed %d alive %t\n", elapsed.Nanoseconds(), reaped == 0)
	return 0
}

// failures (R8 v6, members /proc cannot show; optional, privileged): with a
// /proc from another pid namespace, a running member of the group that
// /proc does not show still counts as running, so waitProcessGroupGone
// lasts until its limit of about 2 s. The setup needs root and the
// util-linux unshare and mount tools (a private mount namespace, then a proc
// filesystem of a new pid namespace mounted on /proc inside it; the host's
// /proc is untouched), and the test skips cleanly without them. Fails if
// the wait treats a member /proc does not list as gone. The other case R8
// names, hidepid together with a non-dumpable member, is not tested: it
// needs a remount of /proc with hidepid, which on older kernels changes the
// options of the host's /proc too.
func TestProcessGroupWaitCountsMembersProcCannotShow(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("pid namespaces and /proc are Linux only")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root to make a mount namespace and mount /proc")
	}
	unshare, err := exec.LookPath("unshare")
	if err != nil {
		t.Skipf("needs util-linux unshare: %v", err)
	}
	if _, err := exec.LookPath("mount"); err != nil {
		t.Skipf("needs mount: %v", err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	waiter := exec.Command(unshare, "--mount", "--propagation", "private", "--", executable)
	waiter.Env = append(os.Environ(), pgChildEnv+"=foreignproc")
	var stderr pgSyncBuffer
	waiter.Stderr = &stderr
	stdout, err := waiter.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := waiter.Start(); err != nil {
		t.Skipf("cannot start unshare: %v", err)
	}
	lines := make(chan string)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()
	member := 0
	t.Cleanup(func() {
		if member > 0 {
			syscall.Kill(-member, syscall.SIGKILL)
		}
		waiter.Process.Kill()
		for range lines {
		}
		waiter.Wait()
	})
	next := func(within time.Duration) (string, bool) {
		select {
		case line, ok := <-lines:
			return line, ok
		case <-time.After(within):
			t.Fatalf("the waiter printed nothing within %v\nstderr: %s", within, stderr.String())
		}
		return "", false
	}

	line, ok := next(pgSetupBound)
	if reason, skipped := strings.CutPrefix(line, "skip "); skipped || !ok {
		// Before "member", only the test's own setup ran, so a failure
		// there is a missing privilege or tool, not a defect. Waiting
		// first completes stderr; the waiter has ended or ends soon.
		waiter.Wait()
		t.Skipf("cannot build a /proc from another pid namespace: %s %s", reason, strings.TrimSpace(stderr.String()))
	}
	if _, err := fmt.Sscanf(line, "member %d", &member); err != nil || member <= 0 {
		t.Fatalf("the waiter printed %q, want \"member <pid>\"\nstderr: %s", line, stderr.String())
	}
	line, ok = next(pgLiveLimit + pgReturnSlack)
	if !ok {
		t.Fatalf("the waiter ended without a result\nstderr: %s", stderr.String())
	}
	var nanoseconds int64
	var alive bool
	if _, err := fmt.Sscanf(line, "elapsed %d alive %t", &nanoseconds, &alive); err != nil {
		t.Fatalf("the waiter printed %q, want \"elapsed <ns> alive <bool>\": %v\nstderr: %s", line, err, stderr.String())
	}
	if !alive {
		t.Fatalf("setup: member %d ended during the wait, so the case was not exercised", member)
	}
	if elapsed := time.Duration(nanoseconds); elapsed < pgLiveLimit-pgLiveTolerance {
		t.Errorf("waitProcessGroupGone returned after %v although member %d ran and only a /proc from another pid namespace hid it, want about the %v limit", elapsed.Round(time.Millisecond), member, pgLiveLimit)
	}
}

// pgSyncBuffer is a bytes.Buffer that a child's output copier and the test
// may use at the same time.
type pgSyncBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *pgSyncBuffer) Write(data []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(data)
}

func (b *pgSyncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}
