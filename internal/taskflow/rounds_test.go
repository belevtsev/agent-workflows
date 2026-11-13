package taskflow

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// round is one assignment of a counting case, built explicitly: its stage,
// whether it fixes a finding, the assignment it repeats, its handoff status
// ("" before it returned) and the status of its acceptances in order.
type round struct {
	id, stage, repeats string
	fixes              bool
	result             string
	accepted           []string
}

// roundRecord builds a record holding exactly rounds.
func roundRecord(rounds ...round) Record {
	var record Record
	for _, r := range rounds {
		brief := WorkerBrief{ID: r.id, Stage: r.stage, Repeats: r.repeats}
		if r.fixes {
			brief.Fixes = []FixRef{{Requirement: "behavior"}}
		}
		record.Assignments = append(record.Assignments, WorkerAssignment{Brief: brief})
		if r.result != "" {
			record.WorkerResults = append(record.WorkerResults, BoundWorkerResult{Result: WorkerResult{AssignmentID: r.id, Status: r.result}})
		}
		for _, status := range r.accepted {
			record.WorkerAcceptances = append(record.WorkerAcceptances, BoundWorkerAcceptance{Acceptance: WorkerAcceptance{AssignmentID: r.id, Status: status}})
		}
	}
	return record
}

// Oracle: worker-handoffs.md "Round budget", one rule per case; each case's
// count changes when its rule is removed (the PR body lists the mutations).
func TestRoundsUsedCountsAuthorRoundsAsTheBudgetDefinesThem(t *testing.T) {
	const done, cancelled = "completed", "cancelled"
	for _, c := range []struct {
		name   string
		rounds []round
		want   map[string]int
	}{
		// The test engineer's first dispatch counts.
		{"first tests dispatch counts", []round{{id: "t1", stage: StageTests}}, map[string]int{StageTests: 1}},
		// Briefs assigned before any of their stage returned are one round.
		{"parallel first briefs are one round", []round{{id: "t1a", stage: StageTests}, {id: "t1b", stage: StageTests}}, map[string]int{StageTests: 1}},
		// Every brief with fixes is a round of its own.
		{"each fix round counts", []round{{id: "t1", stage: StageTests}, {id: "t2", stage: StageTests, fixes: true}, {id: "t3", stage: StageTests, fixes: true}}, map[string]int{StageTests: 3}},
		// The first spec and design are not revisions.
		{"first spec is not a revision", []round{{id: "s1", stage: StageSpec}}, map[string]int{}},
		{"parallel first designs are no revision", []round{{id: "d1a", stage: StageDesign}, {id: "d1b", stage: StageDesign}}, map[string]int{}},
		{"design revisions count", []round{{id: "d1", stage: StageDesign}, {id: "d2", stage: StageDesign, fixes: true}, {id: "d3", stage: StageDesign, fixes: true}}, map[string]int{StageDesign: 2}},
		// A replacement of a round that counts is no new round, nor is a
		// replacement of that replacement.
		{"repeat of a counted round is free", []round{{id: "t1", stage: StageTests}, {id: "t2", stage: StageTests, repeats: "t1", result: cancelled, accepted: []string{"unused"}}, {id: "t3", stage: StageTests, repeats: "t2"}}, map[string]int{StageTests: 1}},
		// A replacement of a round that returned no work takes its place.
		{"repeat of a round without work counts", []round{{id: "t1", stage: StageTests, result: cancelled, accepted: []string{"unused"}}, {id: "t2", stage: StageTests, repeats: "t1"}}, map[string]int{StageTests: 1}},
		{"repeat of a first-round brief joins the first round", []round{{id: "t1a", stage: StageTests, result: done}, {id: "t1b", stage: StageTests, result: cancelled, accepted: []string{"unused"}}, {id: "t2", stage: StageTests, repeats: "t1b"}}, map[string]int{StageTests: 1}},
		// A round closed unused that returned no work does not count.
		{"unused without completed work does not count", []round{{id: "t1", stage: StageTests, result: done}, {id: "t2", stage: StageTests, fixes: true, result: cancelled, accepted: []string{"unused"}}}, map[string]int{StageTests: 1}},
		// A round whose completed work was closed unused, as a superseded
		// round is, still counts.
		{"superseded completed round counts", []round{{id: "t1", stage: StageTests, result: done, accepted: []string{"unused"}}, {id: "t2", stage: StageTests, fixes: true}}, map[string]int{StageTests: 2}},
		// The latest acceptance decides.
		{"latest acceptance decides", []round{{id: "t1", stage: StageTests, result: cancelled, accepted: []string{"unused", "rejected"}}}, map[string]int{StageTests: 1}},
		// Implementer fixes and reviews never count.
		{"implementation and reviews do not count", []round{{id: "i1", stage: StageImplementation}, {id: "i2", stage: StageImplementation, fixes: true}, {id: "r1", stage: StageTestReview}, {id: "r2", stage: StageCodeReview}}, map[string]int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := roundsUsed(roundRecord(c.rounds...)); !maps.Equal(got, c.want) {
				t.Fatalf("roundsUsed = %v, want %v", got, c.want)
			}
		})
	}
}

