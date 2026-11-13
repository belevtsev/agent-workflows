package cli

import (
	json "encoding/json/v2"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// receiptPlan has one requirement that a source reference satisfies without
// a check, so an assessment and a decision complete the task.
const receiptPlan = `{"title":"Inspect real source","kind":"small","requirements":[{"id":"source","description":"Existing source inspected","check_ids":[],"allow_non_execution":true}],"checks":[]}`

// expectReceipt requires outcome's stdout to stay one JSON line and its
// stderr to end with the task's receipt, as cw task receipt prints it.
func expectReceipt(h *summaryCLIHarness, name string, outcome compactOutcome, state string) {
	h.t.Helper()
	receipt := compactExecute(h, nil, "task", "receipt", "--task", h.task)
	if receipt.err != nil || !strings.HasPrefix(receipt.stdout, "receipt of task "+h.load().ID+": "+state+"\n") {
		h.t.Fatalf("cw task receipt after %s: %v\n%s", name, receipt.err, receipt.stdout)
	}
	if !strings.HasSuffix(outcome.stderr, receipt.stdout) {
		h.t.Errorf("%s stderr does not end with the receipt:\n%s\nwant suffix\n%s", name, outcome.stderr, receipt.stdout)
	}
	if outcome.stdout != "" && strings.Count(outcome.stdout, "\n") != 1 {
		h.t.Errorf("%s stdout is not one line: %q", name, outcome.stdout)
	}
}

// Oracle: plan T1. cw task verify prints the receipt on stderr after its one
// JSON line, when it fails (exit 5, state incomplete) and when it passes
// (exit 0, state complete); --json prints the same receipt as JSON.
func TestVerifyPrintsTheReceiptWhetherItPassesOrFails(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.startPlan(receiptPlan, false)).Directory
	failed := compactExecute(h, nil, "task", "verify", "--task", h.task)
	if ExitCode(failed.err) != ExitNegative {
		t.Fatalf("verify of a fresh task: exit %d (%v), want %d", ExitCode(failed.err), failed.err, ExitNegative)
	}
	expectReceipt(h, "failing verify", failed, "incomplete")

	ref := taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: h.fileSHA256()}
	reason := "Read-only source inspection; no executable behavior changed."
	h.taskInput("assess", taskflow.AssessmentInput{
		Requirements: []taskflow.Assessment{{ID: "source", Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason}},
		Decision:     &taskflow.Decision{Status: "resolved", Reason: "The source reference establishes the result.", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{}, NonExecutionReason: reason},
	}, true)
	passed := compactExecute(h, nil, "task", "verify", "--task", h.task)
	if passed.err != nil {
		t.Fatalf("verify of an assessed task: %v; stdout %s", passed.err, passed.stdout)
	}
	expectReceipt(h, "passing verify", passed, "complete")

	asJSON := compactExecute(h, nil, "task", "receipt", "--task", h.task, "--json")
	var receipt taskflow.TaskReceipt
	if err := json.Unmarshal([]byte(asJSON.stdout), &receipt); err != nil || receipt.Text() != passed.stderr || len(asJSON.stdout) > taskflow.MaxReceiptBytes {
		t.Fatalf("receipt --json = %s (%v); want the text receipt's struct", asJSON.stdout, err)
	}
}

// Oracle: plan T1. cw task abandon prints the receipt on stderr, state
// abandoned, after its one JSON line.
func TestAbandonPrintsTheReceipt(t *testing.T) {
	h := budgetStopHarness(t)
	abandoned := compactExecute(h, nil, "task", "abandon", "--task", h.task, "--reason", "superseded", "--summary")
	if abandoned.err != nil {
		t.Fatalf("abandon: %v; stderr %q", abandoned.err, abandoned.stderr)
	}
	expectReceipt(h, "abandon", abandoned, "abandoned")
}

// Oracle: plan T1 and C2. A budget stop prints the receipt in place of the
// usage summary: the estimated dollars against the budget, before the
// question for the owner.
func TestBudgetStopPrintsTheReceipt(t *testing.T) {
	h := budgetStopHarness(t)
	h.sampleUSD(0.85)
	for name, refused := range map[string]compactOutcome{"assign": h.assignBrief("stopped"), "run": h.runBudgetCheck()} {
		receipt := compactExecute(h, nil, "task", "receipt", "--task", h.task)
		at := strings.Index(refused.stderr, receipt.stdout)
		question := strings.Index(refused.stderr, "Ask the owner: continue with a higher budget")
		if ExitCode(refused.err) != ExitRefused || at < 0 || question < at || !strings.Contains(receipt.stdout, "usage: an estimated 0.85 USD, 85% of the budget 1.00 USD") {
			t.Errorf("%s: exit %d; stderr\n%s\nwant the receipt\n%s\nbefore the question", name, ExitCode(refused.err), refused.stderr, receipt.stdout)
		}
	}
}
