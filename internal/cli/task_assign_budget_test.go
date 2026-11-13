package cli

import (
	json "encoding/json/v2"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: plan R3 acceptance. Under lite, whose tests cap is 2, the third
// tests round exits 4 (refused), names the finding it would answer, says a
// person must give the words for --owner-approval, and writes no revision.
// With --owner-approval "one more round" it succeeds, and
// `cw task show --field assignments` shows the approval on the assignment.
func TestAssignPastTheRoundCapIsRefusedUntilTheOwnerApproves(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	if record := h.load(); record.Profile == nil || record.Profile.Rounds[taskflow.StageTests] != 2 {
		t.Fatalf("precondition: profile %+v, want tests cap 2", record.Profile)
	}
	brief := func(id, fixes string) taskflow.WorkerBrief {
		b := summaryCLIBrief()
		b.ID, b.AgentID, b.Role, b.Stage = id, "/root/"+id, "implementation", taskflow.StageTests
		if fixes != "" {
			b.Fixes = []taskflow.FixRef{{Finding: fixes}}
		}
		return b
	}
	handoff := func(id string) {
		result := summaryCLIResult()
		result["assignment_id"], result["status"] = id, "cancelled"
		result["findings"] = []any{map[string]any{"id": "f1", "summary": "The empty case is wrong.", "evidence": []any{summaryCLIUnhashedRef()}}}
		h.taskInput("handoff", result, true)
	}
	h.taskInput("assign", brief("t1", ""), true)
	handoff("t1")
	h.taskInput("assign", brief("t2", "t1/f1"), true)
	handoff("t2")

	revisions := func() int {
		names, err := filepath.Glob(filepath.Join(h.task, "record-*.json"))
		if err != nil {
			t.Fatal(err)
		}
		return len(names)
	}
	before := revisions()
	encoded, err := json.Marshal(brief("t3", "t2/f1"))
	if err != nil {
		t.Fatal(err)
	}
	refused := compactExecute(h, encoded, "task", "assign", "--task", h.task, "--input", "-")
	if got := ExitCode(refused.err); got != ExitRefused {
		t.Fatalf("third tests round exits %d (%v), want %d; stderr %q", got, refused.err, ExitRefused, refused.stderr)
	}
	for _, want := range []string{"stage tests has used 2 of the 2 author rounds profile lite allows", "finding t2/f1 (major): The empty case is wrong.", "A person must give the words", `--owner-approval "<the owner's words>"`} {
		if !strings.Contains(refused.stderr, want) {
			t.Errorf("refusal stderr %q lacks %q", refused.stderr, want)
		}
	}
	if after := revisions(); after != before {
		t.Fatalf("the refused round wrote %d revisions", after-before)
	}

	approved := compactExecute(h, encoded, "task", "assign", "--task", h.task, "--input", "-", "--owner-approval", "one more round")
	if approved.err != nil {
		t.Fatalf("approved round: %v; stderr %q", approved.err, approved.stderr)
	}
	shown := compactExecute(h, nil, "task", "show", "--task", h.task, "--field", "assignments")
	var fields struct {
		Assignments []taskflow.WorkerAssignment `json:"assignments"`
	}
	if err := json.Unmarshal([]byte(shown.stdout), &fields); err != nil || len(fields.Assignments) != 3 {
		t.Fatalf("task show --field assignments = %q, %v; want three assignments", shown.stdout, err)
	}
	got := fields.Assignments[2]
	if got.OwnerApproval == nil || got.OwnerApproval.Text != "one more round" || got.OwnerApproval.For != taskflow.ApprovalRoundBudget || !got.OwnerApproval.At.Equal(got.AssignedAt) {
		t.Fatalf("assignment t3 approval = %+v, want the owner's words for round-budget at %v", got.OwnerApproval, got.AssignedAt)
	}
}
