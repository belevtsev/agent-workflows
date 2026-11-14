//go:build linux || darwin

package graphify

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// TestToolSessionHelper does nothing in a normal test run. Started by
// TestToolRunsInASessionOfItsOwn with "-- tool-session", it prints its pid and
// its session id and exits 0.
func TestToolSessionHelper(t *testing.T) {
	separator := slices.Index(os.Args, "--")
	if separator < 0 || len(os.Args) <= separator+1 || os.Args[separator+1] != "tool-session" {
		return
	}
	sid, err := unix.Getsid(0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%d %d\n", os.Getpid(), sid)
	os.Exit(0)
}

// Oracle: a Graphify tool process leads a session of its own, so it has no
// controlling terminal to read or signal: its session id is its pid and not
// the test's session id. A process started only in a group of its own
// (proc.Spec without Session) stays in the test's session. The tool reads its
// session while it runs and prints it.
func TestToolRunsInASessionOfItsOwn(t *testing.T) {
	own, err := unix.Getsid(0)
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	arguments := []string{binary, "-test.run=^TestToolSessionHelper$", "--", "tool-session"}
	code, stdout, stderr, err := runWithOutputLimit(t.Context(), arguments, t.TempDir(), pgFakeEnv(), pgRunTimeout, pgRunLimit)
	if err != nil || code != 0 {
		t.Fatalf("tool = code %d, error %v, stderr %q; want code 0", code, err, stderr)
	}
	fields := strings.Fields(stdout)
	if len(fields) != 2 {
		t.Fatalf("tool printed %q, want its pid and session id", stdout)
	}
	pid, pidErr := strconv.Atoi(fields[0])
	sid, sidErr := strconv.Atoi(fields[1])
	if pidErr != nil || sidErr != nil {
		t.Fatalf("tool printed %q, want two integers", stdout)
	}
	if sid != pid || sid == own {
		t.Fatalf("tool %d ran in session %d, want a session of its own (%d), not the test's (%d)", pid, sid, pid, own)
	}
}
