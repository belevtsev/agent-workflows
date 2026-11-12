//go:build unix

package taskflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// TestCheckSpecRunsTheArgvWithTheFilteredEnvironmentAndBounds pins how a
// check becomes a process. Oracle: the runner before proc.Run executed
// exec.CommandContext(argv[0], argv[1:]...) in the snapshot's selected path
// with checkEnvironment(), no input, timeout_seconds as its bound and a 2 s
// WaitDelay, and gave both outputs to the log capture, which RunCheck attaches.
func TestCheckSpecRunsTheArgvWithTheFilteredEnvironmentAndBounds(t *testing.T) {
	t.Setenv("CW_SPEC_PROBE_TOKEN", "hidden")
	t.Setenv("CW_SPEC_PROBE_VISIBLE", "shown")
	check := Check{ID: "unit", Argv: []string{"go", "test", "-run", "TestX", "./..."}, TimeoutSeconds: 42}
	got := checkSpec(check, "/repo/selected")
	want := proc.Spec{
		Path: "go", Args: []string{"test", "-run", "TestX", "./..."}, Dir: "/repo/selected", Env: checkEnvironment(),
		Timeout: 42 * time.Second, WaitDelay: 2 * time.Second,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checkSpec = %+v\nwant %+v", got, want)
	}
	if !slices.Contains(got.Env, "CW_SPEC_PROBE_VISIBLE=shown") {
		t.Errorf("environment lacks an ordinary variable: %q", got.Env)
	}
	if slices.ContainsFunc(got.Env, func(entry string) bool { return strings.HasPrefix(entry, "CW_SPEC_PROBE_TOKEN=") }) {
		t.Errorf("environment passes a TOKEN variable: %q", got.Env)
	}
}

// TestCheckExitCodeRecordsOnlyAStartedProgramsStatus runs real programs
// through proc.Run as RunCheck does. Oracle: the runner before proc.Run
// recorded os.ProcessState.ExitCode() whenever the program had started (its
// status, or -1 once killed) and no exit code when it never started.
func TestCheckExitCodeRecordsOnlyAStartedProgramsStatus(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := func(mode string) Check {
		return Check{ID: "focused", Argv: []string{executable, "-test.run=^TestCheckSubprocess$", "--", mode}, TimeoutSeconds: 10}
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	missing := Check{ID: "focused", Argv: []string{filepath.Join(t.TempDir(), "missing")}, TimeoutSeconds: 10}
	for _, tc := range []struct {
		name    string
		ctx     context.Context
		check   Check
		timeout time.Duration
		want    *int
	}{
		{name: "clean exit", ctx: t.Context(), check: helper("pass"), want: new(0)},
		{name: "exit status", ctx: t.Context(), check: helper("fail"), want: new(9)},
		{name: "stopped at the timeout", ctx: t.Context(), check: helper("wait"), timeout: 100 * time.Millisecond, want: new(-1)},
		{name: "program missing", ctx: t.Context(), check: missing},
		{name: "stopped before the start", ctx: cancelled, check: helper("pass")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := checkSpec(tc.check, t.TempDir())
			if tc.timeout != 0 {
				spec.Timeout = tc.timeout
			}
			got := proc.Run(tc.ctx, spec)
			if code := checkExitCode(got); !reflect.DeepEqual(code, tc.want) {
				t.Fatalf("checkExitCode = %v, want %v (result %+v)", deref(code), deref(tc.want), got)
			}
		})
	}
}

// TestCheckExitCodeKeepsTheStatusOfAProgramThatExitedBeforeTheTimeout runs a
// check that exits at once and leaves a background child holding its output,
// so the timeout stops the run during the output wait, after the exit.
// Oracle: the runner before proc.Run recorded os.ProcessState.ExitCode(),
// which is the program's own status for such a run (3 and 0 here), not -1 and
// not nothing.
func TestCheckExitCodeKeepsTheStatusOfAProgramThatExitedBeforeTheTimeout(t *testing.T) {
	for _, status := range []int{3, 0} {
		t.Run(fmt.Sprintf("exits %d", status), func(t *testing.T) {
			// sh prints its pid, which is the id of the check's process group,
			// and exits; the background sleep holds the output pipe open.
			script := fmt.Sprintf("sleep 30 & echo $$; exit %d", status)
			spec := checkSpec(Check{ID: "focused", Argv: []string{"/bin/sh", "-c", script}, TimeoutSeconds: 10}, t.TempDir())
			// sh exits within milliseconds of its start, so the timeout falls
			// inside checkSpec's 2 s output wait.
			spec.Timeout, spec.StdoutLimit = 500*time.Millisecond, 64
			got := proc.Run(t.Context(), spec)
			pgid, err := strconv.Atoi(strings.TrimSpace(string(got.Stdout)))
			if err != nil {
				t.Fatalf("check printed %q, want its process group id", got.Stdout)
			}
			// proc.Run kills the group, the sleep with it, before it returns. A
			// group still holding a live member here is killed and fails the
			// test; only a group shown to be in use is signalled.
			t.Cleanup(func() {
				live, probeErr := pgGroupHasLiveMember(pgid)
				switch {
				case probeErr != nil:
					t.Errorf("probe check process group %d: %v", pgid, probeErr)
				case live:
					t.Errorf("check process group %d outlived the run; killing it: %v", pgid, proc.KillGroup(pgid))
				}
			})
			if !got.Stopped || got.Cancelled {
				t.Fatalf("precondition: the timeout did not stop the run: %+v", got)
			}
			if code := checkExitCode(got); code == nil || *code != status {
				t.Fatalf("checkExitCode = %v, want %d (result %+v)", deref(code), status, got)
			}
		})
	}
}

// deref shows an exit code or its absence.
func deref(code *int) any {
	if code == nil {
		return "none"
	}
	return *code
}
