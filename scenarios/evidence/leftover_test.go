package evidence_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Leftover processes", scenario "A timeout stops the whole
// process tree".
func TestTimeoutStopsTheWholeProcessTree(t *testing.T) {
	skipOnWindows(t)
	t.Parallel()
	repo := kitsutest.Demo(t)
	// Durations no other test uses, so that pgrep finds only these.
	const pattern = "sleep 6123[45]"
	killMatching(t, pattern)
	declare(t, repo, "[[check]]\nid = \"tree\"\ntimeout = \"2s\"\nrun = [\"sh\", \"-c\", \"sleep 61234 & sleep 61235\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "tree")

	if took > 10*time.Second {
		t.Errorf("kitsu run took %s, want at most 10s", took)
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
	repo := kitsutest.Demo(t)
	const pattern = "sleep 61236"
	killMatching(t, pattern)
	declare(t, repo, "[[check]]\nid = \"bg\"\nrun = [\"sh\", \"-c\", \"sleep 61236 & exit 0\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "bg")

	if took > 10*time.Second {
		t.Errorf("kitsu run took %s, want at most 10s", took)
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
	repo := kitsutest.Demo(t)
	pidFile := filepath.Join(t.TempDir(), "pids")
	// The program starts a second program, writes both process ids, and
	// neither ends for 600 seconds. It is a compiled program, not a script:
	// under load PowerShell alone can take longer to start than the timeout.
	program := buildProgram(t, "kitsu-test-tree", treeProgram)
	declare(t, repo, "[[check]]\nid = \"tree\"\ntimeout = \"2s\"\nrun = [\""+filepath.ToSlash(program)+"\", \""+filepath.ToSlash(pidFile)+"\"]\n")

	result, took := runLimited(t, repo, 60*time.Second, "run", "tree")

	if took > 10*time.Second {
		t.Errorf("kitsu run took %s, want at most 10s", took)
	}
	if outcome := lastRecord(t, repo, "tree")["outcome"]; outcome != "timed_out" {
		t.Errorf("tree ended %v, want timed_out", outcome)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("the program did not write its process ids: %v\n%s", err, result.Output())
	}
	for _, pid := range strings.Fields(string(data)) {
		// kitsu has ended, so the id may already belong to another process.
		// Only one that still runs the check's program counts.
		if running, found := watchProcess(t, pid, filepath.Base(program)); found && running() {
			t.Errorf("process %s started by the check still runs", pid)
		}
	}
	if t.Failed() {
		t.Logf("output (exit status %d):\n%s", result.Code, result.Output())
	}
}

// treeProgram starts itself a second time, writes the process ids of both to
// the file its first argument names, and then both wait for 600 seconds.
const treeProgram = `package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

func main() {
	if len(os.Args) > 2 {
		time.Sleep(600 * time.Second)
		return
	}
	child := exec.Command(os.Args[0], "second", "program")
	if err := child.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ids := fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)
	if err := os.WriteFile(os.Args[1], []byte(ids), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	time.Sleep(600 * time.Second)
}
`

// buildProgram compiles a main package of one file and returns the program.
func buildProgram(t *testing.T, name, source string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, name+".go")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".exe")
	build := exec.Command("go", "build", "-o", out, src)
	build.Dir = dir
	build.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building %s: %v\n%s", name, err, output)
	}
	return out
}
