package evidence_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// statusJSON is the object that `cw status --json` prints.
type statusJSON struct {
	Tree   string       `json:"tree"`
	Checks []checkEntry `json:"checks"`
}

type checkEntry struct {
	ID         string `json:"id"`
	State      string `json:"state"`
	RunID      string `json:"run_id"`
	Tree       string `json:"tree"`
	Outcome    string `json:"outcome"`
	ExitStatus *int   `json:"exit_status"`
	EndedAt    string `json:"ended_at"`
	Log        string `json:"log"`
}

// status runs `cw status --json` and decodes what it prints.
func status(t *testing.T, repo *cwtest.Repo) statusJSON {
	t.Helper()
	return decodeStatus(t, repo.CW("status", "--json"))
}

func decodeStatus(t *testing.T, result cwtest.Result) statusJSON {
	t.Helper()
	var out statusJSON
	if err := json.Unmarshal([]byte(result.Stdout), &out); err != nil {
		t.Fatalf("cw status --json (exit status %d) printed no JSON object: %v\n%s", result.Code, err, result.Output())
	}
	return out
}

func (s statusJSON) check(t *testing.T, id string) checkEntry {
	t.Helper()
	for _, entry := range s.Checks {
		if entry.ID == id {
			return entry
		}
	}
	t.Fatalf("cw status --json has no entry for %q: %+v", id, s.Checks)
	return checkEntry{}
}

func exitStatus(entry checkEntry) string {
	if entry.ExitStatus == nil {
		return "none"
	}
	return strconv.Itoa(*entry.ExitStatus)
}

// tracedDemo is the demo repository whose unit.sh and lint.sh also append
// their name to a file outside the repository each time they are executed.
// The change is committed, so the fixture still has one clean commit on top.
func tracedDemo(t *testing.T) (*cwtest.Repo, string) {
	t.Helper()
	repo := cwtest.Demo(t)
	trace := filepath.Join(t.TempDir(), "trace")
	for _, name := range []string{"unit", "lint"} {
		script := name + ".sh"
		repo.Write(script, "echo "+name+" >> '"+trace+"'\n"+repo.Read(script))
	}
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "trace the scripts")
	return repo, trace
}

// executed lists the scripts that the trace says were executed, in order.
func executed(t *testing.T, trace string) []string {
	t.Helper()
	data, err := os.ReadFile(trace)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

// declare appends checks to cw.toml and commits the change.
func declare(t *testing.T, repo *cwtest.Repo, checks string) {
	t.Helper()
	repo.Write("cw.toml", repo.Read("cw.toml")+"\n"+checks)
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "declare more checks")
}

// checkedState runs cw run and requires both checks to pass.
func checkedState(t *testing.T, repo *cwtest.Repo) {
	t.Helper()
	if result := repo.CW("run"); result.Code != 0 {
		t.Fatalf("setting up the checked state: cw run exit status %d\n%s", result.Code, result.Output())
	}
}

// lastRecord decodes the most recent record file of a check.
func lastRecord(t *testing.T, repo *cwtest.Repo, id string) map[string]any {
	t.Helper()
	files := repo.RecordFiles(id)
	if len(files) == 0 {
		t.Fatalf("no run of %q is recorded", id)
	}
	return readRecord(t, files[len(files)-1])
}

func readRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fields := map[string]any{}
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return fields
}

// logPath turns the log of a record or a status entry into a file name.
func logPath(repo *cwtest.Repo, log string) string {
	if filepath.IsAbs(log) {
		return log
	}
	return filepath.Join(repo.StoreDir(), filepath.FromSlash(log))
}

// lineWith says whether one line of output holds every word.
func lineWith(output string, words ...string) bool {
	for _, line := range strings.Split(output, "\n") {
		all := true
		for _, word := range words {
			if !strings.Contains(line, word) {
				all = false
				break
			}
		}
		if all {
			return true
		}
	}
	return false
}

