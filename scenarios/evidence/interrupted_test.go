package evidence_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// slowScript is the program of the check slow. It creates started.<pid> in
// the directory $KITSU_TEST_SLOW and then waits for that directory to hold
// release or to be gone. Only when neither happens for ten minutes, far
// longer than any test waits, does it create finished.<pid> and exit 0. So
// a program that kitsu did not stop ends by itself once the test releases it,
// and the test never has to kill a process by an id that may be reused.
const slowScript = `d="$KITSU_TEST_SLOW"
: > "$d/started.$$"
i=0
while test -d "$d" && ! test -e "$d/release"; do
  i=$((i + 1))
  if test $i -ge 6000; then : > "$d/finished.$$"; exit 0; fi
  sleep 0.1
done
`

// slow declares the check slow, whose program is slowScript, commits it,
// and returns the directory the program reports to. The test's cleanup
// releases every program of slow that still runs.
func slow(t *testing.T, repo *kitsutest.Repo) string {
	t.Helper()
	dir := t.TempDir()
	repo.Write("slow.sh", slowScript)
	declare(t, repo, "[[check]]\nid = \"slow\"\nrun = [\"sh\", \"slow.sh\"]\n")
	repo.Setenv("KITSU_TEST_SLOW", filepath.ToSlash(dir))
	t.Cleanup(func() { _ = os.WriteFile(filepath.Join(dir, "release"), nil, 0o644) })
	return dir
}

// slowPrograms lists the process ids of the programs of slow that have
// started, in no order, as their started files name them.
func slowPrograms(t *testing.T, dir string) []int {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(dir, "started.*"))
	if err != nil {
		t.Fatal(err)
	}
	var pids []int
	for _, file := range files {
		pid, err := strconv.Atoi(strings.TrimPrefix(filepath.Ext(file), "."))
		if err != nil {
			t.Fatalf("bad started file %s", file)
		}
		pids = append(pids, pid)
	}
	return pids
}

// waitForPrograms waits until n programs of slow have started and returns
// their process ids. The bound only ends a test that failed.
func waitForPrograms(t *testing.T, dir string, n int, run *background) []int {
	t.Helper()
	var pids []int
	if !waitFor(60*time.Second, func() bool {
		pids = slowPrograms(t, dir)
		return len(pids) >= n
	}) {
		t.Fatalf("%d program(s) of slow started, want %d\n%s", len(pids), n, run.out.String())
	}
	return pids
}

// Requirement "Interrupted runs", scenario "`kitsu` is killed during a run".
// "The second command starts a new run at once" is held by content, not by
// the clock: the program of the killed run is still waiting, and would only
// end on its own after ten minutes, when the program of the second run has
// started.
func TestKilledRunDoesNotBlockTheNextRun(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := kitsutest.Demo(t)
	dir := slow(t, repo)
	first := startKitsu(t, repo, nil, "run", "slow")
	killed := waitForPrograms(t, dir, 1, first)[0]
	if err := first.cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if !first.wait(60 * time.Second) {
		t.Fatal("kitsu did not end after SIGKILL")
	}

	second := startKitsu(t, repo, nil, "run", "slow")
	waitForPrograms(t, dir, 2, second)
	if _, err := os.Stat(filepath.Join(dir, "release")); err == nil {
		t.Fatal("the programs were released before the test ended")
	}
	if _, err := os.Stat(filepath.Join(dir, fmt.Sprintf("finished.%d", killed))); err == nil {
		t.Errorf("the program of the killed run had ended (after ten minutes) before the second run started: the second run waited for it")
	}
	// End the second run, so that only the killed run could still show as
	// running.
	if err := second.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if !second.wait(60 * time.Second) {
		t.Fatalf("the second kitsu run slow did not end after SIGINT\n%s", second.out.String())
	}

	text := repo.Kitsu("status")
	if lineWith(strings.ToLower(text.Output()), "slow", "running") {
		t.Errorf("kitsu status shows a run of slow as running:\n%s", text.Output())
	}
	if state := status(t, repo).check(t, "slow").State; state == "running" {
		t.Errorf("kitsu status --json shows slow as running")
	}
}

// Requirement "Interrupted runs", scenario "The terminal closes". The
// program of slow stands for `sleep 60`: it does not end on its own while
// the test runs.
func TestHangupRecordsTheRunAsInterrupted(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := kitsutest.Demo(t)
	dir := slow(t, repo)
	run := startKitsu(t, repo, nil, "run", "slow")
	program := waitForPrograms(t, dir, 1, run)[0]

	if err := run.cmd.Process.Signal(syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	signalled := time.Now()
	if !run.wait(60 * time.Second) {
		t.Fatalf("kitsu did not exit after SIGHUP\n%s", run.out.String())
	}
	if took := run.ended.Sub(signalled); took > 10*time.Second {
		t.Errorf("kitsu took %s to exit after SIGHUP, want at most 10s", took)
	}

	if outcome := lastRecord(t, repo, "slow")["outcome"]; outcome != "interrupted" {
		t.Errorf("slow is recorded as %v, want interrupted", outcome)
	}
	// A process that was killed may stay a zombie for a moment until its new
	// parent collects it. Only reading whether the process exists: the test
	// never signals this id.
	if !waitFor(30*time.Second, func() bool { return !alive(program) }) {
		t.Errorf("the program of slow (process %d) still runs", program)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", run.code, run.out.String())
	}
}

// Requirement "Interrupted runs", scenario "Ctrl+Break on Windows".
func TestCtrlBreakRecordsTheRunAsInterrupted(t *testing.T) {
	onlyOnWindows(t)
	t.Parallel()
	repo := kitsutest.Demo(t)
	pidFile := filepath.Join(t.TempDir(), "pid")
	repo.Write("slow.ps1", "Set-Content -Path '"+pidFile+"' -Value $PID\nStart-Sleep 60\n")
	declare(t, repo, "[[check]]\nid = \"slow\"\nrun = [\"powershell\", \"-NoProfile\", \"-ExecutionPolicy\", \"Bypass\", \"-File\", \"slow.ps1\"]\n")
	run := startKitsu(t, repo, newConsoleGroup, "run", "slow")
	var pid string
	if !waitFor(30*time.Second, func() bool {
		data, err := os.ReadFile(pidFile)
		pid = strings.TrimSpace(string(data))
		return err == nil && pid != ""
	}) {
		t.Fatalf("the program of slow did not start\n%s", run.out.String())
	}
	// The program still runs here, so the id is its own. From now on the
	// test asks the handle, not the id.
	running, found := watchProcess(t, pid, "powershell.exe")
	if !found {
		t.Fatalf("process %s, the program of slow, is not a running powershell.exe", pid)
	}

	if err := sendCtrlBreak(run.cmd.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if !run.wait(10 * time.Second) {
		t.Fatalf("kitsu did not exit within 10s of Ctrl+Break\n%s", run.out.String())
	}

	if outcome := lastRecord(t, repo, "slow")["outcome"]; outcome != "interrupted" {
		t.Errorf("slow is recorded as %v, want interrupted", outcome)
	}
	if running() {
		t.Errorf("the program of slow (process %s) still runs", pid)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", run.code, run.out.String())
	}
}