// Oracle: a stored approval needs the owner's words (bounded, no control
// characters), a known limit named once on the assignment, and a time within
// the task, not after its assignment.
func TestStoredOwnerApprovalsAreValidated(t *testing.T) {
	start := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	record := Record{CreatedAt: start}
	assigned := start.Add(time.Minute)
	valid := OwnerApproval{Text: "one more round", For: ApprovalRoundBudget, At: assigned}
	for _, c := range []struct {
		name      string
		approvals []OwnerApproval
		ok        bool
	}{
		{"valid", []OwnerApproval{valid}, true},
		{"blank text", []OwnerApproval{{Text: "  ", For: ApprovalRoundBudget, At: assigned}}, false},
		{"control character", []OwnerApproval{{Text: "one\nmore", For: ApprovalRoundBudget, At: assigned}}, false},
		{"too long", []OwnerApproval{{Text: strings.Repeat("x", maxApprovalText+1), For: ApprovalRoundBudget, At: assigned}}, false},
		{"unknown limit", []OwnerApproval{{Text: "yes", For: "anything", At: assigned}}, false},
		{"before the task", []OwnerApproval{{Text: "yes", For: ApprovalRoundBudget, At: start.Add(-time.Second)}}, false},
		{"after the assignment", []OwnerApproval{{Text: "yes", For: ApprovalRoundBudget, At: assigned.Add(time.Second)}}, false},
		{"one limit twice", []OwnerApproval{valid, valid}, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := validateStoredApprovals(record, WorkerAssignment{AssignedAt: assigned, OwnerApprovals: c.approvals})
			if (err == nil) != c.ok {
				t.Fatalf("validateStoredApprovals = %v, want ok %v", err, c.ok)
			}
		})
	}
}

