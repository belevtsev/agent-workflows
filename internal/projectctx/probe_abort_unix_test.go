//go:build unix

package projectctx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Tests for contract probe-abort revision 2, requirements R1-R7 and R10,
// driven through capture(ctx, repo, budget).
//
// TestMain puts one fake git first on PATH for the whole package. The fake
// passes every call to the real git unless the call's `-C <dir>` holds a
// control directory <dir>/.git/pa-fake. Then it logs the call there and, when
// the probe words equal the control file "target", touches "reached" and
// performs the control file "action". Each test therefore sets its fake's
// behaviour inside its own repository, and per-probe subtests run in
// parallel. Processes a fake leaves behind are listed in "pids" and killed in
// cleanup.

// paProbes are capture's six probes in the agreed order: the words after
// `-C <dir>`, joined by single spaces.
var paProbes = []string{
	"rev-parse --show-toplevel",
	"rev-parse --verify HEAD",
	"symbolic-ref --short -q HEAD",
	"ls-files --stage -z --cached",
	"ls-tree -r -z HEAD",
	"ls-files -z --others --exclude-standard",
}

// paProbePrefix starts every capture probe's argument list.
const paProbePrefix = "-c core.fsmonitor=false -c core.untrackedCache=false -C "

// paFolderOnlyWarning is the R4 warning for a folder without Git identity.
const paFolderOnlyWarning = "Git identity is unavailable; content identity covers the selected folder only"

// Timing (contract timing rules): every budget or deadline that also bounds
// real Git calls is at least 10 seconds, and elapsed-time bounds are only hang
// guards with at least 20 seconds of slack.
const (
	paTimeoutBudget  = 10 * time.Second // R1: waited for in full
	paRoomyBudget    = 30 * time.Second // cases where the budget must not fire
	paCallerDeadline = 10 * time.Second // R3 deadline case
	paHangSeconds    = "120"            // a hanging probe outlives every bound
	paLingerSeconds  = "60"             // R10 child holds stdout well past 3 s
	paGuardSlack     = 20 * time.Second
	paReachGuard     = 60 * time.Second // waiting for a fake's marker
)

// Set by TestMain.
var (
	paRealGit      string
	paOriginalPath string
	paSetupError   error
)

func TestMain(m *testing.M) {
	os.Exit(paMain(m))
}

func paMain(m *testing.M) int {
	paOriginalPath = os.Getenv("PATH")
	realGit, gitErr := exec.LookPath("git")
	sleep, sleepErr := exec.LookPath("sleep")
	if err := errors.Join(gitErr, sleepErr); err != nil {
		paSetupError = err
		return m.Run()
	}
	paRealGit = realGit
	dir, err := os.MkdirTemp("", "probe-abort-fake-git")
	if err != nil {
		paSetupError = err
		return m.Run()
	}
	defer os.RemoveAll(dir)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(paFakeScript(realGit, sleep, paOriginalPath)), 0o755); err != nil {
		paSetupError = err
		return m.Run()
	}
	os.Setenv("PATH", dir+string(os.PathListSeparator)+paOriginalPath)
	return m.Run()
}

func paQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func paFakeScript(realGit, sleep, path string) string {
	git := paQuote(realGit)
	sl := paQuote(sleep)
	return strings.Join([]string{
		"#!/bin/sh",
		"sig=''",
		"dir=''",
		"state=0",
		`for a in "$@"; do`,
		"\tcase $state in",
		`	0) if [ "$a" = -C ]; then state=1; fi ;;`,
		`	1) dir=$a; state=2 ;;`,
		`	2) if [ -z "$sig" ]; then sig=$a; else sig="$sig $a"; fi ;;`,
		"\tesac",
		"done",
		"PATH=" + paQuote(path),
		"export PATH",
		`ctl="$dir/.git/pa-fake"`,
		`if [ -n "$dir" ] && [ -d "$ctl" ]; then`,
		"\tGIT_CONFIG_GLOBAL=/dev/null",
		"\tGIT_CONFIG_NOSYSTEM=1",
		"\texport GIT_CONFIG_GLOBAL GIT_CONFIG_NOSYSTEM",
		`	printf '%s\n' "$*" >> "$ctl/log"`,
		`	if [ -f "$ctl/target" ] && [ "$sig" = "$(cat "$ctl/target")" ]; then`,
		`		case "$(cat "$ctl/action")" in`,
		"\t\thang)",
		`			echo $$ >> "$ctl/pids"`,
		`			: > "$ctl/reached"`,
		"\t\t\texec " + sl + " " + paHangSeconds + " ;;",
		"\t\tkill)",
		`			: > "$ctl/reached"`,
		"\t\t\tkill -KILL $$ ;;",
		"\t\tfail)",
		`			: > "$ctl/reached"`,
		"\t\t\techo 'fatal: not a git repository (or any of the parent directories): .git' >&2",
		"\t\t\texit 1 ;;",
		"\t\tlinger)",
		"\t\t\t" + sl + " " + paLingerSeconds + " &",
		`			echo $! >> "$ctl/pids"`,
		`			: > "$ctl/reached"`,
		"\t\t\texec " + git + ` "$@" ;;`,
		"\t\tesac",
		"\tfi",
		"fi",
		"exec " + git + ` "$@"`,
		"",
	}, "\n")
}

