package projectctx

import (
	"bytes"
	"cmp"
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/proc"
)

// defaultProbeWaitDelay is how long a probe's output may still arrive after
// the process exits or is stopped, unless the Command sets its own.
const defaultProbeWaitDelay = 100 * time.Millisecond

// probeOutput keeps a probe's bounded output and stops a one-request protocol
// probe once that output holds the awaited response.
type probeOutput struct {
	proc.HeadBuffer
	responseID int
	stop       context.CancelFunc
	complete   bool
}

func (out *probeOutput) Write(data []byte) (int, error) {
	count, err := out.HeadBuffer.Write(data)
	if out.responseID != 0 && !out.complete && hasJSONResponse(out.Bytes(), out.responseID) {
		out.complete = true
		out.stop()
	}
	return count, err
}

// executeBounded runs one probe with proc.Run, in a process group of its own
// that is killed until it is empty when the probe ends, normally or not.
func executeBounded(ctx context.Context, command Command, limit int) (CommandResult, error) {
	if len(command.Stdin) > maxProbeOutput {
		return CommandResult{}, errors.New("probe input exceeded its size limit")
	}
	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	out := &probeOutput{HeadBuffer: proc.HeadBuffer{Limit: limit}, responseID: command.StopAfterJSONID, stop: cancel}
	got := proc.Run(probeCtx, proc.Spec{
		Path: command.Executable, Args: command.Args, Dir: command.Dir, Env: command.Env,
		// Keep a protocol probe's stdin open after the request. EOF means
		// shutdown to asynchronous MCP servers, so only a response, process
		// exit or deadline closes it.
		Stdin: strings.NewReader(command.Stdin), KeepStdinOpen: command.StopAfterJSONID != 0,
		// Optional tools can echo keys or local configuration in failures.
		// Neither stderr nor subprocess errors become doctor report data.
		Stdout: out, Stderr: io.Discard,
		WaitDelay: cmp.Or(command.WaitDelay, defaultProbeWaitDelay),
	})
	return CommandResult{Stdout: out.Bytes(), Truncated: out.Overflow()}, probeError(ctx.Err(), got, out.complete)
}

// probeError is the error a probe returns. The caller's context error comes
// first and stays matchable with errors.Is when it is joined with a group
// member that survived the kill; a survivor fails an otherwise clean probe;
// a completed protocol exchange succeeds whatever the stopped process
// reported; otherwise the error is os/exec's.
func probeError(ctxErr error, got proc.Result, complete bool) error {
	switch {
	case ctxErr != nil && got.Cleanup != nil:
		return errors.Join(ctxErr, got.Cleanup)
	case ctxErr != nil:
		return ctxErr
	case got.Cleanup != nil:
		return got.Cleanup
	case complete:
		return nil
	}
	return got.Err
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
