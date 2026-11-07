package graphify

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// defaultPath is the only PATH a child inherits beyond the runtime's own
// directory, so an inherited PATH cannot substitute git or Graphify helpers.
const defaultPath = "/bin:/usr/bin"

// killWait bounds the wait for pipes after a process group has been killed.
const killWait = 5 * time.Second

// environment is deliberately minimal: no inherited API keys, interpreter
// injection, Graphify flags, logging or backends. Graphify reads
// ~/.graphify/providers.json even in code-only mode, so a child gets a throwaway
// home; the runner's own runtime and cache resolution stays on the real one.
// The query-log and backend switches are fixed, never taken from the caller's
// environment: graphs and queries must not depend on ambient Graphify settings,
// and GRAPHIFY_BACKEND=none keeps every LLM backend off even if a future
// release stops honouring --code-only for some input.
func environment(runtime, output, childHome string) []string {
	values := map[string]string{}
	for _, name := range []string{"HOME", "USERPROFILE", "SYSTEMROOT", "WINDIR", "TMPDIR"} {
		if value, present := os.LookupEnv(name); present {
			values[name] = value
		}
	}
	values["PATH"] = defaultPath
	if runtime != "" {
		values["PATH"] = filepath.Dir(runtime) + string(os.PathListSeparator) + defaultPath
	}
	values["LANG"] = "C.UTF-8"
	values["PYTHONHASHSEED"] = "0"
	values["PYTHONDONTWRITEBYTECODE"] = "1"
	values["GRAPHIFY_NO_AUTO_REFRESH"] = "1"
	values["GRAPHIFY_QUERY_LOG_DISABLE"] = "1"
	values["GRAPHIFY_BACKEND"] = "none"
	if output != "" {
		values["GRAPHIFY_OUT"] = output
	}
	if childHome != "" {
		values["HOME"] = childHome
		values["USERPROFILE"] = childHome
		values["XDG_CONFIG_HOME"] = filepath.Join(childHome, ".config")
		values["XDG_CACHE_HOME"] = filepath.Join(childHome, ".cache")
	}
	result := make([]string, 0, len(values))
	for name, value := range values {
		result = append(result, name+"="+value)
	}
	return result
}

func lookPath(name, searchPath string) (string, error) {
	for _, directory := range filepath.SplitList(searchPath) {
		if directory == "" {
			continue
		}
		candidate := filepath.Join(directory, name)
		if executable(candidate) {
			return candidate, nil
		}
	}
	return "", &os.PathError{Op: "lookup", Path: name, Err: os.ErrNotExist}
}

// run executes one child in its own session without a shell. A timeout or
// cancellation kills every owned descendant, not just the direct child.
func run(ctx context.Context, arguments []string, directory string, env []string, timeout time.Duration) (int, string, string, error) {
	return runWithOutputLimit(ctx, arguments, directory, env, timeout, maxGraphBytes)
}

// capturedOutput shares one raw byte budget between both pipe copiers. Writes
// always consume their input, including after overflow, so only the owner of the
// process decides when to kill and join it.
type capturedOutput struct {
	mu             sync.Mutex
	remaining      int
	stdout, stderr bytes.Buffer
	overflow       chan struct{}
	exceeded       bool
}

type captureStream struct {
	output *capturedOutput
	stderr bool
}

func (stream captureStream) Write(data []byte) (int, error) {
	output := stream.output
	output.mu.Lock()
	defer output.mu.Unlock()
	count := min(len(data), output.remaining)
	buffer := &output.stdout
	if stream.stderr {
		buffer = &output.stderr
	}
	_, _ = buffer.Write(data[:count])
	output.remaining -= count
	if count < len(data) && !output.exceeded {
		output.exceeded = true
		close(output.overflow)
	}
	return len(data), nil
}

func runWithOutputLimit(ctx context.Context, arguments []string, directory string, env []string, timeout time.Duration, limit int) (int, string, string, error) {
	if err := supported(); err != nil {
		return 0, "", "", err
	}
	if err := ctx.Err(); err != nil {
		return 0, "", "", err
	}
	if len(arguments) == 0 {
		return 0, "", "", errors.New("missing tool command")
	}
	output := &capturedOutput{remaining: max(0, limit), overflow: make(chan struct{})}
	command := &exec.Cmd{Path: arguments[0], Args: arguments, Dir: directory, Env: env,
		Stdout: captureStream{output: output}, Stderr: captureStream{output: output, stderr: true}, WaitDelay: killWait}
	isolate(command)
	if err := command.Start(); err != nil {
		return 0, "", "", err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	var interrupted error
	joined := false
	select {
	case waitErr = <-done:
		joined = true
	case <-output.overflow:
		interrupted = refuse("output_limit", "Tool output exceeded the local limit")
	case <-timer.C:
		interrupted = refuse("timeout", "Graphify operation timed out; no incomplete graph was activated")
	case <-ctx.Done():
		interrupted = ctx.Err()
	}
	// Wait can win the select after a short-lived child has already overflowed.
	// Kill the group even then: descendants may outlive the direct child, and
	// exec's context watcher would no longer own their lifetime.
	if interrupted == nil {
		select {
		case <-output.overflow:
			interrupted = refuse("output_limit", "Tool output exceeded the local limit")
		default:
		}
	}
	if interrupted != nil {
		killGroup(command)
		if !joined {
			// This is the only Wait result. WaitDelay closes retained pipes and
			// joins the copiers; never abandon a goroutine writing our buffers.
			<-done
		}
		// One group kill can miss a member that a dying process was forking at
		// that moment (seen on macOS). Kill the group again once the leader is
		// joined, as taskflow does for a cancelled check.
		killGroup(command)
		return 0, "", "", interrupted
	}
	// Completion transfers no descendant lifetime to the caller. Wait may
	// retain ExitError over a copier timeout, and redirected descendants can
	// outlive even a successful, fully joined leader.
	killGroup(command)
	if _, exited := errors.AsType[*exec.ExitError](waitErr); waitErr != nil && !exited {
		return 0, "", "", waitErr
	}
	// Invalid bytes become replacement characters, one per byte.
	return command.ProcessState.ExitCode(), string([]rune(output.stdout.String())), string([]rune(output.stderr.String())), nil
}
