//go:build unix

package procgroup_test

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/procgroup"
)

const (
	// afterKill is the file the tests create once Kill has returned. Every
	// member waits for it before its late write, so a late-* file proves that
	// its writer was alive after Kill returned. A late write no longer depends
	// on how long a loaded runner takes between the members starting and Kill:
	// a member that wrote after a fixed delay could finish before Kill was
	// even called (macOS CI, PR #80), which proved nothing about Kill.
	afterKill = "after-kill"
	// awaitAfterKill polls for $1/after-kill for about 30 s and then exits
	// without writing, so a member left behind by a failed test still ends.
	awaitAfterKill = `i=0
while [ ! -e "$1/` + afterKill + `" ]; do
	i=$((i + 1))
	if [ "$i" -gt 600 ]; then exit 0; fi
	sleep 0.05
done
`
	// delayedMemberScript creates $1/ready-delayed once running, then creates
	// $1/late-delayed once $1/after-kill exists. Only a kill stops that write.
	delayedMemberScript = `: > "$1/ready-delayed"
` + awaitAfterKill + `: > "$1/late-delayed"`
	// forkedChildScript is one child of the forking member: it creates
	// $1/late-forked-$2 once $1/after-kill exists.
	forkedChildScript = awaitAfterKill + `: > "$1/late-forked-$2"`
	// forkingMemberScript creates $1/ready-forking and then, for about 2.5 s,
	// keeps forking children ($2 is forkedChildScript), each of which forks
	// sleep every 50 ms, so forks are in flight whenever Kill runs.
	forkingMemberScript = `: > "$1/ready-forking"
i=0
while [ "$i" -lt 200 ]; do
	/bin/sh -c "$2" sh "$1" "$i" &
	i=$((i + 1))
	sleep 0.01
done
wait`
	// memberLeaderScript starts the delayed member and the forking member in
	// its group with all their output on /dev/null, waits until both are
	// running and exits 0. It gives up with exit 3 after about five seconds.
	memberLeaderScript = `/bin/sh -c "$2" sh "$1" >/dev/null 2>&1 </dev/null &
/bin/sh -c "$3" sh "$1" "$4" >/dev/null 2>&1 </dev/null &
i=0
while [ ! -e "$1/ready-delayed" ] || [ ! -e "$1/ready-forking" ]; do
	i=$((i + 1))
	if [ "$i" -gt 500 ]; then exit 3; fi
	sleep 0.01
done
exit 0`
	// lateWindow is how long the tests watch for late writes after creating
	// $1/after-kill: a surviving member notices it within 50 ms, and a
	// surviving forking member forks a child that notices it at once.
	lateWindow = 2 * time.Second
	// prompt is how quickly Kill must return when no live member remains to
	// wait for.
	prompt = time.Second
)

func requireGroupPlatform(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("process-group kill semantics are verified on native Linux and macOS")
	}
}

// groupHasLiveMember reports whether process group pgid has a member that is
// not a zombie. It only ever sends signal 0, and only to a group led by a
// process this test started. kill(-pgid, 0) failing with ESRCH (no member) or
// EPERM (macOS: only zombies remain) means no live member. On Linux a zombie
// still accepts signal 0, so a success there is checked against /proc.
func groupHasLiveMember(pgid int) (bool, error) {
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

func requireLive(t *testing.T, pgid int, want bool, when string) {
	t.Helper()
	live, err := groupHasLiveMember(pgid)
	if err != nil {
		t.Fatalf("%s: %v", when, err)
	}
	if live != want {
		t.Fatalf("%s: process group %d has a live member = %v, want %v", when, pgid, live, want)
	}
}

// groupLeader starts command as the leader of a new process group, whose id
// is the leader's pid, and returns that id. Cleanup reaps a leader the test
// did not wait for.
func groupLeader(t *testing.T, command *exec.Cmd) int {
	t.Helper()
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Wait()
		}
	})
	pgid := command.Process.Pid
	if pgid <= 1 || pgid == os.Getpid() || pgid == syscall.Getpgrp() {
		t.Fatalf("unexpected process group id %d", pgid)
	}
	return pgid
}

// memberGroupCommand returns the leader of a member group writing into
// directory. If the test stops before requireNoLateWrite, cleanup creates
// $1/after-kill so surviving members write and exit at once instead of
// polling for 30 s.
func memberGroupCommand(t *testing.T, directory string) *exec.Cmd {
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(directory, afterKill), nil, 0o600) })
	return exec.Command("/bin/sh", "-c", memberLeaderScript, "sh", directory, delayedMemberScript, forkingMemberScript, forkedChildScript)
}

// awaitStdoutClosed reads reader until every writer has closed it. A leader
// whose stdout is the only writer has then exited, without being reaped.
func awaitStdoutClosed(t *testing.T, reader *os.File) {
	t.Helper()
	defer reader.Close()
	if _, err := io.Copy(io.Discard, reader); err != nil {
		t.Fatal(err)
	}
}

