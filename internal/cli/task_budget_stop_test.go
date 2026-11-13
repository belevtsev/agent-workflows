package cli

import (
	json "encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// budgetStopPlan is summaryCLITestPlan with a 1 USD budget, which stops new
// work at 0.80 USD.
var budgetStopPlan = strings.Replace(summaryCLITestPlan, `"kind":"small",`, `"kind":"small","budget":{"max_usd":1},`, 1)

// budgetStopHarness starts a task under budgetStopPlan.
func budgetStopHarness(t *testing.T) *summaryCLIHarness {
	t.Helper()
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.startPlan(budgetStopPlan, false)).Directory
	if budget := h.load().Profile.Budget; budget.MaxUSD != 1 || budget.StopAt != 0.8 {
		t.Fatalf("precondition: budget %+v, want 1 USD stopping at 0.8", budget)
	}
	return h
}

// sampleModel runs cw task usage on a transcript of session s1 with one
// request of model and output tokens, and returns the sample's summary.
func (h *summaryCLIHarness) sampleModel(model string, output int64) taskUsageSummary {
	h.t.Helper()
	transcript := filepath.Join(h.t.TempDir(), "s1.jsonl")
	writeModelTranscript(h.t, transcript, model, -1, usageRequest{output: output})
	var summary taskUsageSummary
	if err := json.Unmarshal(h.execute(nil, "task", "usage", "--task", h.task, "--claude-transcript", transcript), &summary); err != nil {
		h.t.Fatal(err)
	}
	return summary
}

// sampleUSD stores a usage sample of session s1 whose built-in estimate is
// usd: claude-opus-5-5 output at 20 USD per million tokens. The sample must
// be stored, so the task's usage is usd.
func (h *summaryCLIHarness) sampleUSD(usd float64) {
	h.t.Helper()
	if summary := h.sampleModel("claude-opus-5-5", int64(usd*50_000)); !summary.Stored || !usdNear(summary.Usage.USD, usd) {
		h.t.Fatalf("sample of %v USD: stored %v, task usage %v; want stored and %v", usd, summary.Stored, summary.Usage.USD, usd)
	}
}

// sampleUnknown stores a sample of session s1 whose dollars are unknown: a
// model the built-in table does not list.
func (h *summaryCLIHarness) sampleUnknown() {
	h.t.Helper()
	if summary := h.sampleModel("claude-unlisted-1", 1000); !summary.Stored || summary.Usage.USD != nil {
		h.t.Fatalf("unknown sample: stored %v, usage %v; want stored and unknown", summary.Stored, summary.Usage.USD)
	}
}

// assignBrief runs cw task assign for an evidence brief with ID id and the
// extra arguments.
func (h *summaryCLIHarness) assignBrief(id string, extra ...string) compactOutcome {
	h.t.Helper()
	brief := summaryCLIBrief()
	brief.ID, brief.AgentID = id, "/root/"+id
	encoded, err := json.Marshal(brief)
	if err != nil {
		h.t.Fatal(err)
	}
	return compactExecute(h, encoded, append([]string{"task", "assign", "--task", h.task, "--input", "-"}, extra...)...)
}

// runBudgetCheck runs cw task run --force of check version with the extra
// arguments.
func (h *summaryCLIHarness) runBudgetCheck(extra ...string) compactOutcome {
	h.t.Helper()
	return compactExecute(h, nil, append([]string{"task", "run", "--task", h.task, "--check", "version", "--force"}, extra...)...)
}

func (h *summaryCLIHarness) revisions() int {
	h.t.Helper()
	names, err := filepath.Glob(filepath.Join(h.task, "record-*.json"))
	if err != nil {
		h.t.Fatal(err)
	}
	return len(names)
}

// verifyLimitations runs cw task verify, which must print a verification
// whatever it concludes, and returns its limitations.
func (h *summaryCLIHarness) verifyLimitations() []string {
	h.t.Helper()
	verified := compactExecute(h, nil, "task", "verify", "--task", h.task)
	var summary verifySummary
	if err := json.Unmarshal([]byte(verified.stdout), &summary); err != nil || summary.TaskID == "" {
		h.t.Fatalf("cw task verify printed %q (%v), stderr %q; want a verification", verified.stdout, verified.err, verified.stderr)
	}
	return summary.Limitations
}

func hasLimitation(limitations []string, prefix string) bool {
	for _, limitation := range limitations {
		if strings.HasPrefix(limitation, prefix) {
			return true
		}
	}
	return false
}

// expectStopped requires each outcome to exit 4 with each wanted text on
// stderr.
func expectStopped(t *testing.T, outcomes map[string]compactOutcome, wants ...string) {
	t.Helper()
	for name, refused := range outcomes {
		if got := ExitCode(refused.err); got != ExitRefused {
			t.Fatalf("%s exits %d (%v), want %d; stderr %q", name, got, refused.err, ExitRefused, refused.stderr)
		}
		for _, want := range wants {
			if !strings.Contains(refused.stderr, want) {
				t.Errorf("%s refusal stderr %q lacks %q", name, refused.stderr, want)
			}
		}
	}
}

