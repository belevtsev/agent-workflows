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

func checkEnvironment() []string {
	environment := []string{}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if strings.Contains(upper, "KEY") || strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "CREDENTIAL") || upper == "BASH_ENV" || upper == "ENV" {
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
// without a shell, with the filtered environment of checkEnvironment, no
// input, the check's timeout and checkWaitDelay. It is the one place that
// decides which program a check runs and what that program sees; RunCheck
// records the environment's GOFLAGS from it and attaches the log as both
// outputs.
func checkSpec(check Check, dir string) proc.Spec {
	return proc.Spec{
		Path: check.Argv[0], Args: check.Argv[1:], Dir: dir, Env: checkEnvironment(),
		Timeout:   time.Duration(check.TimeoutSeconds) * time.Second,
		WaitDelay: checkWaitDelay,
	}
}

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
func RunCheck(ctx context.Context, path, checkID string) (CheckRun, error) {
	var run CheckRun
	var spec proc.Spec
	var logFile *os.File
	// The start revision also reserves its terminal revision before the callback
	// can create a log or the planned child can execute.
	_, err := updateWithCapacity(ctx, path, 2, "", func(root *os.Root, record *Record) error {
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
		snapshot, err := projectctx.Capture(ctx, record.Baseline.SelectedPath)
		if err != nil {
			return err
		}
		id, err := randomID()
		if err != nil {
			return err
		}
		spec = checkSpec(check, snapshot.SelectedPath)
		run = CheckRun{ID: id, CheckID: checkID, Status: "running", Argv: append([]string(nil), check.Argv...), StartedAt: time.Now().UTC(), Before: snapshot, Log: "check-" + id + ".log", GOFLAGS: langpack.RedactGOFLAGS(environmentValue(spec.Env, "GOFLAGS"))}
		logFile, err = root.OpenFile(run.Log, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
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
	capture := &logCapture{file: logFile, full: sha256.New(), saved: sha256.New()}
	// One writer takes both outputs, so os/exec gives the check a single pipe
	// and the log keeps stdout and stderr in the order the check wrote them.
	spec.Stdout, spec.Stderr = capture, capture
	// proc.Run kills the check's process group until it is empty, after a
	// normal exit as well as at the timeout or cancellation.
	got := proc.Run(ctx, spec)
	syncErr := logFile.Sync()
	closeErr := logFile.Close()
	finished := time.Now().UTC()
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
	after, snapshotErr := projectctx.Capture(captureCtx, run.Before.SelectedPath)
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
	_, storeErr := updateWhenUnlocked(finishCtx, path, run.ID, func(_ *os.Root, record *Record) error {
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

var _ io.Writer = (*logCapture)(nil)
