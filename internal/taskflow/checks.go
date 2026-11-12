package taskflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/proc"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

const (
	// checkFinishTimeout bounds recording a finished check: the source
	// capture after the run and the terminal record write.
	checkFinishTimeout = 30 * time.Second
	// checkRecordWriteReserve is the part of checkFinishTimeout the source
	// capture never uses, so the terminal record write can still wait for
	// the task lock. Without it a stalled capture could use the whole
	// budget and leave the run recorded as running.
	checkRecordWriteReserve = 10 * time.Second
	// checkWaitDelay bounds how long a check's output may still arrive after
	// the check exits or is stopped, for a descendant that keeps the output
	// pipe open.
	checkWaitDelay = 2 * time.Second
)

// validateCommandShape checks the bounded representation shared by immutable
// history and new commands without making an execution-policy decision.
func validateCommandShape(argv []string) error {
	if len(argv) == 0 || len(argv) > 256 || !nonblank(argv[0]) {
		return errors.New("check needs an explicit argv with 1..256 arguments")
	}
	total := 0
	for _, arg := range argv {
		total += len(arg)
		if strings.IndexByte(arg, 0) >= 0 {
			return errors.New("check argv contains NUL")
		}
	}
	if total > 65536 {
		return errors.New("check argv exceeds 64 KiB")
	}
	return nil
}

func validateCommand(argv []string) error {
	if err := validateCommandShape(argv); err != nil {
		return err
	}
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(argv[0])), ".exe")
	switch name {
	case "sh", "bash", "zsh", "dash", "fish", "ksh", "csh", "tcsh", "cmd", "powershell", "pwsh", "eval", "env", "sudo", "doas":
		return errors.New("checks cannot invoke a shell, environment wrapper or privilege elevation")
	case "curl", "wget", "ssh", "scp", "sftp", "rsync", "az", "aws", "gcloud", "kubectl", "helm", "gh":
		return errors.New("network, publication and deployment tools are outside task checks")
	}
	if name == "git" || name == "terraform" || name == "tofu" {
		subcommand, err := directSubcommand(name, argv[1:])
		if err != nil {
			return err
		}
		if name == "git" {
			if subcommand != "" && !slices.Contains([]string{"diff", "status", "log", "show", "rev-parse", "rev-list", "ls-files", "ls-tree", "merge-base", "describe"}, subcommand) {
				return errors.New("Git checks must select a supported inspection builtin")
			}
		} else if subcommand == "apply" || subcommand == "destroy" {
			return errors.New("publication and deployment commands are outside task checks")
		}
		return nil
	}
	// cw publish commits, pushes and opens pull requests. Refusing it here is
	// a guard, not a sandbox: a check can still run another program that
	// publishes.
	if name == "cw" && cwSubcommand(argv[1:]) == "publish" {
		return errors.New("publication and deployment commands are outside task checks")
	}
	for _, subcommand := range argv[1:] {
		publication := (name == "docker" || name == "podman") && subcommand == "push"
		publication = publication || slices.Contains([]string{"npm", "pnpm", "yarn", "cargo"}, name) && subcommand == "publish"
		deployment := name == "pulumi" && (subcommand == "up" || subcommand == "destroy")
		if publication || deployment {
			return errors.New("publication and deployment commands are outside task checks")
		}
	}
	return nil
}

// cwSubcommand returns the first argument after cw's global flags: the
// directory flags take a value (separate or after =), --dry-run takes none
// or an attached one, and any other argument starting with - stands alone.
func cwSubcommand(args []string) string {
	valued := []string{"--config-dir", "--home", "--codex-home", "--claude-home", "--state-dir"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case slices.Contains(valued, arg):
			i++
		case strings.HasPrefix(arg, "-"):
		default:
			return arg
		}
	}
	return ""
}

