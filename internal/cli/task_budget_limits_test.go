package cli

import (
	json "encoding/json/v2"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: plans R4 and C2 together. Under lite (tests cap 2, 2 fix rounds)
// with 0.85 USD used of a 1 USD budget, a third tests round that is also a
// third fix round on open finding i0/f1 exits 4 with no revision and a
// refusal naming round-budget, escalation and cost-budget, the round's
// needs, the earlier fix rounds and the budget question. Words for only some
// limits are refused naming the rest; bare words are refused as several
// limits. Words for all three succeed: round-budget and escalation go on the
// assignment, cost-budget on the record, and the record loads again.
func TestABriefPastEveryLimitNamesThemAllAndTakesWordsForEach(t *testing.T) {
	h := budgetStopHarness(t)
	brief := func(id, stage string, fixes ...taskflow.FixRef) []byte {
		b := summaryCLIBrief()
		b.ID, b.AgentID, b.Role, b.Stage, b.Fixes = id, "/root/"+id, "implementation", stage, fixes
		encoded, err := json.Marshal(b)
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	handoff := func(id string) {
		result := summaryCLIResult()
		result["assignment_id"], result["status"], result["summary"] = id, "cancelled", "Round "+id+" changed the guard."
		result["findings"] = []any{map[string]any{"id": "f1", "summary": "The empty case is wrong.", "evidence": []any{summaryCLIUnhashedRef()}}}
		h.taskInput("handoff", result, true)
	}
	assign := func(encoded []byte, extra ...string) compactOutcome {
		return compactExecute(h, encoded, append([]string{"task", "assign", "--task", h.task, "--input", "-"}, extra...)...)
	}
	f1 := taskflow.FixRef{Finding: "i0/f1"}
	for _, step := range []struct {
		id, stage string
		fixes     []taskflow.FixRef
	}{{"i0", taskflow.StageImplementation, nil}, {"t0", taskflow.StageTests, []taskflow.FixRef{f1}}, {"t1", taskflow.StageTests, []taskflow.FixRef{f1}}} {
		if out := assign(brief(step.id, step.stage, step.fixes...)); out.err != nil {
			t.Fatalf("assign %s: %v; stderr %q", step.id, out.err, out.stderr)
		}
		handoff(step.id)
	}
	h.sampleUSD(0.85)

	t2 := brief("t2", taskflow.StageTests, f1)
	before := h.revisions()
	expectStopped(t, map[string]compactOutcome{"assign": assign(t2)},
		"round-budget", "escalation", "cost-budget",
		"Earlier fix rounds on finding i0/f1:",
		"The cost budget: task usage is an estimated 0.85 USD, 85% of its 1.00 USD budget",
		budgetQuestionText,
	)
	// Each limit without words is refused by its own refusal, whatever the
	// words for the others.
	refusals := map[string]string{
		taskflow.ApprovalRoundBudget: "stage tests has used 2 of the 2 author rounds profile lite allows",
		taskflow.ApprovalEscalation:  "finding i0/f1 is still open after 2 fix rounds",
		taskflow.ApprovalCostBudget:  "task usage is an estimated 0.85 USD, 85% of its 1.00 USD budget",
	}
	words := map[string]string{
		taskflow.ApprovalRoundBudget: "round-budget=a third tests round",
		taskflow.ApprovalEscalation:  "escalation=one more fix round",
		taskflow.ApprovalCostBudget:  "cost-budget=spend the whole dollar",
	}
	for missing, refusal := range refusals {
		var partial []string
		for limit, text := range words {
			if limit != missing {
				partial = append(partial, "--owner-approval", text)
			}
		}
		refused := assign(t2, partial...)
		expectStopped(t, map[string]compactOutcome{"without " + missing: refused}, refusal)
		for given, other := range refusals {
			if given != missing && strings.Contains(firstLine(refused.stderr), other) {
				t.Errorf("without %s: refusal %q also refuses %s, which has words", missing, firstLine(refused.stderr), given)
			}
		}
	}
	if bare := assign(t2, "--owner-approval", "go on"); ExitCode(bare.err) != ExitFailure {
		t.Fatalf("bare words past several limits: exit %d (%v), want 1", ExitCode(bare.err), bare.err)
	}
	if h.revisions() != before {
		t.Fatalf("refused briefs wrote %d revisions", h.revisions()-before)
	}

	approved := assign(t2, "--owner-approval", "round-budget=a third tests round", "--owner-approval", "escalation=one more fix round", "--owner-approval", "cost-budget=spend the whole dollar")
	if approved.err != nil {
		t.Fatalf("approved brief: %v; stderr %q", approved.err, approved.stderr)
	}
	record := h.load()
	got := record.Assignments[len(record.Assignments)-1]
	var limits []string
	for _, approval := range got.OwnerApprovals {
		limits = append(limits, approval.For)
	}
	slices.Sort(limits)
	if !slices.Equal(limits, []string{taskflow.ApprovalEscalation, taskflow.ApprovalRoundBudget}) {
		t.Fatalf("assignment t2 approvals %+v, want round-budget and escalation", got.OwnerApprovals)
	}
	if approvals := record.OwnerApprovals; len(approvals) != 1 || approvals[0].For != taskflow.ApprovalCostBudget || approvals[0].Text != "spend the whole dollar" {
		t.Fatalf("record approvals %+v, want the owner's words for cost-budget", approvals)
	}
}

// budgetQuestionText is the question a budget stop asks the owner.
const budgetQuestionText = "continue with a higher budget, or stop with the partial result?"

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}
