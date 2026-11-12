//go:build unix

package projectctx

import (
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
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