// Oracle: plan C2 acceptance. With no sample, assign proceeds and verify
// lists "task usage is unknown: no usage sample is recorded". At 0.85 USD of
// a 1 USD budget, assign and run exit 4 and write no revision; stderr gives
// the usage, the budget, 85%, the dollars as estimated, the question and the
// approval for both; verify still runs, without the unknown limitation.
func TestAssignAtTheBudgetStopAsksAndVerifyStillRuns(t *testing.T) {
	h := budgetStopHarness(t)
	if got := h.assignBrief("first"); got.err != nil {
		t.Fatalf("assign without a sample: %v; stderr %q", got.err, got.stderr)
	}
	if limitations := h.verifyLimitations(); !hasLimitation(limitations, "task usage is unknown: no usage sample is recorded") {
		t.Fatalf("verify limitations %q lack the unknown usage", limitations)
	}

	h.sampleUSD(0.85)
	before := h.revisions()
	expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("second"), "run": h.runBudgetCheck()},
		"estimated 0.85 USD, 85% of its 1.00 USD budget, which stops new work at 80%", "cw wrote nothing", `"usd_estimated":true`,
		"continue with a higher budget, or stop with the partial result?", `on this cw task assign or cw task run as --owner-approval "cost-budget=`)
	if after := h.revisions(); after != before {
		t.Fatalf("the refused assign and run wrote %d revisions", after-before)
	}
	if limitations := h.verifyLimitations(); hasLimitation(limitations, "task usage is unknown") {
		t.Fatalf("verify limitations %q report known usage as unknown", limitations)
	}
}

// Oracle (review finding 1): with a 1 USD budget, 5% is 0.05 USD. A sample
// at 0.82 after one at 0.78 moves less than that, yet crosses the stop, so it
// is stored and assign exits 4; likewise 1.02 after 0.98 crosses 100% under
// the owner's approval.
func TestASampleCrossingTheStopIsAlwaysStored(t *testing.T) {
	h := budgetStopHarness(t)
	h.sampleUSD(0.78)
	h.sampleUSD(0.82)
	expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("at-82")}, "82% of its 1.00 USD budget")
	if approved := h.assignBrief("approved", "--owner-approval", "cost-budget=go on"); approved.err != nil {
		t.Fatalf("approved assign: %v; stderr %q", approved.err, approved.stderr)
	}
	h.sampleUSD(0.98)
	h.sampleUSD(1.02)
	expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("at-102")}, "102% of its 1.00 USD budget", "start a new task with a higher budget")
}

// Oracle (review finding 2): a later sample of the session whose dollars are
// unknown makes the task's usage unknown, but its known lower bound still
// stops new work: at 85% assign and run exit 4 saying "at least"; at 120%,
// after the owner's approval, too, with only a new task continuing.
func TestUnknownUsageNeverLiftsAReachedStop(t *testing.T) {
	h := budgetStopHarness(t)
	h.sampleUSD(0.85)
	h.sampleUnknown()
	expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("unknown-85"), "run": h.runBudgetCheck()}, "at least an estimated 0.85 USD, 85%")

	h = budgetStopHarness(t)
	h.sampleUSD(0.85)
	if approved := h.assignBrief("approved", "--owner-approval", "cost-budget=go on"); approved.err != nil {
		t.Fatalf("approved assign: %v; stderr %q", approved.err, approved.stderr)
	}
	h.sampleUSD(1.2)
	h.sampleUnknown()
	expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("unknown-120"), "run": h.runBudgetCheck()},
		"at least an estimated 1.20 USD, 120%", "start a new task with a higher budget")
}

// Oracle (review finding 4): the cost-budget approval is task-level. Below
// the stop it is refused on run and assign (exit 1, no revision). At 85%
// `cw task run --owner-approval cost-budget=WORDS` runs the check and
// records the words on the task, not on an assignment; assign then proceeds
// without words, and words again are refused, as the task is no longer at
// its stop. At 100% assign and run are refused again, with or without
// words, saying only a new task continues.
func TestCostBudgetApprovalIsTaskLevelAndRaisesTheStopOnce(t *testing.T) {
	h := budgetStopHarness(t)
	h.sampleUSD(0.5)
	before := h.revisions()
	for name, early := range map[string]compactOutcome{
		"assign": h.assignBrief("early", "--owner-approval", "cost-budget=go on"),
		"run":    h.runBudgetCheck("--owner-approval", "cost-budget=go on"),
	} {
		if ExitCode(early.err) != ExitFailure || !strings.Contains(early.err.Error(), "does not go past cost-budget, so it takes no owner approval") {
			t.Fatalf("%s approval below the stop: exit %d (%v); want exit 1, no approval below the stop", name, ExitCode(early.err), early.err)
		}
	}
	if h.revisions() != before {
		t.Fatalf("approvals below the stop wrote %d revisions", h.revisions()-before)
	}

	h.sampleUSD(0.85)
	if approved := h.runBudgetCheck("--owner-approval", "spend up to the whole dollar"); approved.err != nil {
		t.Fatalf("approved run: %v; stderr %q", approved.err, approved.stderr)
	}
	record := h.load()
	if got := record.OwnerApprovals; len(got) != 1 || got[0].For != taskflow.ApprovalCostBudget || got[0].Text != "spend up to the whole dollar" {
		t.Fatalf("record approvals %+v, want the owner's words for cost-budget", got)
	}
	if next := h.assignBrief("next"); next.err != nil {
		t.Fatalf("assign below 100%% after the approval: %v; stderr %q", next.err, next.stderr)
	}
	if again := h.assignBrief("again", "--owner-approval", "cost-budget=more"); ExitCode(again.err) != ExitFailure {
		t.Fatalf("a second approval: exit %d (%v), want 1", ExitCode(again.err), again.err)
	}
	for _, assigned := range h.load().Assignments {
		if len(assigned.OwnerApprovals) != 0 {
			t.Fatalf("assignment %s carries %+v; cost approvals belong to the task", assigned.Brief.ID, assigned.OwnerApprovals)
		}
	}

	h.sampleUSD(1)
	for _, extra := range [][]string{nil, {"--owner-approval", "cost-budget=more"}} {
		expectStopped(t, map[string]compactOutcome{"assign": h.assignBrief("final", extra...), "run": h.runBudgetCheck(extra...)}, "start a new task with a higher budget")
	}
}
