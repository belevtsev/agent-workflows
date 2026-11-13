package taskflow

import (
	"errors"
	"maps"
	"reflect"
	"strings"
	"testing"
	"time"
)

// round is one assignment of a counting case, built explicitly: its stage,
// the assignment it repeats, and the status of its acceptances in order.
type round struct {
	id, stage, repeats string
	accepted           []string
}

// roundRecord builds a record holding exactly rounds.
func roundRecord(rounds ...round) Record {
	var record Record
	for _, r := range rounds {
		record.Assignments = append(record.Assignments, WorkerAssignment{Brief: WorkerBrief{ID: r.id, Stage: r.stage, Repeats: r.repeats}})
		for _, status := range r.accepted {
			record.WorkerAcceptances = append(record.WorkerAcceptances, BoundWorkerAcceptance{Acceptance: WorkerAcceptance{AssignmentID: r.id, Status: status}})
		}
	}
	return record
}

// Oracle: worker-handoffs.md "Round budget" as plan R3 reads it, one rule per
// case; each case's count changes when its rule is removed.
func TestRoundsUsedCountsAuthorRoundsAsTheBudgetDefinesThem(t *testing.T) {
	for _, c := range []struct {
		name   string
		rounds []round
		want   map[string]int
	}{
		// The test engineer's first dispatch counts.
		{"first tests dispatch counts", []round{{id: "t1", stage: StageTests}}, map[string]int{StageTests: 1}},
		// Every later tests assignment counts, fixes or not.
		{"each tests round counts", []round{{id: "t1", stage: StageTests}, {id: "t2", stage: StageTests}}, map[string]int{StageTests: 2}},
		// The first spec and design are not revisions.
		{"first spec is not a revision", []round{{id: "s1", stage: StageSpec}}, map[string]int{}},
		{"spec revision counts", []round{{id: "s1", stage: StageSpec}, {id: "s2", stage: StageSpec}}, map[string]int{StageSpec: 1}},
		{"first design is not a revision", []round{{id: "d1", stage: StageDesign}, {id: "d2", stage: StageDesign}, {id: "d3", stage: StageDesign}}, map[string]int{StageDesign: 2}},
		// A replacement repeats an earlier round and does not count.
		{"repeat does not count", []round{{id: "t1", stage: StageTests}, {id: "t2", stage: StageTests, repeats: "t1"}}, map[string]int{StageTests: 1}},
		// An assignment closed as unused does not count, by its latest
		// acceptance only.
		{"unused does not count", []round{{id: "t1", stage: StageTests, accepted: []string{"accepted"}}, {id: "t2", stage: StageTests, accepted: []string{"unused"}}}, map[string]int{StageTests: 1}},
		{"latest acceptance decides", []round{{id: "t1", stage: StageTests, accepted: []string{"unused", "accepted"}}}, map[string]int{StageTests: 1}},
		{"unused first spec leaves the next as the first", []round{{id: "s1", stage: StageSpec, accepted: []string{"unused"}}, {id: "s2", stage: StageSpec}}, map[string]int{}},
		// Implementer fixes and reviews never count.
		{"implementation and reviews do not count", []round{{id: "i1", stage: StageImplementation}, {id: "i2", stage: StageImplementation}, {id: "r1", stage: StageTestReview}, {id: "r2", stage: StageCodeReview}}, map[string]int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := roundsUsed(roundRecord(c.rounds...)); !maps.Equal(got, c.want) {
				t.Fatalf("roundsUsed = %v, want %v", got, c.want)
			}
		})
	}
}

// Oracle: a stored approval needs the owner's words (bounded, no control
// characters), a known limit, and a time within the task, not after its
// assignment.
func TestStoredOwnerApprovalIsValidated(t *testing.T) {
	start := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	record := Record{CreatedAt: start}
	assigned := start.Add(time.Minute)
	for _, c := range []struct {
		name     string
		approval OwnerApproval
		ok       bool
	}{
		{"valid", OwnerApproval{Text: "one more round", For: ApprovalRoundBudget, At: assigned}, true},
		{"blank text", OwnerApproval{Text: "  ", For: ApprovalRoundBudget, At: assigned}, false},
		{"control character", OwnerApproval{Text: "one\nmore", For: ApprovalRoundBudget, At: assigned}, false},
		{"too long", OwnerApproval{Text: strings.Repeat("x", maxApprovalText+1), For: ApprovalRoundBudget, At: assigned}, false},
		{"unknown limit", OwnerApproval{Text: "yes", For: "anything", At: assigned}, false},
		{"before the task", OwnerApproval{Text: "yes", For: ApprovalRoundBudget, At: start.Add(-time.Second)}, false},
		{"after the assignment", OwnerApproval{Text: "yes", For: ApprovalRoundBudget, At: assigned.Add(time.Second)}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := validateStoredApproval(record, WorkerAssignment{AssignedAt: assigned, OwnerApproval: &c.approval})
			if (err == nil) != c.ok {
				t.Fatalf("validateStoredApproval = %v, want ok %v", err, c.ok)
			}
		})
	}
}