// directSubcommand consumes global options before classifying a direct Git or
// Terraform operation. Values of -C/-chdir are data, and arguments after the
// command (including a source path named "push" or "apply") are not commands.
// Unknown global options fail closed instead of guessing their value arity.
func directSubcommand(tool string, args []string) (string, error) {
	if tool == "git" {
		return directGitSubcommand(args)
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) {
				return args[i+1], nil
			}
			return "", nil
		}
		if !strings.HasPrefix(arg, "-") {
			return arg, nil
		}
		if arg == "-chdir" {
			if i+1 >= len(args) {
				return "", errors.New("global check option is missing its value")
			}
			i++
			continue
		}
		if arg == "-help" || arg == "--help" || arg == "-version" || arg == "--version" || strings.HasPrefix(arg, "-chdir=") {
			continue
		}
		return "", fmt.Errorf("unsupported global %s check option %s", tool, arg)
	}
	return "", nil
}

// directGitSubcommand filters dispatch and explicit configuration overrides.
// It does not sandbox a builtin's arguments or inherited Git configuration.
func directGitSubcommand(args []string) (string, error) {
	values := []string{"-C", "--git-dir", "--work-tree", "--namespace", "--super-prefix"}
	flags := []string{"-P", "--no-pager", "--bare", "--no-replace-objects", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-optional-locks", "--no-lazy-fetch", "--no-advice"}
	outputs := []string{"-v", "--version", "--html-path", "--man-path", "--info-path", "--exec-path"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			if i+1 < len(args) && args[i+1] != "" {
				return args[i+1], nil
			}
			return "", errors.New("Git check is missing an inspection builtin")
		}
		if !strings.HasPrefix(arg, "-") {
			if arg == "" {
				return "", errors.New("Git check is missing an inspection builtin")
			}
			return arg, nil
		}
		if arg == "-c" || arg == "--config-env" {
			if i+1 >= len(args) {
				return "", errors.New("global Git check option is missing its value")
			}
			i++
			if err := validateGitConfigOverride(args[i], arg == "--config-env"); err != nil {
				return "", err
			}
			continue
		}
		if slices.Contains(values, arg) {
			if i+1 >= len(args) {
				return "", errors.New("global Git check option is missing its value")
			}
			i++
			continue
		}
		if slices.Contains(outputs, arg) {
			if i+1 != len(args) {
				return "", errors.New("Git metadata output must stand alone without a command")
			}
			return "", nil
		}
		if slices.Contains(flags, arg) || strings.HasPrefix(arg, "-C") && len(arg) > 2 {
			continue
		}
		if strings.HasPrefix(arg, "-c") && len(arg) > 2 {
			if err := validateGitConfigOverride(arg[2:], false); err != nil {
				return "", err
			}
			continue
		}
		option, value, hasValue := strings.Cut(arg, "=")
		if hasValue {
			if option == "--config-env" {
				if err := validateGitConfigOverride(value, true); err != nil {
					return "", err
				}
				continue
			}
			if slices.Contains(values[1:], option) {
				continue
			}
		}
		return "", fmt.Errorf("unsupported global Git check option %s", arg)
	}
	return "", errors.New("Git check is missing an inspection builtin or metadata output")
}

func validateGitConfigOverride(value string, fromEnvironment bool) error {
	key, environment, assigned := strings.Cut(value, "=")
	if fromEnvironment && (!assigned || environment == "" || strings.Contains(environment, "=")) {
		return errors.New("Git check config-env needs a key and environment variable name")
	}
	if !slices.Contains([]string{"core.quotepath", "color.ui", "diff.noprefix", "diff.mnemonicprefix", "diff.context", "status.short", "status.branch"}, strings.ToLower(key)) {
		return fmt.Errorf("unsupported Git check configuration key %s", key)
	}
	return nil
}

// envPassthroughName is the shape of a name in a check's env_passthrough.
var envPassthroughName = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

const (
	// maxEnvPassthrough bounds the names in one check's env_passthrough.
	maxEnvPassthrough = 32
	// maxEnvPassthroughName bounds the length of one of those names.
	maxEnvPassthroughName = 128
)

