//go:build unix

package taskflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
	got, tool, goEnv, err := checkSpec(t.Context(), check, "/repo/selected")
	want := proc.Spec{
		Path: "make", Args: []string{"test", "-j", "4"}, Dir: "/repo/selected", Env: checkEnvironment(check),
		Timeout: 42 * time.Second, WaitDelay: 2 * time.Second,
	}
	if err != nil || tool != nil || goEnv != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("checkSpec = %+v, %v, %v, %v\nwant %+v, no tool, no go env, no error", got, tool, goEnv, err, want)
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
// to the same environment, and returns that go command and (F1) what
// langpack.ReadGoEnv reports for it under that environment, whose pin of
// GOFLAGS, GOOS and GOARCH ends the run's environment; when ResolveGo
// refuses, checkSpec returns its error and neither.
func TestCheckSpecRunsAGoCheckWithTheVerifiedGoCommand(t *testing.T) {
	dir := t.TempDir()
	check := Check{ID: "unit", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 42}
	// go test puts the go command that runs it first on the test's PATH.
	want, err := langpack.ResolveGo(t.Context(), "go", dir, checkEnvironment(check))
	if err != nil {
		t.Fatalf("precondition: the real go does not verify: %v", err)
	}
	got, tool, goEnv, err := checkSpec(t.Context(), check, dir)
	if err != nil || tool == nil || *tool != want {
		t.Fatalf("checkSpec tool = %+v, %v; want %+v", tool, err, want)
	}
	verified := append(checkEnvironment(check), "GOROOT="+want.GOROOT, "GOTOOLCHAIN=local")
	read, pin, err := langpack.ReadGoEnv(t.Context(), want, dir, verified)
	if err != nil || goEnv == nil || *goEnv != read {
		t.Fatalf("checkSpec go env = %+v, want %+v (%v)", goEnv, read, err)
	}
	wantSpec := proc.Spec{
		Path: want.Path, Args: []string{"test", "./..."}, Dir: dir, Env: append(verified, pin...),
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
	if _, tool, goEnv, err := checkSpec(t.Context(), check, dir); err == nil || tool != nil || goEnv != nil || !strings.Contains(err.Error(), filepath.Join(fake, "go")) {
		t.Fatalf("checkSpec with a script first on PATH: tool %+v, go env %+v, error %v; want neither and an error naming %s", tool, goEnv, err, filepath.Join(fake, "go"))
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

// TestRunCheckRefusesAGoCheckWhoseGoEnvCannotBeRead verifies the go command
// of a go check, which would create marker if it ran, and fails to read its
// go env. Oracle (F1): without the configuration the go command applies, cw
// gives the check no credit by not running it: the run is failed with failure
// untrusted_toolchain, ErrCheckFailed names the go env error, the stored run
// has no tool, no go env and an empty log, and the go command never ran.
func TestRunCheckRefusesAGoCheckWhoseGoEnvCannotBeRead(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	plan.Checks = append(plan.Checks, Check{ID: "unit", Argv: []string{"go", "test", "./..."}, TimeoutSeconds: 10})
	plan.Requirements[0].CheckIDs = append(plan.Requirements[0].CheckIDs, "unit")
	record, _ := fixtureTask(t, plan)
	marker := filepath.Join(t.TempDir(), "ran")
	program := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(program, []byte("#!/bin/sh\ntouch '"+marker+"'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	resolveGo = func(context.Context, string, string, []string) (langpack.GoTool, error) {
		return langpack.GoTool{Path: program, Version: "go1.27.4", GOROOT: filepath.Dir(program)}, nil
	}
	readGoEnv = func(context.Context, langpack.GoTool, string, []string) (langpack.GoEnv, []string, error) {
		return langpack.GoEnv{}, nil, errors.New("cannot read the configuration of the go command: test reader")
	}
	t.Cleanup(func() { resolveGo, readGoEnv = langpack.ResolveGo, langpack.ReadGoEnv })
	run, err := RunCheck(t.Context(), record.Directory, "unit")
	if run.Status != "failed" || run.Failure != "untrusted_toolchain" || !errors.Is(err, ErrCheckFailed) || !strings.Contains(err.Error(), "test reader") {
		t.Fatalf("RunCheck = %s/%s, %v; want failed/untrusted_toolchain and ErrCheckFailed naming the go env error", run.Status, run.Failure, err)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	stored := loaded.Checks[len(loaded.Checks)-1]
	if stored.Tool != nil || stored.GoEnv != nil || stored.ExitCode != nil || stored.LogSHA256 != digest(nil) {
		t.Errorf("stored refused run %+v, want no tool, no go env, no exit code and an empty log", stored)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the go command ran although its go env could not be read: %v", err)
	}
}

// TestCheckSpecHoldsTheRunToTheGoEnvItRead runs a go test check of a test
// that fails whenever it runs, with GOENV naming an empty go env file that
// the go env read sees and that is then rewritten, before the run reads it
// again, to GOFLAGS=-exec=/bin/true and another GOARCH. Oracle: review
// finding 1 on #139: cmd/go takes a GOFLAGS, GOOS or GOARCH that is not
// empty from the environment over any go env file, so with go_env's values
// at the end of the run's environment the run builds for the host, runs the
// test and fails, and the go command reports what go_env recorded.
func TestCheckSpecHoldsTheRunToTheGoEnvItRead(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":       "module example.com/pinned\n\ngo 1.21\n",
		"fail_test.go": "package pinned\n\nimport \"testing\"\n\nfunc TestFails(t *testing.T) { t.Fatal(\"ran\") }\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOENV", file)
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOOS", "")
	t.Setenv("GOARCH", "")
	other := "arm64"
	if runtime.GOARCH == "arm64" {
		other = "amd64"
	}
	readGoEnv = func(ctx context.Context, tool langpack.GoTool, dir string, env []string) (langpack.GoEnv, []string, error) {
		read, pin, err := langpack.ReadGoEnv(ctx, tool, dir, env)
		if err := os.WriteFile(file, []byte("GOFLAGS=-exec=/bin/true\nGOARCH="+other+"\n"), 0o644); err != nil {
			t.Error(err)
		}
		return read, pin, err
	}
	t.Cleanup(func() { readGoEnv = langpack.ReadGoEnv })
	check := Check{ID: "unit", Argv: []string{"go", "test", "-count=1", "./..."}, TimeoutSeconds: 120}
	spec, tool, goEnv, err := checkSpec(t.Context(), check, dir)
	if err != nil || tool == nil || goEnv == nil {
		t.Fatalf("checkSpec: tool %+v, go env %+v, %v", tool, goEnv, err)
	}
	want := langpack.GoEnv{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, GOHOSTOS: runtime.GOOS, GOHOSTARCH: runtime.GOARCH}
	if *goEnv != want {
		t.Fatalf("precondition: go env read before the rewrite = %+v, want %+v", *goEnv, want)
	}
	if got, _, err := langpack.ReadGoEnv(t.Context(), *tool, dir, spec.Env); err != nil || got != *goEnv {
		t.Errorf("the run's go command reports %+v (%v), want what go_env recorded, %+v", got, err, *goEnv)
	}
	spec.StdoutLimit, spec.StderrLimit = 1<<20, 1<<20
	got := proc.Run(t.Context(), spec)
	if got.Err == nil && got.Code == 0 || !strings.Contains(string(got.Stdout), "ran") {
		t.Errorf("go test of a failing test under the rewritten go env file exited %d (%v), output %q; want the test to run and fail", got.Code, got.Err, got.Stdout)
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
			spec, _, _, err := checkSpec(t.Context(), tc.check, t.TempDir())
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
			spec, _, _, err := checkSpec(t.Context(), Check{ID: "focused", Argv: []string{"/bin/sh", "-c", script}, TimeoutSeconds: 10}, t.TempDir())
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