func cwProgram(t *testing.T) string {
	t.Helper()
	name := "cw"
	if runtime.GOOS == "windows" {
		name = "cw.exe"
	}
	return filepath.Join(cwtest.Binary(t), name)
}

// lockedBuffer collects the output of a program that runs in the background.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// background is a cw command the test started and may signal.
type background struct {
	cmd   *exec.Cmd
	out   *lockedBuffer
	done  chan struct{}
	code  int
	err   error
	ended time.Time
}

// startCW starts cw in the background. A process that outlives cw and holds
// its output open cannot keep the test waiting: Wait gives up on the output
// five seconds after cw has exited. The test's cleanup kills cw if it is
// still running.
func startCW(t *testing.T, repo *cwtest.Repo, prepare func(*exec.Cmd), args ...string) *background {
	t.Helper()
	cmd := repo.Command(cwProgram(t), args...)
	out := &lockedBuffer{}
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second
	if prepare != nil {
		prepare(cmd)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	bg := &background{cmd: cmd, out: out, done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		bg.ended = time.Now()
		bg.code = cmd.ProcessState.ExitCode()
		bg.err = err
		close(bg.done)
	}()
	t.Cleanup(func() {
		select {
		case <-bg.done:
		default:
			_ = cmd.Process.Kill()
			<-bg.done
		}
	})
	return bg
}

// wait waits at most limit for cw to exit and says whether it did.
func (b *background) wait(limit time.Duration) bool {
	select {
	case <-b.done:
		return true
	case <-time.After(limit):
		return false
	}
}

// runLimited runs cw in the foreground, kills it after limit, and returns how
// it ended and how long it took.
func runLimited(t *testing.T, repo *cwtest.Repo, limit time.Duration, args ...string) (cwtest.Result, time.Duration) {
	t.Helper()
	start := time.Now()
	bg := startCW(t, repo, nil, args...)
	if !bg.wait(limit) {
		_ = bg.cmd.Process.Kill()
		<-bg.done
		t.Fatalf("cw %s did not end within %s\n%s", strings.Join(args, " "), limit, bg.out.String())
	}
	return cwtest.Result{Stdout: bg.out.String(), Code: bg.code}, bg.ended.Sub(start)
}

// pgrep lists the processes whose command line matches pattern; with parent
// above 0, only the children of that process.
func pgrep(t *testing.T, parent int, pattern string) []int {
	t.Helper()
	args := []string{"-f", pattern}
	if parent > 0 {
		args = append([]string{"-P", strconv.Itoa(parent)}, args...)
	}
	out, err := exec.Command("pgrep", args...).Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 1 {
			return nil
		}
		t.Fatalf("pgrep %s: %v", strings.Join(args, " "), err)
	}
	var pids []int
	for _, field := range strings.Fields(string(out)) {
		pid, err := strconv.Atoi(field)
		if err != nil {
			t.Fatalf("pgrep printed %q", out)
		}
		pids = append(pids, pid)
	}
	sort.Ints(pids)
	return pids
}

// killMatching kills every process whose command line matches pattern, so
// that a failing test leaves nothing behind.
func killMatching(t *testing.T, pattern string) {
	t.Cleanup(func() { _ = exec.Command("pkill", "-KILL", "-f", pattern).Run() })
}

// waitFor polls cond until it holds or limit passes.
func waitFor(limit time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(limit)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the scenario sends a POSIX signal")
	}
}

func onlyOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the scenario is about Windows")
	}
}

// windowsAlive says whether a process with this id runs on Windows.
func windowsAlive(t *testing.T, pid string) bool {
	t.Helper()
	out, err := exec.Command("tasklist", "/FI", "PID eq "+pid, "/NH").Output()
	if err != nil {
		t.Fatalf("tasklist: %v", err)
	}
	for _, field := range strings.Fields(string(out)) {
		if field == pid {
			return true
		}
	}
	return false
}

// windowsKill ends a process on Windows.
func windowsKill(pid string) error {
	return exec.Command("taskkill", "/F", "/T", "/PID", pid).Run()
}

// alive says whether a process with this id exists on a POSIX system.
func alive(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