// validateEnvPassthroughShape checks the bounded representation of a check's
// env_passthrough shared by stored history and new checks: at most
// maxEnvPassthrough distinct names, each matching envPassthroughName and at
// most maxEnvPassthroughName bytes long.
func validateEnvPassthroughShape(names []string) error {
	if len(names) > maxEnvPassthrough {
		return fmt.Errorf("env_passthrough names at most %d variables", maxEnvPassthrough)
	}
	for i, name := range names {
		if len(name) > maxEnvPassthroughName {
			return fmt.Errorf("env_passthrough names a variable of %d bytes, over %d", len(name), maxEnvPassthroughName)
		}
		if !envPassthroughName.MatchString(name) {
			return fmt.Errorf("env_passthrough name %q does not match %s", name, envPassthroughName)
		}
		if slices.Contains(names[:i], name) {
			return fmt.Errorf("env_passthrough names %s twice", name)
		}
	}
	return nil
}

// validateEnvPassthrough admits a check's env_passthrough for execution: its
// shape, and none of the names that the environment filter or cw's own
// settings exist to keep from a check. Like validateCommand it guards against
// accidents and is no sandbox: the same value exported under another name
// passes, though the plan and verify then show that name.
func validateEnvPassthrough(names []string) error {
	if err := validateEnvPassthroughShape(names); err != nil {
		return err
	}
	for _, name := range names {
		switch name {
		case "BASH_ENV", "ENV":
			return fmt.Errorf("env_passthrough cannot name %s: a shell that the check starts would run the file it names", name)
		case "GOROOT", "GOTOOLCHAIN":
			return fmt.Errorf("env_passthrough cannot name %s: cw sets it for a go check to the go command it verified", name)
		case "GH_TOKEN", "GITHUB_TOKEN", "GH_ENTERPRISE_TOKEN", "GITHUB_ENTERPRISE_TOKEN":
			return fmt.Errorf("env_passthrough cannot name %s: gh publishes with it, and publication is outside task checks", name)
		}
	}
	return nil
}

// checkEnvironment returns cw's environment for check without the variables
// whose names, in any case, contain KEY, TOKEN, SECRET, PASSWORD or
// CREDENTIAL, or are BASH_ENV or ENV, except those that check's
// env_passthrough names exactly. The Go rule's own go commands
// (executionProblems) get it for a check with no env_passthrough.
func checkEnvironment(check Check) []string {
	environment := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		filtered := strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") || upper == "BASH_ENV" || upper == "ENV"
		if filtered && !slices.Contains(check.EnvPassthrough, key) {
			continue
		}
		environment = append(environment, entry)
	}
	return environment
}

// environmentValue returns the value of key in an os.Environ-style list, or
// "" when it is unset.
func environmentValue(environment []string, key string) string {
	for _, entry := range environment {
		if name, value, ok := strings.Cut(entry, "="); ok && name == key {
			return value
		}
	}
	return ""
}

type logCapture struct {
	mu      sync.Mutex
	file    *os.File
	full    hash.Hash
	saved   hash.Hash
	bytes   int64
	written int
	failure error
	// tests reads the per-test results of the complete output.
	tests langpack.GoTestOutput
}

func (capture *logCapture) Write(data []byte) (int, error) {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	capture.bytes += int64(len(data))
	capture.full.Write(data)
	_, _ = capture.tests.Write(data) // GoTestOutput.Write only parses and never fails.
	keep := min(len(data), MaxLogBytes-capture.written)
	if keep > 0 && capture.failure == nil {
		n, err := capture.file.Write(data[:keep])
		capture.saved.Write(data[:n])
		capture.written += n
		if err != nil {
			capture.failure = err
		}
	}
	// Drain all process output even after reaching the storage bound.
	return len(data), nil
}

// testResults reports the per-test results of the output written so far,
// with each test's run and marked counts (R6).
func (capture *logCapture) testResults() langpack.GoTestResults {
	capture.mu.Lock()
	defer capture.mu.Unlock()
	return capture.tests.ResultsWithRuns()
}