func paRequireFake(t *testing.T) {
	t.Helper()
	if paSetupError != nil {
		t.Skipf("fake git is unavailable: %v", paSetupError)
	}
}

func paTempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// paGit runs the real git in dir without user or system configuration.
func paGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.CommandContext(t.Context(), paRealGit, append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "init.defaultBranch=main", "-c", "commit.gpgsign=false"}, args...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "PATH="+paOriginalPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

// paCommittedRepo creates a repository with one commit of tracked.txt on
// branch, plus an untracked file, and returns its canonical path.
func paCommittedRepo(t *testing.T, branch string) string {
	t.Helper()
	repo := paTempDir(t)
	paGit(t, repo, "init", "-q")
	if err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("tracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	paGit(t, repo, "add", "tracked.txt")
	paGit(t, repo, "commit", "-q", "-m", "init")
	paGit(t, repo, "checkout", "-q", "-b", branch)
	if err := os.WriteFile(filepath.Join(repo, "untracked.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

// paControl is one repository's fake-git control directory.
type paControl string

// paConfigure makes the fake git perform action ("hang", "kill", "fail",
// "linger") for probe in repo, or only log every call when action is "". It
// kills every process the fake records in cleanup.
func paConfigure(t *testing.T, repo, probe, action string) paControl {
	t.Helper()
	ctl := paControl(filepath.Join(repo, ".git", "pa-fake"))
	if err := os.Mkdir(string(ctl), 0o755); err != nil {
		t.Fatal(err)
	}
	if action != "" {
		if err := os.WriteFile(ctl.file("target"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ctl.file("action"), []byte(action), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(ctl.killLeftovers)
	return ctl
}

func (c paControl) file(name string) string { return filepath.Join(string(c), name) }

func (c paControl) killLeftovers() {
	data, err := os.ReadFile(c.file("pids"))
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(data)) {
		if pid, err := strconv.Atoi(field); err == nil && pid > 1 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}
}

func (c paControl) reached() bool {
	_, err := os.Stat(c.file("reached"))
	return err == nil
}

func (c paControl) requireReached(t *testing.T) {
	t.Helper()
	if !c.reached() {
		t.Fatal("capture never started the targeted probe")
	}
}

func (c paControl) invocations(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(c.file("log"))
	if err != nil {
		t.Fatalf("the fake git was never run for this repository: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

type paResult struct {
	got captured
	err error
}

// paCaptureAsync runs capture in the background.
func paCaptureAsync(ctx context.Context, repo string, budget time.Duration) <-chan paResult {
	done := make(chan paResult, 1)
	go func() {
		got, err := capture(ctx, repo, budget)
		done <- paResult{got, err}
	}()
	return done
}

// paAwait returns capture's result, failing if it takes longer than guard.
func paAwait(t *testing.T, done <-chan paResult, guard time.Duration) paResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(guard):
		t.Fatalf("capture did not return within the %v hang guard", guard)
		return paResult{}
	}
}

// paExpectZero requires the zero captured value that goes with an abort.
func paExpectZero(t *testing.T, got captured) {
	t.Helper()
	if !reflect.ValueOf(got).IsZero() {
		t.Fatalf("capture returned %+v with its error, want the zero value", got)
	}
}

// paExpectFolderOnly requires the R4 outcome.
func paExpectFolderOnly(t *testing.T, got captured, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("capture returned %v, want nil", err)
	}
	if got.snapshot.Head != "" || got.snapshot.Branch != "" {
		t.Fatalf("snapshot Head %q Branch %q, want both empty", got.snapshot.Head, got.snapshot.Branch)
	}
	if got.snapshot.Fingerprint == "" {
		t.Fatal("snapshot Fingerprint is empty, want the folder's content identity")
	}
	if !slices.Contains(got.warnings, paFolderOnlyWarning) {
		t.Fatalf("warnings %q do not include %q", got.warnings, paFolderOnlyWarning)
	}
}

// Interface check (supports R1-R3, R6, R7, R10): capture runs every agreed
// probe, in order, with the agreed leading arguments, so the fake's targets
// are real probes.
func TestProbeAbortInvocationInterface(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	repo := paCommittedRepo(t, "probe-branch")
	ctl := paConfigure(t, repo, "", "")

	if _, err := capture(t.Context(), repo, paRoomyBudget); err != nil {
		t.Fatalf("capture through the logging fake: %v", err)
	}
	lines := ctl.invocations(t)
	next := 0
	for _, line := range lines {
		if !strings.HasPrefix(line, paProbePrefix) {
			t.Fatalf("probe %q does not start with %q", line, paProbePrefix)
		}
		if next < len(paProbes) && strings.HasSuffix(line, " "+paProbes[next]) {
			next++
		}
	}
	if next != len(paProbes) {
		t.Fatalf("capture ran %q; probe %q is missing or out of order", lines, paProbes[next])
	}
}

// R1: a probe that outlives the budget aborts capture with a zero value and
// the timeout message naming the whole probe, for each of the six probes.
// The six captures run at once in goroutines, so the suite waits for one
// budget whatever the -parallel limit.
func TestProbeAbortTimeoutNamesProbe(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	type run struct {
		ctl  paControl
		done <-chan paResult
	}
	runs := make([]run, len(paProbes))
	for i, probe := range paProbes {
		repo := paCommittedRepo(t, "probe-branch")
		ctl := paConfigure(t, repo, probe, "hang")
		runs[i] = run{ctl, paCaptureAsync(t.Context(), repo, paTimeoutBudget)}
	}
	for i, probe := range paProbes {
		t.Run(probe, func(t *testing.T) {
			result := paAwait(t, runs[i].done, paTimeoutBudget+paGuardSlack)

			runs[i].ctl.requireReached(t)
			want := fmt.Sprintf("Git inspection timed out after %s (git %s)", paTimeoutBudget.String(), probe)
			if result.err == nil || result.err.Error() != want {
				t.Fatalf("capture error %v, want %q", result.err, want)
			}
			paExpectZero(t, result.got)
		})
	}
}

// R2: a probe whose git process is ended by a signal aborts capture with a
// zero value and the killed message naming the whole probe, for each of the
// six probes.
func TestProbeAbortKilledNamesProbe(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	for _, probe := range paProbes {
		t.Run(probe, func(t *testing.T) {
			t.Parallel()
			repo := paCommittedRepo(t, "probe-branch")
			ctl := paConfigure(t, repo, probe, "kill")

			result := paAwait(t, paCaptureAsync(t.Context(), repo, paRoomyBudget), paRoomyBudget+paGuardSlack)

			ctl.requireReached(t)
			want := fmt.Sprintf("Git inspection was killed before it finished (git %s)", probe)
			if result.err == nil || result.err.Error() != want {
				t.Fatalf("capture error %v, want %q", result.err, want)
			}
			paExpectZero(t, result.got)
		})
	}
}

// R3: when the caller's context ends while a probe runs, capture's error
// matches the context's error and names the whole probe. Cancellation is
// tested for each of the six probes, once the probe has started; one case
// uses a deadline instead.
func TestProbeAbortCallerEnds(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	for _, probe := range paProbes {
		t.Run("cancel/"+probe, func(t *testing.T) {
			t.Parallel()
			repo := paCommittedRepo(t, "probe-branch")
			ctl := paConfigure(t, repo, probe, "hang")
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			done := paCaptureAsync(ctx, repo, paRoomyBudget)
			waitUntil := time.Now().Add(paReachGuard)
			for !ctl.reached() {
				select {
				case result := <-done:
					t.Fatalf("capture returned (%+v, %v) before the targeted probe started", result.got, result.err)
				case <-time.After(10 * time.Millisecond):
				}
				if time.Now().After(waitUntil) {
					t.Fatalf("the targeted probe did not start within %v", paReachGuard)
				}
			}
			cancel()
			result := paAwait(t, done, paGuardSlack)

			if !errors.Is(result.err, context.Canceled) {
				t.Fatalf("capture error %v, want one matching context.Canceled", result.err)
			}
			want := fmt.Sprintf("Git inspection stopped by the caller (git %s): %s", probe, ctx.Err().Error())
			if result.err.Error() != want {
				t.Fatalf("capture error %q, want %q", result.err.Error(), want)
			}
		})
	}
	// The first probe runs no real Git before it, so only the hanging probe
	// is under the deadline.
	t.Run("deadline/"+paProbes[0], func(t *testing.T) {
		t.Parallel()
		probe := paProbes[0]
		repo := paCommittedRepo(t, "probe-branch")
		ctl := paConfigure(t, repo, probe, "hang")
		ctx, cancel := context.WithTimeout(t.Context(), paCallerDeadline)
		defer cancel()

		result := paAwait(t, paCaptureAsync(ctx, repo, 2*paRoomyBudget), paCallerDeadline+paGuardSlack)

		ctl.requireReached(t)
		if !errors.Is(result.err, context.DeadlineExceeded) {
			t.Fatalf("capture error %v, want one matching context.DeadlineExceeded", result.err)
		}
		want := fmt.Sprintf("Git inspection stopped by the caller (git %s): %s", probe, ctx.Err().Error())
		if result.err.Error() != want {
			t.Fatalf("capture error %q, want %q", result.err.Error(), want)
		}
	})
}

// R10: a probe whose git exits 0 while a background child keeps its stdout
// open aborts capture with a zero value and the could-not-complete message
// naming the whole probe; it is neither read as "not a repository" nor
// returned as a snapshot without Head and Branch. For each of the six probes.
func TestProbeAbortUncleanFinish(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	for _, probe := range paProbes {
		t.Run(probe, func(t *testing.T) {
			t.Parallel()
			repo := paCommittedRepo(t, "probe-branch")
			ctl := paConfigure(t, repo, probe, "linger")

			result := paAwait(t, paCaptureAsync(t.Context(), repo, paRoomyBudget), paRoomyBudget+paGuardSlack)

			ctl.requireReached(t)
			prefix := fmt.Sprintf("Git inspection could not complete (git %s): ", probe)
			if result.err == nil || !strings.HasPrefix(result.err.Error(), prefix) {
				t.Fatalf("capture returned (%+v, %v), want an error starting with %q", result.got, result.err, prefix)
			}
			paExpectZero(t, result.got)
		})
	}
}

// R4: a plain directory outside any repository, with the real git, is
// captured as a folder without Git identity.
func TestProbeAbortOutsideRepository(t *testing.T) {
	paRequireFake(t)
	parent := paTempDir(t)
	dir := filepath.Join(parent, "plain")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The real git alone, and no repository above the temporary directory.
	t.Setenv("PATH", paOriginalPath)
	t.Setenv("GIT_CEILING_DIRECTORIES", parent)

	got, err := capture(t.Context(), dir, paRoomyBudget)
	paExpectFolderOnly(t, got, err)
}

// R5: with no git on PATH, even a real repository is captured as a folder
// without Git identity.
func TestProbeAbortNoGit(t *testing.T) {
	paRequireFake(t)
	repo := paCommittedRepo(t, "probe-branch")
	t.Setenv("PATH", paTempDir(t))

	got, err := capture(t.Context(), repo, paRoomyBudget)
	paExpectFolderOnly(t, got, err)
}

// R6: when `rev-parse --show-toplevel` exits with status 1 promptly, even in
// a real repository, capture treats the directory as a folder without Git
// identity.
func TestProbeAbortGitFails(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	repo := paCommittedRepo(t, "probe-branch")
	ctl := paConfigure(t, repo, paProbes[0], "fail")

	got, err := capture(t.Context(), repo, paRoomyBudget)

	ctl.requireReached(t)
	paExpectFolderOnly(t, got, err)
}

// R7: a repository with one commit on a named branch is captured with its
// HEAD and that branch. The control directory makes every probe go through
// the fake, showing the fake alone does not change the outcome.
func TestProbeAbortRepository(t *testing.T) {
	paRequireFake(t)
	t.Parallel()
	repo := paCommittedRepo(t, "probe-branch")
	paConfigure(t, repo, "", "")

	got, err := capture(t.Context(), repo, paRoomyBudget)

	if err != nil {
		t.Fatalf("capture returned %v, want nil", err)
	}
	if got.snapshot.Head == "" {
		t.Fatal("snapshot Head is empty, want the commit")
	}
	if got.snapshot.Branch != "probe-branch" {
		t.Fatalf("snapshot Branch %q, want %q", got.snapshot.Branch, "probe-branch")
	}
}
