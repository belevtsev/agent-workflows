package evidence_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// Requirement "Leftover processes", scenario "A timeout stops the whole
// process tree".
func TestTimeoutStopsTheWholeProcessTree(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	// Durations no other test uses, so that pgrep finds only these.
	const pattern = "sleep 6123[45]"
	killMatching(t, pattern)
	declare(t, repo, "[[check]]\nid = \"tree\"\ntimeout = \"2s\"\nrun = [\"sh\", \"-c\", \"sleep 61234 & sleep 61235\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "tree")

	if took > 10*time.Second {
		t.Errorf("cw run took %s, want at most 10s", took)
	}
	if outcome := lastRecord(t, repo, "tree")["outcome"]; outcome != "timed_out" {
		t.Errorf("tree ended %v, want timed_out", outcome)
	}
	if left := pgrep(t, 0, pattern); len(left) != 0 {
		t.Errorf("processes started by the check still run: %v", left)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", result.Code, result.Output())
	}
}

// Requirement "Leftover processes", scenario "A background process left
// behind".
func TestBackgroundProcessLeftBehindIsStoppedWithAWarning(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	const pattern = "sleep 61236"
	killMatching(t, pattern)
	declare(t, repo, "[[check]]\nid = \"bg\"\nrun = [\"sh\", \"-c\", \"sleep 61236 & exit 0\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "bg")

	if took > 10*time.Second {
		t.Errorf("cw run took %s, want at most 10s", took)
	}
	if outcome := lastRecord(t, repo, "bg")["outcome"]; outcome != "passed" {
		t.Errorf("bg ended %v, want passed", outcome)
	}
	lower := strings.ToLower(result.Output())
	if !strings.Contains(lower, "warn") || !strings.Contains(lower, "process") {
		t.Errorf("the output does not warn that a process was left behind")
	}
	if left := pgrep(t, 0, pattern); len(left) != 0 {
		t.Errorf("the sleep started in the background still runs: %v", left)
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", result.Code, result.Output())
	}
}

// Requirement "Leftover processes", scenario "A timeout stops the whole
// process tree on Windows".
func TestTimeoutStopsTheWholeProcessTreeOnWindows(t *testing.T) {
	onlyOnWindows(t)
	t.Parallel()
	repo := cwtest.Demo(t)
	pidFile := filepath.Join(t.TempDir(), "pids")
	// The program starts a second program; both write their process id and
	// neither ends for 600 seconds.
	repo.Write("tree.ps1", "$child = Start-Process -PassThru -NoNewWindow -FilePath powershell -ArgumentList '-NoProfile','-Command','Start-Sleep 600'\n"+
		"Set-Content -Path '"+pidFile+"' -Value \"$PID $($child.Id)\"\n"+
		"Start-Sleep 600\n")
	declare(t, repo, "[[check]]\nid = \"tree\"\ntimeout = \"2s\"\nrun = [\"powershell\", \"-NoProfile\", \"-ExecutionPolicy\", \"Bypass\", \"-File\", \"tree.ps1\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "tree")

	if took > 10*time.Second {
		t.Errorf("cw run took %s, want at most 10s", took)
	}
	if outcome := lastRecord(t, repo, "tree")["outcome"]; outcome != "timed_out" {
		t.Errorf("tree ended %v, want timed_out", outcome)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the program did not write its process ids: %v\n%s", err, result.Output())
	}
	for _, pid := range strings.Fields(string(data)) {
		t.Cleanup(func() { _ = windowsKill(pid) })
		if windowsAlive(t, pid) {
			t.Errorf("process %s started by the check still runs", pid)
		}
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", result.Code, result.Output())
	}
}
