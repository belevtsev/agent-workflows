package commitgate_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Hooks decide from records", scenario "A denied commit starts
// no check".
func TestDeniedCommitStartsNoCheck(t *testing.T) {
	t.Parallel()
	g := newGate(t)
	// The scripts leave a mark outside the repository when they execute.
	marks := t.TempDir()
	g.Write("unit.sh", "touch '"+filepath.Join(marks, "unit.sh")+"'\n"+g.Read("unit.sh"))
	g.Write("lint.sh", "touch '"+filepath.Join(marks, "lint.sh")+"'\n"+g.Read("lint.sh"))
	g.commitNoVerify("scripts that leave a mark")
	g.checked()
	for _, name := range []string{"unit.sh", "lint.sh"} {
		if err := os.Remove(filepath.Join(marks, name)); err != nil {
			t.Fatalf("kitsu run did not execute %s: %v", name, err)
		}
	}

	g.Write("src/app.txt", "ok")
	g.commitDenied("pre-commit", "commit", "-a", "-m", "change")
	entries, err := os.ReadDir(marks)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		t.Errorf("%s was executed", entry.Name())
	}
}

// Requirement "Hooks decide from records", scenario "A running check does not
// hold up a commit".
func TestRunningCheckDoesNotHoldUpCommit(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "linux" {
		t.Skip("finds the check's process through /proc")
	}
	g := newGate(t)
	g.Write("kitsu.toml", g.Read("kitsu.toml")+"\n[[check]]\nid = \"slow\"\nrun = [\"sleep\", \"60\"]\n")
	g.commitNoVerify("a slow check")

	background := g.Command(filepath.Join(kitsutest.Binary(t), "kitsu"), "run")
	var output bytes.Buffer
	background.Stdout, background.Stderr = &output, &output
	if err := background.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = background.Wait()
		close(done)
	}()
	var sleeps []int
	t.Cleanup(func() {
		// Stop the background run and anything it left.
		_ = background.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = background.Process.Kill()
			<-done
		}
		for _, pid := range sleeps {
			if alive(pid) {
				t.Logf("the check's sleep (pid %d) outlived kitsu run; killing it", pid)
				if process, err := os.FindProcess(pid); err == nil {
					_ = process.Kill()
				}
			}
		}
		for _, pid := range sleeps {
			deadline := time.Now().Add(5 * time.Second)
			for alive(pid) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if alive(pid) {
				t.Errorf("the sleep with pid %d is still running", pid)
			}
		}
	})

	deadline := time.Now().Add(20 * time.Second)
	for len(sleeps) == 0 {
		sleeps = sleepsBelow(background.Process.Pid)
		if len(sleeps) > 0 {
			break
		}
		select {
		case <-done:
			t.Fatalf("kitsu run ended before the slow check ran:\n%s", output.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the slow check did not start within 20 seconds:\n%s", output.String())
		}
		time.Sleep(20 * time.Millisecond)
	}

	g.Write("src/app.txt", "ok")
	g.resetExits()
	start := time.Now()
	result := g.TryGit("commit", "-a", "-m", "change")
	elapsed := time.Since(start)
	if exits := g.hookExits("pre-commit"); len(exits) == 0 {
		t.Errorf("the hook did not decide:\n%s", result.Output())
	}
	if elapsed > 2*time.Second {
		t.Errorf("the hook took %v to decide, want at most 2s:\n%s", elapsed, result.Output())
	}
	select {
	case <-done:
		t.Errorf("the background kitsu run ended while its check should still sleep:\n%s", output.String())
	default:
	}
}

// sleepsBelow lists the processes named sleep that descend from pid.
func sleepsBelow(root int) []int {
	children := map[int][]int{}
	names := map[int]string{}
	entries, _ := os.ReadDir("/proc")
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "stat"))
		if err != nil {
			continue
		}
		// pid (comm) state ppid ...
		text := string(stat)
		open, end := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
		if open < 0 || end < open {
			continue
		}
		fields := strings.Fields(text[end+1:])
		if len(fields) < 2 {
			continue
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		children[ppid] = append(children[ppid], pid)
		names[pid] = text[open+1 : end]
	}
	var found []int
	queue := []int{root}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range children[pid] {
			if names[child] == "sleep" {
				found = append(found, child)
			}
			queue = append(queue, child)
		}
	}
	return found
}

// alive says whether a process exists and is not a zombie.
func alive(pid int) bool {
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	text := string(stat)
	end := strings.LastIndexByte(text, ')')
	fields := strings.Fields(text[end+1:])
	return len(fields) > 0 && fields[0] != "Z"
}
