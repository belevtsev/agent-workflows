package taskflow

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// deref shows an exit code or its absence.
func deref(code *int) any {
	if code == nil {
		return "none"
	}
	return *code
}
