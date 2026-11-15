//go:build windows

package proc_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/belevtsev/gatecub/v3/internal/proc"
)

// roleEnv makes a re-executed test binary act as a helper program instead of
// running the tests:
//   - "sleep" sleeps for a minute;
//   - "spawn-exit" starts a "sleep" child, prints its pid, and exits;
//   - "spawn-wait" starts a "sleep" child, prints its pid, and sleeps.
const roleEnv = "GATECUB_PROC_TEST_ROLE"

func TestMain(m *testing.M) {
	if role := os.Getenv(roleEnv); role != "" {
		os.Exit(act(role))
	}
	os.Exit(m.Run())
}

func act(role string) int {
	switch role {
	case "sleep":
		time.Sleep(time.Minute)
		return 0
	case "spawn-exit", "spawn-wait":
		exe, err := os.Executable()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		// The child gets no stdout, so it does not hold the run's pipe open.
		child := exec.Command(exe, "-test.run=^$")
		child.Env = helperEnv("sleep")
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		fmt.Println(child.Process.Pid)
		if role == "spawn-wait" {
			time.Sleep(time.Minute)
		}
		return 0
	}
	fmt.Fprintln(os.Stderr, "unknown role", role)
	return 2
}

// helperEnv is the environment of a helper acting as role.
func helperEnv(role string) []string {
	return []string{roleEnv + "=" + role, "SYSTEMROOT=" + os.Getenv("SYSTEMROOT")}
}

// childWatch is the program's stdout. When the program prints its child's
// pid, it opens a handle to the child at once, while the child is known to
// run, so a later check cannot hit a reused pid, and then calls onChild.
type childWatch struct {
	mu      sync.Mutex
	out     []byte
	child   windows.Handle
	err     error
	onChild func()
}

func (w *childWatch) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.out = append(w.out, p...)
	if w.child != 0 || w.err != nil {
		return len(p), nil
	}
	line, _, found := bytes.Cut(w.out, []byte("\n"))
	if !found {
		return len(p), nil
	}
	pid, err := strconv.ParseUint(string(bytes.TrimSpace(line)), 10, 32)
	if err == nil {
		w.child, err = windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	}
	w.err = err
	if w.onChild != nil {
		w.onChild()
	}
	return len(p), nil
}

// childHandle returns the handle opened to the child, failing the test when
// the program never reported one, and closes it at the end of the test.
func (w *childWatch) childHandle(t *testing.T, got proc.Result) windows.Handle {
	t.Helper()
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.child == 0 {
		t.Fatalf("program reported no child (output %q, err %v); run: Code=%d Err=%v Stderr=%q", w.out, w.err, got.Code, got.Err, got.Stderr)
	}
	child := w.child
	t.Cleanup(func() { _ = windows.CloseHandle(child) })
	return child
}

// assertExited fails the test when the process has not exited.
func assertExited(t *testing.T, process windows.Handle) {
	t.Helper()
	event, err := windows.WaitForSingleObject(process, 0)
	if err != nil {
		t.Fatalf("wait for child: %v", err)
	}
	if event != windows.WAIT_OBJECT_0 {
		t.Fatal("the program's child was still running after Run returned")
	}
}

func helperSpec(t *testing.T, role string, stdout *childWatch) proc.Spec {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return proc.Spec{Path: exe, Args: []string{"-test.run=^$"}, Env: helperEnv(role), Stdout: stdout, StderrLimit: 4096}
}

// Oracle: a program that starts a child and exits on its own keeps its exit
// status, and its child is no longer running when Run returns.
func TestRunLeavesNoChildAfterTheProgramExits(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	watch := &childWatch{}
	got := proc.Run(ctx, helperSpec(t, "spawn-exit", watch))
	child := watch.childHandle(t, got)
	if got.Err != nil || got.Code != 0 || got.Stopped || got.Cleanup != nil {
		t.Fatalf("Code=%d Err=%v Stopped=%v Cleanup=%v Stderr=%q, want a clean exit", got.Code, got.Err, got.Stopped, got.Cleanup, got.Stderr)
	}
	assertExited(t, child)
}

// Oracle: a program that outlives its Timeout is stopped as out of time
// (Stopped, not Cancelled), long before its own minute, and its child is no
// longer running when Run returns.
func TestRunStopsAProgramPastItsTimeoutTogetherWithItsChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Second)
	defer cancel()
	watch := &childWatch{}
	spec := helperSpec(t, "spawn-wait", watch)
	spec.Timeout = 5 * time.Second
	got := proc.Run(ctx, spec)
	child := watch.childHandle(t, got)
	if !got.Stopped || got.Cancelled || got.Cleanup != nil || ctx.Err() != nil {
		t.Fatalf("Stopped=%v Cancelled=%v Cleanup=%v ctx=%v, want stopped by its timeout well within the guard", got.Stopped, got.Cancelled, got.Cleanup, ctx.Err())
	}
	assertExited(t, child)
}

// Oracle: cancelling ctx once the program has started its child stops the
// program as Cancelled, and its child is no longer running when Run returns.
func TestRunStopsTheProgramAndItsChildWhenTheContextIsCancelled(t *testing.T) {
	guard, stopGuard := context.WithTimeout(t.Context(), 30*time.Second)
	defer stopGuard()
	ctx, cancel := context.WithCancel(guard)
	defer cancel()
	watch := &childWatch{onChild: cancel}
	got := proc.Run(ctx, helperSpec(t, "spawn-wait", watch))
	child := watch.childHandle(t, got)
	if !got.Stopped || !got.Cancelled || got.Cleanup != nil || guard.Err() != nil {
		t.Fatalf("Stopped=%v Cancelled=%v Cleanup=%v guard=%v, want stopped by the cancellation", got.Stopped, got.Cancelled, got.Cleanup, guard.Err())
	}
	assertExited(t, child)
}
