package cli

import (
	json "encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: plan R4 acceptance. Under lite, which allows 2 fix rounds, a third
// implementation brief fixing finding i0/f1 while it is still open exits 4
// (refused), lists the two earlier rounds, states both of the owner's
// options (one more fix round with --owner-approval, or a change of
// approach), and writes no revision. With --owner-approval it succeeds and
// the assignment records the words for "escalation".
func TestAssignAThirdFixRoundIsRefusedWithBothOptions(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	if record := h.load(); record.Profile == nil || record.Profile.FixRounds != 2 {
		t.Fatalf("precondition: profile %+v, want 2 fix rounds", record.Profile)
	}
	brief := func(id string, fixes ...taskflow.FixRef) taskflow.WorkerBrief {
		b := summaryCLIBrief()
		b.ID, b.AgentID, b.Role, b.Stage, b.Fixes = id, "/root/"+id, "implementation", taskflow.StageImplementation, fixes
		return b
	}
	handoff := func(id string) {
		result := summaryCLIResult()
		result["assignment_id"], result["status"], result["summary"] = id, "cancelled", "Round "+id+" changed the guard."
		result["findings"] = []any{map[string]any{"id": "f1", "summary": "The empty case is wrong.", "evidence": []any{summaryCLIUnhashedRef()}}}
		h.taskInput("handoff", result, true)
	}
	f1 := taskflow.FixRef{Finding: "i0/f1"}
	h.taskInput("assign", brief("i0"), true)
	handoff("i0")
	for _, id := range []string{"i1", "i2"} {
		h.taskInput("assign", brief(id, f1), true)
		handoff(id)
	}

	revisions := func() int {
		names, err := filepath.Glob(filepath.Join(h.task, "record-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		return len(names)
	}
	before := revisions()
	encoded, err := json.Marshal(brief("i3", f1))
	if err != nil {
		t.Fatal(err)
	}
	refused := compactExecute(h, encoded, "task", "assign", "--task", h.task, "--input", "-")
	if got := ExitCode(refused.err); got != ExitRefused {
		t.Fatalf("third fix round exits %d (%v), want %d; stderr %q", got, refused.err, ExitRefused, refused.stderr)
	}
	for _, want := range []string{
		"finding i0/f1 is still open after 2 fix rounds, the most profile lite allows",
		"i1: Round i1 changed the guard.",
		"i2: Round i2 changed the guard.",
		"approve one more fix round",
		"or change the approach",
		`A person must give the words: only then pass them, unchanged, as --owner-approval "<the owner's words>"`,
	} {
		if !strings.Contains(refused.stderr, want) {
			t.Errorf("refusal stderr %q lacks %q", refused.stderr, want)
		}
	}
	if after := revisions(); after != before {
		t.Fatalf("the refused fix round wrote %d revisions", after-before)
	}

	approved := compactExecute(h, encoded, "task", "assign", "--task", h.task, "--input", "-", "--owner-approval", "try once more")
	if approved.err != nil {
		t.Fatalf("approved fix round: %v; stderr %q", approved.err, approved.stderr)
	}
	got := h.load().Assignments[3]
	if len(got.OwnerApprovals) != 1 || got.OwnerApprovals[0].Text != "try once more" || got.OwnerApprovals[0].For != taskflow.ApprovalEscalation {
		t.Fatalf("assignment i3 approvals = %+v, want the owner's words for escalation", got.OwnerApprovals)
	}
}
