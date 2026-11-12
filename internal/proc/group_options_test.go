//go:build linux || darwin

package proc_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
	"github.com/belevtsev/agent-workflows/v3/internal/proc/proctest"
)

// guard bounds every wait in these tests; each program would run for 60 s.
const guard = 20 * time.Second

// stopWhenReady runs the shell script, whose $1 is a FIFO, under spec. Once
// the program writes its pid to the FIFO, it calls ready with that pid and
// cancels the run. It returns the result and how long Run took after the
// cancellation. Cleanup kills a group the test left behind.
func stopWhenReady(t *testing.T, script string, spec proc.Spec, ready func(pid int)) (proc.Result, time.Duration) {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "ready")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 1)
	go func() {
		line, _ := os.ReadFile(fifo)
		lines <- strings.TrimSpace(string(line))
	}()
	pid := 0
	t.Cleanup(func() {
		// Release a reader still waiting, then end what is left of the group.
		if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			writer.Close()
		}
		if pid > 1 {
			_ = proc.KillGroup(pid)
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	spec.Path, spec.Args = "/bin/sh", []string{"-c", script, "sh", fifo}
	results := make(chan proc.Result, 1)
	go func() { results <- proc.Run(ctx, spec) }()
	select {
	case line := <-lines:
		var err error
		if pid, err = strconv.Atoi(line); err != nil || pid <= 1 {
			t.Fatalf("the program reported %q, want its pid", line)
		}
	case got := <-results:
		t.Fatalf("Run returned before the program was ready: Code=%d Err=%v Stderr=%q", got.Code, got.Err, got.Stderr)
	case <-time.After(guard):
		t.Fatal("the program did not report ready")
	}
	ready(pid)
	stopped := time.Now()
	cancel()
	select {
	case got := <-results:
		took := time.Since(stopped)
		if live, err := proctest.GroupHasLiveMember(pid); err != nil || live {
			t.Errorf("after Run returned, group %d has a live member = %v (%v), want none", pid, live, err)
		}
		return got, took
	case <-time.After(guard):
		t.Fatalf("Run had not returned %v after its context was cancelled", guard)
	}
	return proc.Result{}, 0
}

// Oracle: with a Grace a stopped program and its background child first
// receive SIGTERM. A program that traps it runs its handler (SIGKILL cannot be
// trapped), whose output is kept, and Run returns once the group has emptied,
// long before the one-minute grace (the guard is 20 s). A program that ignores
// it, as its child then does, is killed once the grace has passed: Run returns
// no sooner than Grace after the cancellation, an exact lower bound, and long
// before the program's own 60 s. Either way no member of the group is alive
// afterwards and the stop reports no cleanup failure.
func TestRunStopsWithSIGTERMAndKillsAfterTheGrace(t *testing.T) {
	for _, c := range []struct {
		name, trap, output string
		grace, atLeast     time.Duration
	}{
		{"exits within the grace", `'echo terminated; exit 0'`, "terminated\n", time.Minute, 0},
		{"ignores SIGTERM", `''`, "", time.Second, time.Second},
	} {
		t.Run(c.name, func(t *testing.T) {
			script := "trap " + c.trap + " TERM\n/bin/sleep 60 &\necho \"$$\" > \"$1\"\nwait"
			got, took := stopWhenReady(t, script, proc.Spec{StdoutLimit: 64, Grace: c.grace}, func(int) {})
			if !got.Stopped || !got.Cancelled || got.Code != -1 || got.Cleanup != nil || string(got.Stdout) != c.output || took < c.atLeast {
				t.Errorf("Stopped=%v Cancelled=%v Code=%d Cleanup=%v output=%q, Run took %v after the cancellation; want a cancelled stop, %q, at least %v",
					got.Stopped, got.Cancelled, got.Code, got.Cleanup, got.Stdout, took, c.output, c.atLeast)
			}
		})
	}
}

// Oracle: with Session the program leads a session of its own, so its
// session id is its pid; without it, the program stays in the test's
// session. The session is read while the program runs.
func TestRunStartsASessionOnlyWhenAsked(t *testing.T) {
	own, err := unix.Getsid(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []bool{false, true} {
		stopWhenReady(t, `echo "$$" > "$1"; exec /bin/sleep 60`, proc.Spec{Session: session}, func(pid int) {
			sid, err := unix.Getsid(pid)
			want := own
			if session {
				want = pid
			}
			if err != nil || sid != want {
				t.Errorf("Session=%v: the program's session is %d (%v), want %d", session, sid, err, want)
			}
		})
	}
}

// Oracle: after the program exits 0, a member it left in its group is killed
// before Run returns, unless KeepGroupOnExit is set; then it is still alive.
// The program prints its pid, which is the group id.
func TestRunKeepsTheGroupAfterAnExitOnlyWhenAsked(t *testing.T) {
	const script = `/bin/sleep 60 </dev/null >/dev/null 2>&1 &
echo "$$"`
	for _, keep := range []bool{false, true} {
		got := proc.Run(t.Context(), proc.Spec{Path: "/bin/sh", Args: []string{"-c", script}, StdoutLimit: 64, KeepGroupOnExit: keep})
		pgid, err := strconv.Atoi(strings.TrimSpace(string(got.Stdout)))
		if err != nil || pgid <= 1 || got.Code != 0 || got.Err != nil || got.Cleanup != nil {
			t.Fatalf("KeepGroupOnExit=%v: output %q, Code=%d Err=%v Cleanup=%v, want the pid and exit 0", keep, got.Stdout, got.Code, got.Err, got.Cleanup)
		}
		t.Cleanup(func() { _ = proc.KillGroup(pgid) })
		live, err := proctest.GroupHasLiveMember(pgid)
		if err != nil || live != keep {
			t.Errorf("KeepGroupOnExit=%v: group %d has a live member = %v (%v), want %v", keep, pgid, live, err, keep)
		}
	}
}
