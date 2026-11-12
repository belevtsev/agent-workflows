package cli

import (
	"context"
	"errors"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/graphify"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Exit statuses: one table for every cw command, documented under "Exit
// statuses" in docs/operations.md. Each outcome takes the row that names it.
const (
	// ExitOK: the command did what was asked.
	ExitOK = 0
	// ExitFailure: cw could not do it, for example invalid input, an I/O
	// error, or a damaged or locked record.
	ExitFailure = 1
	// ExitUsage: the command line is wrong; nothing ran.
	ExitUsage = 2
	// ExitDenied: the gate blocks the operation.
	ExitDenied = 3
	// ExitRefused: a precondition or the record's state refuses the operation.
	ExitRefused = 4
	// ExitNegative: the command worked and the answer is no, such as an
	// incomplete verification or a failed check.
	ExitNegative = 5
	// ExitUnknown: cw publish cannot establish whether a step happened.
	ExitUnknown = 6
	// ExitMismatch: cw publish read back a step that differs from the request.
	ExitMismatch = 7
	// ExitCancelled: the command was interrupted.
	ExitCancelled = 130
)

// sentinelExits gives the exit status of each error that names an outcome,
// matched in order with errors.Is. A new sentinel is one row.
var sentinelExits = []struct {
	err    error
	status int
}{
	{context.Canceled, ExitCancelled},
	{taskflow.ErrIncomplete, ExitNegative},
	{taskflow.ErrCheckFailed, ExitNegative},
}

type usageError struct{ error }

func usage(err error) error { return usageError{err} }

// reportedExit is the exit status of a command that has already written its
// own result, so the entrypoint adds no further error line.
type reportedExit int

func (reportedExit) Error() string { return "" }

// finish returns err, else status as a reportedExit, else nil for ExitOK.
func finish(status int, err error) error {
	if err != nil {
		return err
	}
	if status != ExitOK {
		return reportedExit(status)
	}
	return nil
}

// ExitCode maps an error from Execute to its exit status.
func ExitCode(err error) int {
	if err == nil {
		return ExitOK
	}
	for _, row := range sentinelExits {
		if errors.Is(err, row.err) {
			return row.status
		}
	}
	if status, ok := errors.AsType[reportedExit](err); ok {
		return int(status)
	}
	if _, ok := errors.AsType[usageError](err); ok {
		return ExitUsage
	}
	return ExitFailure
}

// consultExit is the exit status of a consult.Run outcome.
func consultExit(outcome int) int {
	switch outcome {
	case consult.Complete:
		return ExitOK
	case consult.Incomplete:
		return ExitNegative
	default:
		return ExitFailure
	}
}

// graphifyExit is the exit status of a graphify command that reported itself
// unavailable with err: Graphify's own refusals (no runtime, no current graph,
// a limit or an unsafe path) are refused; any other error is a failure.
func graphifyExit(err error) int {
	if _, ok := errors.AsType[*graphify.Error](err); ok {
		return ExitRefused
	}
	return ExitFailure
}