// testsRound assigns tests round id of a lite task, fixing finding fixes
// when it is set, and returns it with finding f1.
func testsRound(t *testing.T, record Record, ref SourceRef, id, fixes string) Record {
	t.Helper()
	b := stageBrief(id, "implementation", StageTests, "source.txt")
	if fixes != "" {
		b.Fixes = []FixRef{{Finding: fixes}}
	}
	return returnRound(t, mustAssignWorker(t, record, b), ref, id)
}

// Oracle: plan R3. Under lite (tests cap 2) the third tests round is refused
// with ErrOverBudget whose ErrRoundBudget names the stage, the rounds used,
// the cap and the finding it would answer, and no revision is written. The
// owner's words let it through and are stored with the assignment, and the
// record loads. Words on a round within the cap are refused as invalid
// input. A repeat needs no words.
func TestAssignRefusesARoundPastTheCapWithoutTheOwnersWords(t *testing.T) {
	record, ref := stageTask(t, false)
	record = testsRound(t, record, ref, "t1", "")
	if _, err := productionEnv.AssignApprovedWorker(t.Context(), record.Directory, stageBrief("x", "implementation", StageTests, "source.txt"), "yes"); err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("words on a round within the cap = %v, want ErrInvalidInput", err)
	}
	record = testsRound(t, record, ref, "t2", "t1/f1")

	third := stageBrief("t3", "implementation", StageTests, "source.txt")
	third.Fixes = []FixRef{{Finding: "t2/f1"}}
	before := revisions(t, record)
	_, err := AssignWorker(t.Context(), record.Directory, third)
	over, typed := errors.AsType[ErrRoundBudget](err)
	want := ErrRoundBudget{Stage: StageTests, Profile: "lite", Used: 2, Cap: 2, Needs: []string{"finding t2/f1 (major): The empty case is wrong."}}
	if !errors.Is(err, ErrOverBudget) || errors.Is(err, ErrInvalidInput) || !typed || !reflect.DeepEqual(over, want) {
		t.Fatalf("third tests round = %#v, want ErrOverBudget with %+v", err, want)
	}
	if after := revisions(t, record); after != before {
		t.Fatalf("a refused round wrote %d revisions", after-before)
	}

	record, err = productionEnv.AssignApprovedWorker(t.Context(), record.Directory, third, "one more round")
	if err != nil {
		t.Fatal(err)
	}
	record = returnRound(t, record, ref, "t3")
	repeat := stageBrief("t4", "implementation", StageTests, "source.txt")
	repeat.Repeats = "t3"
	record = mustAssignWorker(t, record, repeat)
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	got := findAssignment(loaded, "t3")
	if got.OwnerApproval == nil || *got.OwnerApproval != (OwnerApproval{Text: "one more round", For: ApprovalRoundBudget, At: got.AssignedAt}) || findAssignment(loaded, "t4").OwnerApproval != nil {
		t.Fatalf("stored approvals t3 %+v, t4 %+v", got.OwnerApproval, findAssignment(loaded, "t4").OwnerApproval)
	}
}

// Oracle: a record without a profile has no cap: a third tests round needs
// no words.
func TestRecordWithoutAProfileHasNoRoundCap(t *testing.T) {
	record, ref := stageTask(t, true)
	for _, id := range []string{"t1", "t2", "t3"} {
		record = testsRound(t, record, ref, id, "")
	}
	if loaded, err := Load(record.Directory); err != nil || len(loaded.Assignments) != 3 {
		t.Fatalf("Load = %d assignments, %v; want 3, nil", len(loaded.Assignments), err)
	}
}

// Oracle: the budget holds for a stored revision too: a hand-written
// revision that adds a third tests round without the owner's words, or a
// first round with words it does not need, fails to load.
func TestStoredRoundPastTheCapNeedsTheOwnersWords(t *testing.T) {
	for _, c := range []struct {
		name     string
		rounds   []string
		approval *OwnerApproval
	}{
		{"third round without words", []string{"t1", "t2"}, nil},
		{"first round with words", nil, &OwnerApproval{Text: "one more round", For: ApprovalRoundBudget}},
	} {
		t.Run(c.name, func(t *testing.T) {
			record, ref := stageTask(t, false)
			fixes := ""
			for _, id := range c.rounds {
				record = testsRound(t, record, ref, id, fixes)
				fixes = id + "/f1"
			}
			assigned := WorkerAssignment{TaskID: record.ID, Brief: stageBrief("tx", "implementation", StageTests, "source.txt"), Snapshot: currentWorkerSnapshot(t, record), AssignedAt: time.Now().UTC(), OwnerApproval: c.approval}
			if fixes != "" {
				assigned.Brief.Fixes = []FixRef{{Finding: fixes}}
			}
			if c.approval != nil {
				c.approval.At = assigned.AssignedAt
			}
			var err error
			if assigned.SHA256, err = assignmentHash(assigned); err != nil {
				t.Fatal(err)
			}
			appendWorkerRevision(t, record, func(r *Record) {
				r.Assignments = append(r.Assignments, assigned)
				r.EvidenceRevision++
				r.Verification = nil
			})
			if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "stored worker assignment tx goes past its stage's round cap without the owner's approval, or carries one it does not need") {
				t.Fatalf("Load = %v, want the stored budget refusal", err)
			}
		})
	}
}
