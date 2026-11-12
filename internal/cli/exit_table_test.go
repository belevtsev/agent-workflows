package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/graphify"
	"github.com/belevtsev/agent-workflows/v3/internal/publish"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: the exit table in exit.go and docs/operations.md#exit-statuses. A
// sentinel keeps its row however deeply it is wrapped; an error that names
// no row is a failure.
func TestExitCodeMapsEachErrorToItsTableRow(t *testing.T) {
	for _, row := range []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"plain error", errors.New("read task record: input/output error"), ExitFailure},
		{"usage", usage(errors.New("unknown flag")), ExitUsage},
		{"reported refusal", reportedExit(ExitRefused), ExitRefused},
		{"wrapped incomplete verification", fmt.Errorf("verify: %w", taskflow.ErrIncomplete), ExitNegative},
		{"wrapped failed check", fmt.Errorf("run: %w", taskflow.ErrCheckFailed), ExitNegative},
		{"wrapped cancellation", fmt.Errorf("run: %w", context.Canceled), ExitCancelled},
	} {
		if got := ExitCode(row.err); got != row.want {
			t.Errorf("%s: ExitCode(%v) = %d, want %d", row.name, row.err, got, row.want)
		}
	}
}

// The commands return the sentinels themselves, so a caller in Go can use
// errors.Is rather than the message; the messages are unchanged.
func TestTaskVerifyAndRunReturnTheNegativeResultSentinels(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	verify := compactExecute(h, nil, "task", "verify", "--task", h.task)
	if !errors.Is(verify.err, taskflow.ErrIncomplete) || verify.err.Error() != "task completion evidence is incomplete; inspect problems" {
		t.Fatalf("verify of an unassessed task returned %v, want taskflow.ErrIncomplete", verify.err)
	}

	h = compactCheckHarness(t, nil, []string{"cat", "exit-status-missing-input"})
	run := compactExecute(h, nil, "task", "run", "--task", h.task, "--check", "probe")
	if !errors.Is(run.err, taskflow.ErrCheckFailed) || run.err.Error() != "check did not pass: failed" {
		t.Fatalf("failed check run returned %v, want taskflow.ErrCheckFailed with the run status", run.err)
	}
}

// consult, graphify and publish report their own result; their outcomes land
// on the table: an incomplete consultation is a negative result, Graphify's
// own refusal is refused, any other Graphify error is a failure, and each
// publish status takes the row of the same name.
func TestReportingCommandOutcomesMapToTableRows(t *testing.T) {
	for status, want := range map[string]int{
		publish.StatusPublished: ExitOK, publish.StatusFailed: ExitFailure, publish.StatusDenied: ExitDenied,
		publish.StatusRefused: ExitRefused, publish.StatusUnknown: ExitUnknown, publish.StatusMismatch: ExitMismatch,
		publish.StatusCancelled: ExitCancelled,
	} {
		if got := (publish.Report{Status: status}).ExitCode(); got != want {
			t.Errorf("publish status %q exits %d, want %d", status, got, want)
		}
	}
	for outcome, want := range map[int]int{consult.Complete: ExitOK, consult.Invalid: ExitFailure, consult.Incomplete: ExitNegative} {
		if got := consultExit(outcome); got != want {
			t.Errorf("consult outcome %d exits %d, want %d", outcome, got, want)
		}
	}
	if got := graphifyExit(&graphify.Error{Code: "runtime_missing", Detail: "install the runtime"}); got != ExitRefused {
		t.Errorf("graphify refusal exits %d, want %d", got, ExitRefused)
	}
	if got := graphifyExit(fmt.Errorf("cache: %w", errors.New("permission denied"))); got != ExitFailure {
		t.Errorf("graphify filesystem error exits %d, want %d", got, ExitFailure)
	}
}