// Oracle: each --owner-approval value approves one limit the brief goes
// past: TEXT alone for the only one, LIMIT=TEXT to name it. Words for a
// limit not gone past, plain words past two limits, or one limit twice are
// invalid input; a limit without words is refused with its own refusal, two
// with one that names both.
func TestOwnerApprovalsApproveEachLimitOnce(t *testing.T) {
	const cost = "cost-budget"
	saved := approvalLimits
	approvalLimits = append(slices.Clip(approvalLimits), cost)
	t.Cleanup(func() { approvalLimits = saved })
	roundRefusal := kindError{kind: ErrOverBudget, msg: "round refusal"}
	round := overLimit{For: ApprovalRoundBudget, refusal: roundRefusal}
	both := []overLimit{round, {For: cost, refusal: kindError{kind: ErrOverBudget, msg: "cost refusal"}}}
	for _, c := range []struct {
		name    string
		over    []overLimit
		words   []string
		want    []string // For=Text of each approval
		refused string
	}{
		{"plain words for the only limit", []overLimit{round}, []string{"yes"}, []string{"round-budget=yes"}, ""},
		{"named limit", []overLimit{round}, []string{"round-budget=yes"}, []string{"round-budget=yes"}, ""},
		{"two limits each named", both, []string{"cost-budget=b", "round-budget=a"}, []string{"cost-budget=b", "round-budget=a"}, ""},
		{"no limit gone past", nil, []string{"yes"}, nil, "worker brief b goes past no limit of the task, so it takes no owner approval"},
		{"a limit not gone past", []overLimit{round}, []string{"cost-budget=yes"}, nil, "worker brief b does not go past cost-budget"},
		{"plain words past two limits", both, []string{"yes"}, nil, "worker brief b goes past 2 limits (round-budget, cost-budget)"},
		{"one limit twice", []overLimit{round}, []string{"yes", "round-budget=again"}, nil, "worker brief b has the owner's approval for round-budget twice"},
		{"one of two limits without words", both, []string{"round-budget=a"}, nil, "cost refusal"},
		{"two limits without words", both, nil, nil, "worker brief b goes past round-budget and cost-budget; each needs the owner's approval"},
	} {
		t.Run(c.name, func(t *testing.T) {
			approvals, err := ownerApprovals(c.over, "worker brief b", c.words)
			if c.refused != "" {
				if err == nil || !strings.HasPrefix(err.Error(), c.refused) {
					t.Fatalf("ownerApprovals = %v, want a refusal starting %q", err, c.refused)
				}
				return
			}
			var got []string
			for _, a := range approvals {
				got = append(got, a.For+"="+a.Text)
			}
			if err != nil || !slices.Equal(got, c.want) {
				t.Fatalf("ownerApprovals = %v, %v; want %v", got, err, c.want)
			}
		})
	}
}

// assignRound assigns brief, of the task's lite profile, fixing finding fixes
// when it is set, and records a handoff of status with finding f1.
func assignRound(t *testing.T, record Record, ref SourceRef, brief WorkerBrief, fixes, status string) Record {
	t.Helper()
	if fixes != "" {
		brief.Fixes = []FixRef{{Finding: fixes}}
	}
	record = mustAssignWorker(t, record, brief)
	result := workerResult(t, record, brief.ID, status, ref)
	result.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}}}
	return mustSubmitWorker(t, record, result)
}

// testsRound is assignRound of a tests brief on source.txt, returned
// cancelled.
func testsRound(t *testing.T, record Record, ref SourceRef, id, fixes string) Record {
	t.Helper()
	return assignRound(t, record, ref, stageBrief(id, "implementation", StageTests, "source.txt"), fixes, "cancelled")
}

