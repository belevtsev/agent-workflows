package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

const slowCheck = "[[check]]\nid = \"slow\"\nrun = [\"sleep\", \"60\"]\n"

// sleepOf waits until cw has started `sleep 60` as its child and returns the
// process id of that sleep. The test's cleanup kills it.
func sleepOf(t *testing.T, run *background) int {
	t.Helper()
	var pids []int
	if !waitFor(10*time.Second, func() bool {
		pids = pgrep(t, run.cmd.Process.Pid, "^sleep 60$")
		return len(pids) > 0
	}) {
		t.Fatalf("cw did not start sleep 60\n%s", run.out.String())
	}
	pid := pids[0]
	t.Cleanup(func() {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	})
	return pid
}

// Requirement "Interrupted runs", scenario "`cw` is killed during a run".
func TestKilledRunDoesNotBlockTheNextRun(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	declare(t, repo, slowCheck)
	first := startCW(t, repo, nil, "run", "slow")
	firstSleep := sleepOf(t, first)
	if err := first.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if !first.wait(15 * time.Second) {
		t.Fatal("cw did not end after SIGKILL")
	}

	second := startCW(t, repo, nil, "run", "slow")
	started := time.Now()
	secondSleep := sleepOf(t, second)
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("the second cw run slow took %s to start its run", took)
	}
	if secondSleep == firstSleep {
		t.Fatalf("the second command started no new run")
	}
	// End the second run, so that only the killed run could still show as
	// running.
	if err := second.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if !second.wait(15 * time.Second) {
		t.Fatalf("the second cw run slow did not end after SIGINT\n%s", second.out.String())
	}

	text := repo.CW("status")
	if lineWith(strings.ToLower(text.Output()), "slow", "running") {
		t.Errorf("cw status shows a run of slow as running:\n%s", text.Output())
	}
	if state := status(t, repo).check(t, "slow").State; state == "running" {
		t.Errorf("cw status --json shows slow as running")
	}
}

// Requirement "Interrupted runs", scenario "The terminal closes".
func TestHangupRecordsTheRunAsInterrupted(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	declare(t, repo, slowCheck)
	run := startCW(t, repo, nil, "run", "slow")
	sleep := sleepOf(t, run)

	if err := run.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	signalled := time.Now()
	if !run.wait(10 * time.Second) {
		t.Fatalf("cw did not exit within 10s of SIGHUP\n%s", run.out.String())
	}
	if took := run.ended.Sub(signalled); took > 10*time.Second {
		t.Errorf("cw took %s to exit after SIGHUP", took)
	}

	if outcome := lastRecord(t, repo, "slow")["outcome"]; outcome != "interrupted" {
		t.Errorf("slow is recorded as %v, want interrupted", outcome)
	}
	// A process that was killed may stay a zombie for a moment until its new
	// parent collects it.
	if !waitFor(2*time.Second, func() bool { return !alive(sleep) }) {
		t.Errorf("sleep %d still runs", sleep)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", run.code, run.out.String())
	}
}

// Requirement "Interrupted runs", scenario "Ctrl+Break on Windows".
func TestCtrlBreakRecordsTheRunAsInterrupted(t *testing.T) {
	onlyOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	repo.Write("slow.ps1", "Set-Content -Path '"+pidFile+"' -Value $PID\nStart-Sleep 60\n")
	declare(t, repo, "[[check]]\nid = \"slow\"\nrun = [\"powershell\", \"-NoProfile\", \"-ExecutionPolicy\", \"Bypass\", \"-File\", \"slow.ps1\"]\n")
	run := startCW(t, repo, newConsoleGroup, "run", "slow")
	var pid string
	if !waitFor(30*time.Second, func() bool {
		data, err := os.ReadFile(pidFile)
		pid = strings.TrimSpace(string(data))
		return err == nil && pid != ""
	}) {
		t.Fatalf("the program of slow did not start\n%s", run.out.String())
	}
	t.Cleanup(func() { _ = windowsKill(pid) })

	if err := sendCtrlBreak(run.cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if !run.wait(10 * time.Second) {
		t.Fatalf("cw did not exit within 10s of Ctrl+Break\n%s", run.out.String())
	}

	if outcome := lastRecord(t, repo, "slow")["outcome"]; outcome != "interrupted" {
		t.Errorf("slow is recorded as %v, want interrupted", outcome)
	}
	if windowsAlive(t, pid) {
		t.Errorf("the program of slow (process %s) still runs", pid)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", run.code, run.out.String())
	}
}
