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

// sampleUSD stores a usage sample of session s1 whose built-in estimate is
// usd: claude-opus-5-5 output at 20 USD per million tokens.
func (h *summaryCLIHarness) sampleUSD(usd float64) {
	h.t.Helper()
	transcript := filepath.Join(h.t.TempDir(), "s1.jsonl")
	writeUsageTranscript(h.t, transcript, -1, usageRequest{output: int64(usd * 50_000)})
	h.execute(nil, "task", "usage", "--task", h.task, "--claude-transcript", transcript)
	if got := taskflow.UsageOf(h.load()).USD; !usdNear(got, usd) {
		h.t.Fatalf("precondition: task usage %v, want %v USD", got, usd)
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

// Oracle: plan C2 acceptance. With no sample, assign proceeds and verify
// lists "task usage is unknown: no usage sample is recorded". At 0.85 USD of
// a 1 USD budget, assign and run exit 4 and write no revision; stderr gives
// the usage, the budget, 85%, the dollars as estimated and the question;
// verify still runs, without the unknown limitation.
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
	for name, refused := range map[string]compactOutcome{
		"assign": h.assignBrief("second"),
		"run":    compactExecute(h, nil, "task", "run", "--task", h.task, "--check", "version", "--force"),
	} {
		if got := ExitCode(refused.err); got != ExitRefused {
			t.Fatalf("%s at 85%% exits %d (%v), want %d; stderr %q", name, got, refused.err, ExitRefused, refused.stderr)
		}
		for _, want := range []string{"estimated 0.85 USD, 85% of its 1.00 USD budget, which stops new work at 80%", "cw wrote nothing",
			`"usd_estimated":true`, "continue with a higher budget, or stop with the partial result?", `--owner-approval "cost-budget=`} {
			if !strings.Contains(refused.stderr, want) {
				t.Errorf("%s refusal stderr %q lacks %q", name, refused.stderr, want)
			}
		}
	}
	if after := h.revisions(); after != before {
		t.Fatalf("the refused assign and run wrote %d revisions", after-before)
	}
	if limitations := h.verifyLimitations(); hasLimitation(limitations, "task usage is unknown") {
		t.Fatalf("verify limitations %q report known usage as unknown", limitations)
	}
}

// Oracle: the owner's cost-budget approval is refused below the stop (exit 1,
// no revision); at 85% it lets the assignment through and is recorded on it
// with for "cost-budget"; assign then proceeds without words until 100%, and
// at 100% is refused again, with or without words, saying only a new task
// continues.
func TestCostBudgetApprovalRaisesTheStopToTheWholeBudgetOnce(t *testing.T) {
	h := budgetStopHarness(t)
	h.sampleUSD(0.5)
	before := h.revisions()
	if early := h.assignBrief("early", "--owner-approval", "cost-budget=go on"); ExitCode(early.err) != ExitFailure || h.revisions() != before {
		t.Fatalf("approval below the stop: exit %d (%v), %d revisions; want exit 1 and none", ExitCode(early.err), early.err, h.revisions()-before)
	}

	h.sampleUSD(0.85)
	if approved := h.assignBrief("approved", "--owner-approval", "cost-budget=spend up to the whole dollar"); approved.err != nil {
		t.Fatalf("approved assign: %v; stderr %q", approved.err, approved.stderr)
	}
	record := h.load()
	got := record.Assignments[len(record.Assignments)-1].OwnerApprovals
	if len(got) != 1 || got[0].For != taskflow.ApprovalCostBudget || got[0].Text != "spend up to the whole dollar" {
		t.Fatalf("approvals %+v, want the owner's words for cost-budget", got)
	}
	if next := h.assignBrief("next"); next.err != nil {
		t.Fatalf("assign below 100%% after the approval: %v; stderr %q", next.err, next.stderr)
	}

	h.sampleUSD(1)
	for _, extra := range [][]string{nil, {"--owner-approval", "cost-budget=more"}} {
		refused := h.assignBrief("final", extra...)
		if ExitCode(refused.err) != ExitRefused || !strings.Contains(refused.stderr, "start a new task with a higher budget") {
			t.Fatalf("assign at 100%% with %q: exit %d (%v), stderr %q; want exit 4 and a new task", extra, ExitCode(refused.err), refused.err, refused.stderr)
		}
	}
}