// Oracle: plan R3. Under lite (tests cap 2) the third tests round is refused
// with ErrOverBudget whose ErrRoundBudget names the stage, the rounds used,
// the cap and the finding it would answer, and no revision is written. The
// owner's words let it through and are stored with the assignment, and the
// record loads. Words on a round within the cap are refused for exactly
// that reason. A repeat needs no words.
func TestAssignRefusesARoundPastTheCapWithoutTheOwnersWords(t *testing.T) {
	record, ref := stageTask(t, false)
	record = testsRound(t, record, ref, "t1", "")
	within := stageBrief("x", "implementation", StageTests, "source.txt")
	within.Fixes = []FixRef{{Finding: "t1/f1"}}
	if _, err := productionEnv.AssignApprovedWorker(t.Context(), record.Directory, within, []string{"yes"}); !errors.Is(err, ErrInvalidInput) || err.Error() != "worker brief x goes past no limit of the task, so it takes no owner approval" {
		t.Fatalf("words on a round within the cap = %v, want the no-limit refusal", err)
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

	record, err = productionEnv.AssignApprovedWorker(t.Context(), record.Directory, third, []string{"one more round"})
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
	if wantApprovals := []OwnerApproval{{Text: "one more round", For: ApprovalRoundBudget, At: got.AssignedAt}}; !reflect.DeepEqual(got.OwnerApprovals, wantApprovals) || findAssignment(loaded, "t4").OwnerApprovals != nil {
		t.Fatalf("stored approvals t3 %+v, t4 %+v", got.OwnerApprovals, findAssignment(loaded, "t4").OwnerApprovals)
	}
}

// Oracle: closing a completed round unused, as a superseded round is
// closed, does not give the round back: under lite, after t1 (completed,
// then closed unused) and t2, a third tests round is still refused.
func TestASupersededRoundStillCounts(t *testing.T) {
	record, ref := stageTask(t, false)
	record = assignRound(t, record, ref, stageBrief("t1", "implementation", StageTests, "source.txt"), "", "completed")
	record = assignRound(t, record, ref, stageBrief("t2", "implementation", StageTests, "source.txt"), "t1/f1", "completed")
	closed := workerAcceptance(t, record, "t1", "unused", ref)
	closed.Findings = []FindingDisposition{{FindingID: "f1", Status: "rejected", Reason: "Superseded by round t2.", Evidence: []SourceRef{ref}}}
	record = mustAcceptWorker(t, record, closed)
	third := stageBrief("t3", "implementation", StageTests, "source.txt")
	third.Fixes = []FixRef{{Finding: "t2/f1"}}
	if _, err := AssignWorker(t.Context(), record.Directory, third); !errors.Is(err, ErrOverBudget) {
		t.Fatalf("third tests round after a superseded one = %v, want ErrOverBudget", err)
	}
}

// Oracle: a brief that fixes findings is a new round, so it may not also
// repeat an earlier assignment, which would make it free.
func TestABriefWithFixesRepeatsNothing(t *testing.T) {
	record, ref := stageTask(t, false)
	record = testsRound(t, record, ref, "t1", "")
	both := stageBrief("t2", "implementation", StageTests, "source.txt")
	both.Fixes, both.Repeats = []FixRef{{Finding: "t1/f1"}}, "t1"
	if _, err := AssignWorker(t.Context(), record.Directory, both); !errors.Is(err, ErrInvalidInput) || !strings.HasPrefix(err.Error(), "worker brief t2 carries both fixes and repeats") {
		t.Fatalf("brief with fixes and repeats = %v, want the fixes-and-repeats refusal", err)
	}
}

// Oracle: briefs of a stage assigned before any of them returned are one
// round: under lite, parallel t1a and t1b, then t2 fixing t1a/f1, is two
// rounds, so t2 needs no words and t3 is the one refused.
func TestParallelFirstBriefsAreOneRound(t *testing.T) {
	record, ref := stageTask(t, false)
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "other.txt"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, stageBrief("t1a", "implementation", StageTests, "source.txt"))
	record = mustAssignWorker(t, record, stageBrief("t1b", "implementation", StageTests, "other.txt"))
	record = returnRound(t, record, ref, "t1a")
	record = returnRound(t, record, ref, "t1b")
	record = testsRound(t, record, ref, "t2", "t1a/f1")
	third := stageBrief("t3", "implementation", StageTests, "source.txt")
	third.Fixes = []FixRef{{Finding: "t2/f1"}}
	_, err := AssignWorker(t.Context(), record.Directory, third)
	if over, ok := errors.AsType[ErrRoundBudget](err); !ok || over.Used != 2 {
		t.Fatalf("third round after parallel first briefs = %v, want ErrRoundBudget with 2 used", err)
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
		name      string
		rounds    []string
		approvals []OwnerApproval
	}{
		{"third round without words", []string{"t1", "t2"}, nil},
		{"first round with words", nil, []OwnerApproval{{Text: "one more round", For: ApprovalRoundBudget}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			record, ref := stageTask(t, false)
			fixes := ""
			for _, id := range c.rounds {
				record = testsRound(t, record, ref, id, fixes)
				fixes = id + "/f1"
			}
			assigned := WorkerAssignment{TaskID: record.ID, Brief: stageBrief("tx", "implementation", StageTests, "source.txt"), Snapshot: currentWorkerSnapshot(t, record), AssignedAt: time.Now().UTC(), OwnerApprovals: c.approvals}
			if fixes != "" {
				assigned.Brief.Fixes = []FixRef{{Finding: fixes}}
			}
			for i := range assigned.OwnerApprovals {
				assigned.OwnerApprovals[i].At = assigned.AssignedAt
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
			if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "stored worker assignment tx goes past a limit of its profile without the owner's approval, or carries one it does not need") {
				t.Fatalf("Load = %v, want the stored budget refusal", err)
			}
		})
	}
}