// checkSpec builds the proc.Spec that runs check in dir: its argv directly,
// without a shell, with the environment checkEnvironment filters for it, no
// input, the check's timeout and checkWaitDelay. A check whose program is
// named go (or go.exe, as the Go evidence rule names it) runs instead the go
// command that langpack.ResolveGo verified, by its absolute path and with
// GoTool.Env added, and tool is that go command; goEnv is what it reported
// with go env under that environment and in dir (langpack.ReadGoEnv), which
// the Go evidence rule credits by, and the run's environment ends with the
// GOFLAGS, GOOS and GOARCH it reported, so a go env file read again by the
// run cannot change them. err says why no go command could be verified, or
// its go env read, and then the check must not run. Resolving and reading
// run the go command, within ctx and the check's timeout, so RunCheck calls
// checkSpec before it takes the task lock. checkSpec is the one place that
// decides which program a check runs and what that program sees; RunCheck
// attaches the log as both outputs.
func checkSpec(ctx context.Context, check Check, dir string) (spec proc.Spec, tool *langpack.GoTool, goEnv *langpack.GoEnv, err error) {
	spec = proc.Spec{
		Path: check.Argv[0], Args: check.Argv[1:], Dir: dir, Env: checkEnvironment(check),
		Timeout:   time.Duration(check.TimeoutSeconds) * time.Second,
		WaitDelay: checkWaitDelay,
	}
	if strings.TrimSuffix(filepath.Base(spec.Path), ".exe") != "go" {
		return spec, nil, nil, nil
	}
	if spec.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, spec.Timeout)
		defer cancel()
	}
	resolved, err := resolveGo(ctx, spec.Path, dir, spec.Env)
	if err != nil {
		return spec, nil, nil, err
	}
	spec.Path = resolved.Path
	spec.Env = append(spec.Env, resolved.Env()...)
	read, pin, err := readGoEnv(ctx, resolved, dir, spec.Env)
	if err != nil {
		return spec, nil, nil, err
	}
	// go test reads a go env file again; the values just read win over it.
	spec.Env = append(spec.Env, pin...)
	return spec, &resolved, &read, nil
}

// resolveGo and readGoEnv are langpack.ResolveGo and langpack.ReadGoEnv; a
// test replaces them with functions it controls.
var (
	resolveGo = langpack.ResolveGo
	readGoEnv = langpack.ReadGoEnv
)

// checkExitCode returns the exit code a run records, as os.ProcessState's
// ExitCode reported it: the program's own status whenever it exited, also
// when the run was stopped afterwards while a descendant held the output
// open; -1 when a signal ended it, such as the kill at a timeout or a
// cancellation; and nil when it never started (it could not be started, or
// the run was stopped first). proc.Result.Code is -1 both when a signal ended
// the program and when it never started; only the first has an
// *exec.ExitError.
func checkExitCode(got proc.Result) *int {
	var exit *exec.ExitError
	if got.Code < 0 && !errors.As(got.Err, &exit) {
		return nil
	}
	code := got.Code
	return &code
}

