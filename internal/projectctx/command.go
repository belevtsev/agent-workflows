package projectctx

import (
	"bytes"
	"cmp"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"os/exec"
	"strings"
	"sync/atomic"
	"time"
)

// defaultProbeWaitDelay is how long a probe's output may still arrive after
// the process exits or is stopped, unless the Command sets its own.
const defaultProbeWaitDelay = 100 * time.Millisecond

type limitedOutput struct {
	bytes   []byte
	limit   int
	cut     bool
	onWrite func([]byte)
}

func (out *limitedOutput) Write(bytes []byte) (int, error) {
	count := len(bytes)
	remaining := out.limit - len(out.bytes)
	if count > remaining {
		out.cut = true
		bytes = bytes[:remaining]
	}
	out.bytes = append(out.bytes, bytes...)
	if out.onWrite != nil {
		out.onWrite(out.bytes)
	}
	return count, nil
}

func executeBounded(ctx context.Context, command Command, limit int) (CommandResult, error) {
	if len(command.Stdin) > maxProbeOutput {
		return CommandResult{}, errors.New("probe input exceeded its size limit")
	}
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(probeCtx, command.Executable, command.Args...)
	configureProbeProcess(cmd)
	cmd.Dir = command.Dir
	cmd.Env = command.Env
	cmd.WaitDelay = cmp.Or(command.WaitDelay, defaultProbeWaitDelay)
	var input io.WriteCloser
	if command.StopAfterJSONID != 0 {
		var err error
		input, err = cmd.StdinPipe()
		if err != nil {
			return CommandResult{}, err
		}
		defer input.Close()
	} else {
		cmd.Stdin = strings.NewReader(command.Stdin)
	}
	out := &limitedOutput{limit: limit}
	var protocolComplete atomic.Bool
	if command.StopAfterJSONID != 0 {
		out.onWrite = func(output []byte) {
			if hasJSONResponse(output, command.StopAfterJSONID) {
				protocolComplete.Store(true)
				cancel()
			}
		}
	}
	cmd.Stdout = out
	// Optional tools can echo keys or local configuration in failures. Neither
	// stderr nor subprocess errors become doctor report data.
	cmd.Stderr = io.Discard
	var err error
	if input == nil {
		err = cmd.Run()
	} else if err = cmd.Start(); err == nil {
		written := make(chan error, 1)
		go func() {
			_, writeErr := io.WriteString(input, command.Stdin)
			written <- writeErr
		}()
		// Keep stdin open after the request. EOF means shutdown to asynchronous
		// MCP servers, so only a response, process exit or deadline closes it.
		err = cmd.Wait()
		_ = input.Close()
		if writeErr := <-written; err == nil {
			err = writeErr
		}
	}
	// A successful parent can exit after detaching its stdio while children
	// remain alive. Always clean the owned group, including normal exits.
	cleanupProbeProcess(cmd)
	if ctx.Err() != nil {
		err = ctx.Err()
	} else if protocolComplete.Load() {
		err = nil
	}
	return CommandResult{Stdout: out.bytes, Truncated: out.cut}, err
}

func hasJSONResponse(output []byte, id int) bool {
	for line := range bytes.SplitSeq(output, []byte{'\n'}) {
		var response struct {
			ID     int            `json:"id"`
			Result map[string]any `json:"result"`
		}
		if json.Unmarshal(line, &response) == nil && response.ID == id && response.Result != nil {
			return true
		}
	}
	return false
}

func minimalEnvironment(getenv func(string) string) []string {
	result := []string{"LC_ALL=C", "LANG=C", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOTELEMETRY=off"}
	for _, key := range []string{"PATH", "HOME", "USERPROFILE", "SYSTEMROOT", "WINDIR", "TMPDIR", "TMP", "TEMP"} {
		if value := getenv(key); value != "" {
			result = append(result, key+"="+value)
		}
	}
	return result
}
