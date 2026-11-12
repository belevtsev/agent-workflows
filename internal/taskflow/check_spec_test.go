//go:build unix

package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// TestCheckSpecRunsTheArgvWithTheFilteredEnvironmentAndBounds pins how a
// check whose program is not go becomes a process, with no go command
// resolved. Oracle: the runner before proc.Run executed
// exec.CommandContext(argv[0], argv[1:]...) in the snapshot's selected path
// with checkEnvironment(check), no input, timeout_seconds as its bound and a 2 s
// WaitDelay, and gave both outputs to the log capture, which RunCheck attaches.
func TestCheckSpecRunsTheArgvWithTheFilteredEnvironmentAndBounds(t *testing.T) {
	t.Setenv("CW_SPEC_PROBE_TOKEN", "hidden")
	t.Setenv("CW_SPEC_PROBE_VISIBLE", "shown")
	check := Check{ID: "unit", Argv: []string{"make", "test", "-j", "4"}, TimeoutSeconds: 42}
	got, tool, err := checkSpec(t.Context(), check, "/repo/selected")
	want := proc.Spec{
		Path: "make", Args: []string{"test", "-j", "4"}, Dir: "/repo/selected", Env: checkEnvironment(check),
		Timeout: 42 * time.Second, WaitDelay: 2 * time.Second,
	}
	if err != nil || tool != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("checkSpec = %+v, %v, %v\nwant %+v, no tool, no error", got, tool, err, want)
	}
	if !slices.Contains(got.Env, "CW_SPEC_PROBE_VISIBLE=shown") {
		t.Errorf("environment lacks an ordinary variable: %q", got.Env)
	}
	if slices.ContainsFunc(got.Env, func(entry string) bool { return strings.HasPrefix(entry, "CW_SPEC_PROBE_TOKEN=") }) {
		t.Errorf("environment passes a TOKEN variable: %q", got.Env)
	}
}

// TestCheckSpecRunsAGoCheckWithTheVerifiedGoCommand gives a check whose
// program is go a PATH that names the real go and one that names a script.
// Oracle: G6's design: a go check runs the go command langpack.ResolveGo
// verified, by its absolute path, with GOROOT and GOTOOLCHAIN=local appended
// to the same environment, and returns that go command; when ResolveGo
// refuses, checkSpec returns its error and no go command.
func TestCheckSpecRunsAGoCheckWithTheVerifiedGoCommand(t *testing.T) {
	dir := t.TempDir()
	check := Check{ID: "unit", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 42}
	// go test puts the go command that runs it first on the test's PATH.
	want, err := langpack.ResolveGo(t.Context(), "go", dir, checkEnvironment(check))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	got, tool, err := checkSpec(t.Context(), check, dir)
	if err != nil || tool == nil || *tool != want {
		t.Fatalf("checkSpec tool = %+v, %v; want %+v", tool, err, want)
	}
	wantSpec := proc.Spec{
		Path: want.Path, Args: []string{"test", "./..."}, Dir: dir, Env: append(checkEnvironment(check), "GOROOT="+want.GOROOT, "GOTOOLCHAIN=local"),
		Timeout: 42 * time.Second, WaitDelay: 2 * time.Second,
	}
	if !reflect.DeepEqual(got, wantSpec) {
		t.Fatalf("checkSpec = %+v\nwant %+v", got, wantSpec)
	}

	fake := t.TempDir()
	if err := os.WriteFile(filepath.Join(fake, "go"), []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fake+string(os.PathListSeparator)+os.Getenv("PATH"))
	if _, tool, err := checkSpec(t.Context(), check, dir); err == nil || tool != nil || !strings.Contains(err.Error(), filepath.Join(fake, "go")) {
		t.Fatalf("checkSpec with a script first on PATH: tool %+v, error %v; want no tool and an error naming %s", tool, err, filepath.Join(fake, "go"))
	}
}

// TestRunCheckResolvesGoWithoutHoldingTheTaskLock blocks the go resolver of
// one check while another check of the same task runs to its terminal record.
// Oracle: review finding 2 on #119: resolving a go command (a slow shim, a
// toolchain download) happens before RunCheck takes the task lock, so the
// other run takes the lock twice and passes; the blocked run then records its
// refusal in its start revision.
func TestRunCheckResolvesGoWithoutHoldingTheTaskLock(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	plan.Checks = append(plan.Checks, Check{ID: "unit", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 10})
	plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, "unit")
	record, _ := fixtureTask(t, plan)
	entered, release := make(chan struct{}), make(chan struct{})
	resolveGo = func(context.Context, string, string, []string) (langpack.GoTool, error) {
		close(entered)
		<-release
		return langpack.GoTool{}, errors.New("refused by the test resolver")
	}
	t.Cleanup(func() { resolveGo = langpack.ResolveGo })
	type result struct {
		run CheckRun
		err error
	}
	blocked := make(chan result, 1)
	go func() {
		run, err := RunCheck(t.Context(), record.Directory, "unit")
		blocked <- result{run, err}
	}()
	select {
	case <-entered:
	case got := <-blocked:
		t.Fatalf("the go check ended before resolving its program: %+v, %v", got.run, got.err)
	}
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	close(release)
	if err != nil || run.Status != "passed" {
		t.Errorf("check run while go was resolving: %s, %v; want passed", run.Status, err)
	}
	got := <-blocked
	if got.run.Status != "failed" || got.run.Failure != "untrusted_toolchain" || !errors.Is(got.err, ErrCheckFailed) {
		t.Errorf("refused go check: %s/%s, %v; want failed/untrusted_toolchain and ErrCheckFailed", got.run.Status, got.run.Failure, got.err)
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
			spec, _, err := checkSpec(t.Context(), tc.check, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
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
			spec, _, err := checkSpec(t.Context(), Check{ID: "focused", Argv: []string{"/bin/sh", "-c", script}, TimeoutSeconds: 10}, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
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