// RunCheck executes only argv recorded in the immutable plan or added by an
// amendment (AddChecks), in the repository directory, without a shell. Starting and terminal states are separate durable
// revisions, so process interruption cannot turn an unfinished check into a pass.
func (env Env) RunCheck(ctx context.Context, path, checkID string) (CheckRun, error) {
	var run CheckRun
	var logFile *os.File
	// The program is resolved before the task lock is taken: resolving a go
	// command runs it, which can wait on a slow shim or a toolchain download.
	stored, err := Load(path)
	if err != nil {
		return run, err
	}
	planned, found := findCheck(stored, checkID)
	if !found {
		return run, errors.New("check is not in the explicit coordinator plan")
	}
	// Admitted again, as validateCommand is below, but before checkSpec:
	// resolving go already runs a program with the check's environment.
	if err := validateEnvPassthrough(planned.EnvPassthrough); err != nil {
		return run, fmt.Errorf("check %s: %w", checkID, err)
	}
	// refused says why no verified go command could run the check; the start
	// revision then records the run as failed, and nothing runs.
	spec, tool, goEnv, refused := checkSpec(ctx, planned, stored.Baseline.SelectedPath)
	if refused != nil && ctx.Err() != nil {
		return run, ctx.Err()
	}
	// The start revision also reserves its terminal revision before the callback
	// can create a log or the planned child can execute.
	_, err = updateWithCapacity(ctx, env, path, 2, "", func(root *os.Root, record *Record) error {
		check, found := findCheck(*record, checkID)
		if !found {
			return errors.New("check is not in the explicit coordinator plan")
		}
		if err := validateCommand(check.Argv); err != nil {
			return err
		}
		for _, existing := range record.Checks {
			if existing.CheckID == checkID && existing.Status == "running" {
				return errors.New("the same check already has an owned running attempt; wait for its terminal record")
			}
		}
		snapshot, err := env.source().Snapshot(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		// Both are immutable once recorded; this guards the resolution above.
		if !slices.Equal(check.Argv, planned.Argv) || !slices.Equal(check.EnvPassthrough, planned.EnvPassthrough) || snapshot.SelectedPath != spec.Dir {
			return errors.New("the check or its selected path changed while cw resolved its program; run it again")
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		run = CheckRun{ID: id, CheckID: checkID, Status: "running", Argv: append([]string(nil), check.Argv...), StartedAt: env.now(), Before: snapshot, Log: "check-" + id + ".log", GOFLAGS: langpack.RedactGOFLAGS(environmentValue(checkEnvironment(check), "GOFLAGS")), Tool: tool, GoEnv: goEnv}
		logFile, err = root.OpenFile(run.Log, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		if refused != nil {
			// The start revision is also the terminal one: the run failed
			// without executing anything, and its log stays empty.
			finished := env.now()
			run.Status, run.Failure, run.FinishedAt = "failed", "untrusted_toolchain", &finished
			run.LogSHA256, run.OutputSHA256 = digest(nil), digest(nil)
		}
		record.Checks = append(record.Checks, run)
		record.EvidenceRevision++
		record.Verification = nil
		return nil
	})
	if err != nil {
		if logFile != nil {
			logFile.Close()
		}
		return run, err
	}
	if refused != nil {
		if err := logFile.Close(); err != nil {
			return run, err
		}
		return run, fmt.Errorf("%w: %s (%s): %v", ErrCheckFailed, run.Status, run.Failure, refused)
	}
	capture := &logCapture{file: logFile, full: sha256.New(), saved: sha256.New()}
	// One writer takes both outputs, so os/exec gives the check a single pipe
	// and the log keeps stdout and stderr in the order the check wrote them.
	spec.Stdout, spec.Stderr = capture, capture
	// proc.Run kills the check's process group until it is empty, after a
	// normal exit as well as at the timeout or cancellation.
	got := proc.Run(ctx, spec)
	syncErr := logFile.Sync()
	closeErr := logFile.Close()
	finished := env.now()
	run.FinishedAt = &finished
	run.LogSHA256 = hex.EncodeToString(capture.saved.Sum(nil))
	run.OutputSHA256 = hex.EncodeToString(capture.full.Sum(nil))
	run.OutputBytes = capture.bytes
	run.Truncated = capture.bytes > int64(capture.written)
	run.ExitCode = checkExitCode(got)
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		run.Status = "cancelled"
		run.Failure = "cancelled"
	case got.Stopped:
		// The check's timeout, or a deadline on ctx, stopped the run.
		run.Status = "timed_out"
		run.Failure = "timeout"
	case got.Cleanup != nil:
		run.Status = "failed"
	case got.Err != nil:
		run.Status = "failed"
		run.Failure = "command_failed"
	case capture.failure != nil || syncErr != nil || closeErr != nil:
		run.Status = "failed"
		run.Failure = "log_write_failed"
	default:
		run.Status = "passed"
	}
	if got.Cleanup != nil {
		// A check process outlived the group kill; keep a cancelled or timed-out
		// status, but never let the leak go unrecorded.
		run.Failure = "process_cleanup_failed"
	}
	// A cancelled request still needs a terminal source/run record. This bounded
	// cleanup context never executes another command from the task plan.
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), checkFinishTimeout)
	defer finishCancel()
	// The source capture gets only part of the finish budget, so a slow Git
	// probe cannot leave the terminal record write without time to wait for
	// the task lock; a capture that runs out marks the run source_changed.
	captureCtx, captureCancel := context.WithTimeout(finishCtx, checkFinishTimeout-checkRecordWriteReserve)
	after, snapshotErr := env.source().Snapshot(captureCtx, run.Before.SelectedPath)
	captureCancel()
	if snapshotErr == nil {
		run.After = &after
	}
	if snapshotErr != nil || !equalSnapshot(run.Before, after) {
		if run.Status == "passed" {
			run.Status = "source_changed"
			run.Failure = "source_snapshot_changed_or_unavailable"
		}
	}
	// The results file is written before the terminal revision names it. A
	// write failure leaves the run without results and fails only a run that
	// would otherwise pass.
	if results := capture.testResults(); len(results.Tests) > 0 {
		if name, sum, err := writeTestResults(path, run.ID, results); err == nil {
			run.TestResults, run.TestResultsSHA256 = name, sum
		} else if run.Status == "passed" {
			run.Status = "failed"
			run.Failure = "test_results_write_failed"
		}
	}
	_, storeErr := updateWhenUnlocked(finishCtx, env, path, run.ID, func(_ *os.Root, record *Record) error {
		for i := range record.Checks {
			if record.Checks[i].ID == run.ID {
				if record.Checks[i].Status != "running" {
					return errors.New("check already has a terminal result")
				}
				record.Checks[i] = run
				record.EvidenceRevision++
				record.Verification = nil
				return nil
			}
		}
		return errors.New("starting check record is missing")
	})
	if storeErr != nil {
		return run, storeErr
	}
	if run.Status != "passed" {
		if errors.Is(ctx.Err(), context.Canceled) {
			return run, context.Canceled
		}
		if run.Failure == "log_write_failed" || run.Failure == "test_results_write_failed" {
			// The check passed; cw failed to keep its output, which is no answer.
			return run, errors.New("the check passed, but cw could not write its output: " + run.Failure)
		}
		return run, fmt.Errorf("%w: %s", ErrCheckFailed, run.Status)
	}
	return run, nil
}