// requireMemberMarkerControl runs both member kinds unkilled with
// $1/after-kill already present, proving each can report its late write.
func requireMemberMarkerControl(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, afterKill), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/bin/sh", "-c", delayedMemberScript, "sh", directory).Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("/bin/sh", "-c", forkedChildScript, "sh", directory, "0").Run(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ready-delayed", "late-delayed", "late-forked-0"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("unkilled member did not write %s: %v", name, err)
		}
	}
	// The forking member as the leader starts it: its children must receive
	// forkedChildScript and write. It runs about 2.5 s; one child suffices.
	forking := exec.Command("/bin/sh", "-c", forkingMemberScript, "sh", directory, forkedChildScript)
	if err := forking.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = forking.Process.Kill()
		_ = forking.Wait()
	}()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if _, err := os.Stat(filepath.Join(directory, "late-forked-1")); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("unkilled forking member's children did not write late-forked-1")
		}
	}
}

// requireNoLateWrite creates $1/after-kill, which Kill has returned before,
// waits lateWindow and then checks that no member reached a late write.
func requireNoLateWrite(t *testing.T, directory string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, afterKill), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(lateWindow)
	late, err := filepath.Glob(filepath.Join(directory, "late-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(late) != 0 {
		t.Fatalf("%d group members outlived Kill and wrote: %v", len(late), late)
	}
}

// PG-1: the leader has exited and been reaped; a delayed member and a forking
// member (with its own children) are still alive in the group.
func TestKillStopsLiveMembersAfterLeaderWasReaped(t *testing.T) {
	requireGroupPlatform(t)
	requireMemberMarkerControl(t)
	directory := t.TempDir()
	leader := memberGroupCommand(t, directory)
	pgid := groupLeader(t, leader)
	if err := leader.Wait(); err != nil {
		t.Fatalf("leader did not see both members running: %v", err)
	}
	requireLive(t, pgid, true, "before Kill")
	if err := procgroup.Kill(pgid); err != nil {
		t.Fatalf("Kill(%d) = %v, want nil", pgid, err)
	}
	// On macOS this is kill(-pgid, 0) failing with ESRCH or EPERM; on Linux
	// members that are already zombies (awaiting their new parent) are allowed.
	requireLive(t, pgid, false, "after Kill")
	requireNoLateWrite(t, directory)
}

// PG-2: the only member is the test's own exited child, not yet waited for.
func TestKillReturnsPromptlyForUnreapedZombieOnly(t *testing.T) {
	requireGroupPlatform(t)
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.Command("/bin/sh", "-c", "exit 0")
	child.Stdout = writer
	pgid := groupLeader(t, child)
	writer.Close()
	awaitStdoutClosed(t, reader)
	// Let the exit that closed stdout finish, so the child is a zombie.
	time.Sleep(50 * time.Millisecond)
	if child.ProcessState != nil {
		t.Fatal("child was reaped before Kill")
	}
	started := time.Now()
	err = procgroup.Kill(pgid)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Kill(%d) with only an unreaped zombie = %v, want nil", pgid, err)
	}
	if elapsed >= prompt {
		t.Fatalf("Kill(%d) with only an unreaped zombie took %v, want under %v", pgid, elapsed, prompt)
	}
	_ = child.Wait()
}

// PG-3: the leader has exited but has not been waited for (a zombie), and
// live members remain in its group.
func TestKillReturnsPromptlyWithZombieLeaderAndKillsMembers(t *testing.T) {
	requireGroupPlatform(t)
	requireMemberMarkerControl(t)
	directory := t.TempDir()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	leader := memberGroupCommand(t, directory)
	leader.Stdout = writer
	pgid := groupLeader(t, leader)
	writer.Close()
	awaitStdoutClosed(t, reader)
	// Let the exit that closed stdout finish, so the leader is a zombie.
	time.Sleep(50 * time.Millisecond)
	if leader.ProcessState != nil {
		t.Fatal("leader was reaped before Kill")
	}
	requireLive(t, pgid, true, "before Kill")
	started := time.Now()
	err = procgroup.Kill(pgid)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Kill(%d) with a zombie leader = %v, want nil", pgid, err)
	}
	if elapsed >= prompt {
		t.Fatalf("Kill(%d) with a zombie leader took %v, want under %v", pgid, elapsed, prompt)
	}
	requireLive(t, pgid, false, "after Kill")
	// The leader exited 0 before Kill, which proves both members were running.
	if err := leader.Wait(); err != nil {
		t.Fatalf("leader did not see both members running: %v", err)
	}
	requireNoLateWrite(t, directory)
}

// PG-5: the group's only member was its leader, which has been reaped.
func TestKillReturnsPromptlyForEmptyGroup(t *testing.T) {
	requireGroupPlatform(t)
	leader := exec.Command("/bin/sh", "-c", "exit 0")
	pgid := groupLeader(t, leader)
	if err := leader.Wait(); err != nil {
		t.Fatal(err)
	}
	requireLive(t, pgid, false, "before Kill")
	started := time.Now()
	err := procgroup.Kill(pgid)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Kill(%d) of an empty group = %v, want nil", pgid, err)
	}
	if elapsed >= prompt {
		t.Fatalf("Kill(%d) of an empty group took %v, want under %v", pgid, elapsed, prompt)
	}
}

// PG-4: ids that are not an owned process group are refused. The negative id
// and 0 come first and stop the test if accepted, so a Kill with no guard at
// all never reaches 1, where kill(-1, SIGKILL) would signal every process of
// the user.
func TestKillRefusesInvalidGroupIDs(t *testing.T) {
	for _, pgid := range []int{-5, 0, 1} {
		if err := procgroup.Kill(pgid); err == nil {
			t.Fatalf("Kill(%d) = nil, want an error", pgid)
		}
	}
}
