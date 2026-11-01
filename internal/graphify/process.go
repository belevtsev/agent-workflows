package graphify

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	if err := supported(); err != nil {
		return 0, "", "", err
	}
	var stdout, stderr bytes.Buffer
	command := &exec.Cmd{Path: arguments[0], Args: arguments, Dir: directory, Env: env, Stdout: &stdout, Stderr: &stderr}
	isolate(command)
	if err := command.Start(); err != nil {
		return 0, "", "", err
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	stop := func() {
		killGroup(command)
		select {
		case <-done:
		case <-time.After(killWait):
		}
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	var waitErr error
	select {
	case waitErr = <-done:
	case <-timer.C:
		stop()
		return 0, "", "", refuse("timeout", "Graphify operation timed out; no incomplete graph was activated")
	case <-ctx.Done():
		stop()
		return 0, "", "", ctx.Err()
	}
	if _, exited := errors.AsType[*exec.ExitError](waitErr); waitErr != nil && !exited {
		return 0, "", "", waitErr
	}
	if stdout.Len()+stderr.Len() > maxGraphBytes {
		return 0, "", "", refuse("output_limit", "Tool output exceeded the local limit")
	}
	// Invalid bytes become replacement characters, one per byte.
	return command.ProcessState.ExitCode(), string([]rune(stdout.String())), string([]rune(stderr.String())), nil
}