// toolLimitations names, for the checks whose latest run passed at snapshot
// and ran go, the go command that ran them: one line per go command, in check
// order, and one for runs recorded before cw verified go, which name none.
func toolLimitations(record Record, snapshot projectctx.Snapshot) []string {
	var order []string
	checks := map[string][]string{}
	for _, check := range allChecks(record) {
		run := latestRun(record, check.ID)
		if run == nil || !passedAt(*run, snapshot) || strings.TrimSuffix(filepath.Base(run.Argv[0]), ".exe") != "go" {
			continue
		}
		key := ""
		if run.Tool != nil {
			key = run.Tool.Version + " at " + run.Tool.Path + " with GOROOT " + run.Tool.GOROOT
		}
		if _, seen := checks[key]; !seen {
			order = append(order, key)
		}
		checks[key] = append(checks[key], check.ID)
	}
	limitations := make([]string, 0, len(order))
	for _, key := range order {
		ids := "check " + checks[key][0]
		if len(checks[key]) > 1 {
			ids = "checks " + strings.Join(checks[key], ", ")
		}
		if key == "" {
			limitations = append(limitations, ids+" ran a go command that cw did not verify (recorded before cw verified go)")
			continue
		}
		limitations = append(limitations, ids+" ran "+key+"; cw verified that it is a build of cmd/go, not that it or its GOROOT is unmodified")
	}
	return limitations
}

// passthroughLimitations names, for the checks whose latest run passed at
// snapshot, in check order, the variables their env_passthrough let through
// from cw's environment. The run does not record which of them were set.
func passthroughLimitations(record Record, snapshot projectctx.Snapshot) []string {
	var limitations []string
	for _, check := range allChecks(record) {
		if run := latestRun(record, check.ID); len(check.EnvPassthrough) > 0 && run != nil && passedAt(*run, snapshot) {
			limitations = append(limitations, "check "+check.ID+" received "+strings.Join(check.EnvPassthrough, ", ")+" through env_passthrough, where cw's environment set them")
		}
	}
	return limitations
}

var _ io.Writer = (*logCapture)(nil)
