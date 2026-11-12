//go:build linux || darwin

package projectctx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc/proctest"
)

func TestNormalProbeExitStopsDetachedOwnedChildren(t *testing.T) {
	result, err := executeBounded(t.Context(), Command{Executable: "/bin/sh", Args: []string{"-c", "sleep 30 </dev/null >/dev/null 2>&1 & child=$!; printf '%s' \"$child\""}, Dir: t.TempDir(), Env: minimalEnvironment(os.Getenv)}, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(result.Stdout)))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Kill(pid, syscall.SIGKILL) //nolint:errcheck // best-effort cleanup; the process may already be gone
	deadline := time.Now().Add(time.Second)
	for {
		probeCtx, cancel := context.WithTimeout(t.Context(), time.Second)
		state, _ := exec.CommandContext(probeCtx, "ps", "-p", strconv.Itoa(pid), "-o", "stat=").Output()
		cancel()
		if value := strings.TrimSpace(string(state)); value == "" || strings.HasPrefix(value, "Z") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("normally exited probe left its owned child running")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

const (
	// forkingProbeScript records its pid, which is the probe's process group
	// id, starts forkingMemberScript ($2) with its output detached and then
	// waits like a server that never answers.
	forkingProbeScript = `echo "$$" > "$1/group"
/bin/sh -c "$2" sh "$1" </dev/null >/dev/null 2>&1 &
exec sleep 60`
	// forkingMemberScript keeps forking children that each fork a sleeper. Once
	// ten children exist it writes a line to the FIFO $1/ready, which blocks
	// until the test reads it, and then goes on forking for a few hundred
	// milliseconds, so forks are normally in flight when the test ends the
	// probe. Unstopped, every process ends within 60 s.
	forkingMemberScript = `i=0
while [ "$i" -lt 200 ]; do
	/bin/sh -c 'sleep 60 & wait' &
	i=$((i + 1))
	if [ "$i" -eq 10 ]; then echo forking > "$1/ready"; fi
done
wait`
)

func requireGroupLive(t *testing.T, pgid int, want bool, when string) {
	t.Helper()
	live, err := proctest.GroupHasLiveMember(pgid)
	if err != nil {
		t.Fatalf("%s: %v", when, err)
	}
	if live != want {
		t.Fatalf("%s: process group %d has a live member = %v, want %v", when, pgid, live, want)
	}
}

// Oracle: once a probe whose context ended while a member of its group was
// forking has returned, no process in the probe's group is alive (a zombie
// counts as dead), so no child of the probe outlives it. The control checks
// that the group was alive when the context ended.
func TestProbeEndedWhileForkingLeavesNoLiveProcessInItsGroup(t *testing.T) {
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready")
	if err := syscall.Mkfifo(ready, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ended := make(chan error, 1)
	go func() {
		_, err := executeBounded(ctx, Command{Executable: "/bin/sh", Args: []string{"-c", forkingProbeScript, "sh", dir, forkingMemberScript}, Dir: dir, Env: minimalEnvironment(os.Getenv)}, 1024)
		ended <- err
		// A probe that ended before its member opened the FIFO releases the
		// read below; otherwise no reader waits and this open fails.
		if writer, openErr := os.OpenFile(ready, os.O_WRONLY|syscall.O_NONBLOCK, 0); openErr == nil {
			writer.Close()
		}
	}()
	// The read returns once the member has written its line and closed the FIFO.
	if line, err := os.ReadFile(ready); err != nil || string(line) != "forking\n" {
		t.Fatalf("forking member did not signal that it was forking: %q, %v", line, err)
	}
	text, err := os.ReadFile(filepath.Join(dir, "group"))
	if err != nil {
		t.Fatal(err)
	}
	pgid, err := strconv.Atoi(strings.TrimSpace(string(text)))
	if err != nil {
		t.Fatal(err)
	}
	requireGroupLive(t, pgid, true, "before the probe's context ended")
	cancel()
	if err := <-ended; !errors.Is(err, context.Canceled) {
		t.Fatalf("probe error = %v, want context.Canceled", err)
	}
	requireGroupLive(t, pgid, false, "after the probe returned")
}
