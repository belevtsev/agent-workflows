package taskflow

import (
	"errors"
	"maps"
	"reflect"
	"slices"
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

// fixBrief is a fix round of the implementation stage that fixes refs.
func fixBrief(id string, refs ...FixRef) WorkerBrief {
	b := stageBrief(id, "implementation", StageImplementation, "source.txt")
	b.Fixes = refs
	return b
}

// returnFinding records a cancelled result of assignment id with finding f1,
// which reopens the finding key reopens when it is set.
func returnFinding(t *testing.T, record Record, ref SourceRef, id, reopens string) Record {
	t.Helper()
	result := workerResult(t, record, id, "cancelled")
	result.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}, Reopens: reopens}}
	return mustSubmitWorker(t, record, result)
}

// fixRoundsRecord builds a record holding exactly briefs; reopens maps an
// assignment to the reopens of its result's finding f1 (a result only for
// the assignments it names), and unused names the assignments whose latest
// acceptance is unused.
func fixRoundsRecord(briefs []WorkerBrief, reopens map[string]string, unused ...string) Record {
	var record Record
	for _, brief := range briefs {
		record.Assignments = append(record.Assignments, WorkerAssignment{Brief: brief})
		if key, ok := reopens[brief.ID]; ok {
			record.WorkerResults = append(record.WorkerResults, BoundWorkerResult{Result: WorkerResult{AssignmentID: brief.ID, Findings: []WorkerFinding{{ID: "f1", Reopens: key}}}})
		}
		if slices.Contains(unused, brief.ID) {
			record.WorkerAcceptances = append(record.WorkerAcceptances, BoundWorkerAcceptance{Acceptance: WorkerAcceptance{AssignmentID: brief.ID, Status: "unused"}})
		}
	}
	return record
}

// Oracle: plan R4. fixRounds counts, per finding or requirement, the
// assignments that fix it; a finding counts under the first finding of its
// reopens chain; a repeat and an assignment closed as unused are no rounds,
// as in roundsUsed; a brief that fixes two findings of one chain counts
// once. Each case's count changes when its rule is removed.
func TestFixRoundsCountsFixAssignmentsPerFindingOrRequirement(t *testing.T) {
	f1, req := FixRef{Finding: "a0/f1"}, FixRef{Requirement: "behavior"}
	repeat := fixBrief("a2", f1)
	repeat.Repeats = "a1"
	for _, c := range []struct {
		name    string
		briefs  []WorkerBrief
		reopens map[string]string
		unused  []string
		want    map[FixRef]int
	}{
		{"each fix assignment counts", []WorkerBrief{fixBrief("a1", f1, req), fixBrief("a2", f1)}, nil, nil, map[FixRef]int{f1: 2, req: 1}},
		{"a reopened finding counts as the first", []WorkerBrief{fixBrief("a0"), fixBrief("a1", f1), fixBrief("a2", FixRef{Finding: "a1/f1"}), fixBrief("a3", FixRef{Finding: "a2/f1"})}, map[string]string{"a0": "", "a1": "a0/f1", "a2": "a1/f1"}, nil, map[FixRef]int{f1: 3}},
		{"a finding without reopens is its own key", []WorkerBrief{fixBrief("a0"), fixBrief("a1", f1), fixBrief("a2", FixRef{Finding: "a1/f1"})}, map[string]string{"a0": "", "a1": ""}, nil, map[FixRef]int{f1: 1, {Finding: "a1/f1"}: 1}},
		{"one brief counts once per chain", []WorkerBrief{fixBrief("a0"), fixBrief("a1", f1), fixBrief("a2", f1, FixRef{Finding: "a1/f1"})}, map[string]string{"a0": "", "a1": "a0/f1"}, nil, map[FixRef]int{f1: 2}},
		{"a repeat is no round", []WorkerBrief{fixBrief("a1", f1), repeat}, nil, nil, map[FixRef]int{f1: 1}},
		{"an unused assignment is no round", []WorkerBrief{fixBrief("a1", f1), fixBrief("a2", f1)}, nil, []string{"a1"}, map[FixRef]int{f1: 1}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := fixRounds(fixRoundsRecord(c.briefs, c.reopens, c.unused...)); !maps.Equal(got, c.want) {
				t.Fatalf("fixRounds = %v, want %v", got, c.want)
			}
		})
	}
}

// fixRoundTask starts a lite task whose assignment i0 returned finding f1,
// then records the fix rounds i1 and i2 on fix, each returning finding f1.
// With chain, each round instead fixes the previous round's f1, which its
// own f1 reopens.
func fixRoundTask(t *testing.T, fix FixRef, chain bool) (Record, SourceRef) {
	t.Helper()
	record, ref := stageTask(t, false)
	record = returnFinding(t, mustAssignWorker(t, record, fixBrief("i0")), ref, "i0", "")
	previous := "i0"
	for _, id := range []string{"i1", "i2"} {
		reopens, refs := "", fix
		if chain {
			reopens, refs = previous+"/f1", FixRef{Finding: previous + "/f1"}
		}
		record = returnFinding(t, mustAssignWorker(t, record, fixBrief(id, refs)), ref, id, reopens)
		previous = id
	}
	return record, ref
}

// wantEscalation checks that err refuses a third fix round on key with
// ErrEscalation, typed ErrOverBudget, after rounds i1 and i2, stating both
// of the owner's options.
func wantEscalation(t *testing.T, err error, key FixRef) {
	t.Helper()
	escalation, typed := errors.AsType[ErrEscalation](err)
	summary := "Inspected the assigned source and actual task evidence."
	want := ErrEscalation{Key: key, Profile: "lite", Rounds: 2, History: []FixRound{{"i1", summary}, {"i2", summary}}}
	if !errors.Is(err, ErrOverBudget) || errors.Is(err, ErrInvalidInput) || !typed || !reflect.DeepEqual(escalation, want) {
		t.Fatalf("third fix round = %#v, want ErrOverBudget with %+v", err, want)
	}
	for _, option := range []string{"approving one more fix round", "changing the approach"} {
		if !strings.Contains(err.Error(), option) {
			t.Fatalf("refusal %q lacks the option %q", err, option)
		}
	}
}

// Oracle: plan R4 acceptance. Under lite (2 fix rounds) a third fix brief
// naming one finding that is still open (no disposition yet) is refused
// with ErrEscalation, Rounds 2 and the two earlier rounds in History, and
// writes no revision. The owner's words let it through, stored with For
// "escalation"; a repeat of that round needs none; and the record loads.
func TestAssignRefusesAThirdFixRoundOnAnOpenFindingUntilTheOwnerApproves(t *testing.T) {
	f1 := FixRef{Finding: "i0/f1"}
	record, ref := fixRoundTask(t, f1, false)
	before := revisions(t, record)
	_, err := AssignWorker(t.Context(), record.Directory, fixBrief("i3", f1))
	wantEscalation(t, err, f1)
	if after := revisions(t, record); after != before {
		t.Fatalf("a refused fix round wrote %d revisions", after-before)
	}
	record, err = productionEnv.AssignApprovedWorker(t.Context(), record.Directory, fixBrief("i3", f1), "try once more")
	if err != nil {
		t.Fatal(err)
	}
	// A replacement of i3 repeats its round, so it is no fourth round and
	// needs no words.
	record = returnFinding(t, record, ref, "i3", "")
	repeat := fixBrief("i4", f1)
	repeat.Repeats = "i3"
	record = mustAssignWorker(t, record, repeat)
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := findAssignment(loaded, "i3"); got.OwnerApproval == nil || *got.OwnerApproval != (OwnerApproval{Text: "try once more", For: ApprovalEscalation, At: got.AssignedAt}) {
		t.Fatalf("stored approval %+v, want the owner's words for escalation", got.OwnerApproval)
	}
}

// Oracle: plan R4. Three findings linked by reopens are one key: the brief
// fixing the third (i2/f1) is refused as the first (i0/f1) after rounds i1
// and i2, although no brief named i2/f1 before.
func TestAssignRefusesAFixRoundOnTheThirdFindingOfAReopensChain(t *testing.T) {
	record, _ := fixRoundTask(t, FixRef{}, true)
	_, err := AssignWorker(t.Context(), record.Directory, fixBrief("i3", FixRef{Finding: "i2/f1"}))
	wantEscalation(t, err, FixRef{Finding: "i0/f1"})
}

// Oracle: plan R4 "still open". A finding whose latest disposition is
// accepted (acknowledged but unresolved) still escalates; one resolved,
// rejected or deferred does not, and its third fix round is assigned
// without words.
func TestAssignEscalatesOnlyAFindingStillOpen(t *testing.T) {
	f1 := FixRef{Finding: "i0/f1"}
	for _, c := range []struct {
		status string
		open   bool
	}{{"accepted", true}, {"resolved", false}, {"rejected", false}, {"deferred", false}} {
		t.Run(c.status, func(t *testing.T) {
			record, ref := fixRoundTask(t, f1, false)
			acceptance := workerAcceptance(t, record, "i0", "unused")
			acceptance.Findings = []FindingDisposition{{FindingID: "f1", Status: c.status, Reason: "The coordinator checked the finding against the source.", Evidence: []SourceRef{ref}}}
			record = mustAcceptWorker(t, record, acceptance)
			_, err := AssignWorker(t.Context(), record.Directory, fixBrief("i3", f1))
			if c.open {
				wantEscalation(t, err, f1)
			} else if err != nil {
				t.Fatalf("third fix round on a %s finding = %v, want assigned", c.status, err)
			}
		})
	}
}

// Oracle: plan R4 "still open" for a requirement: open while its check
// lacks a passed run at the current source, so the third fix round is
// refused; after the check passes it is assigned without words.
func TestAssignEscalatesARequirementUntilItsChecksPass(t *testing.T) {
	behavior := FixRef{Requirement: "behavior"}
	record, _ := fixRoundTask(t, behavior, false)
	_, err := AssignWorker(t.Context(), record.Directory, fixBrief("i3", behavior))
	wantEscalation(t, err, behavior)
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	if _, err := AssignWorker(t.Context(), record.Directory, fixBrief("i3", behavior)); err != nil {
		t.Fatalf("third fix round on a requirement whose check passed = %v, want assigned", err)
	}
}

// Oracle: plan R4. reopens names a finding of an earlier result: an unknown
// finding, or the finding itself, is refused as invalid input naming it,
// and no revision is written.
func TestHandoffRefusesAReopensThatNamesNoEarlierFinding(t *testing.T) {
	for _, c := range []struct{ reopens, want string }{
		{"i9/f1", "worker finding f1 reopens i9/f1, which no earlier worker result holds"},
		{"i0/f2", "worker finding f1 reopens i0/f2, which no earlier worker result holds"},
		{"i1/f1", "worker finding f1 reopens itself"},
	} {
		t.Run(c.reopens, func(t *testing.T) {
			record, ref := stageTask(t, false)
			record = returnFinding(t, mustAssignWorker(t, record, fixBrief("i0")), ref, "i0", "")
			record = mustAssignWorker(t, record, fixBrief("i1", FixRef{Finding: "i0/f1"}))
			result := workerResult(t, record, "i1", "cancelled")
			result.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}, Reopens: c.reopens}}
			before := revisions(t, record)
			if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("handoff = %v, want invalid input %q", err, c.want)
			}
			if after := revisions(t, record); after != before {
				t.Fatalf("a refused handoff wrote %d revisions", after-before)
			}
		})
	}
}

// Oracle: the reopens rule holds for a stored revision too: a hand-written
// result whose finding reopens itself fails to load.
func TestStoredReopensMustNameAnEarlierFinding(t *testing.T) {
	record, ref := stageTask(t, false)
	record = mustAssignWorker(t, record, fixBrief("i0"))
	input := workerResult(t, record, "i0", "cancelled")
	input.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}, Reopens: "i0/f1"}}
	appendWorkerRevision(t, record, func(next *Record) {
		bound := BoundWorkerResult{Result: input, CheckRuns: []string{}, ScopeSHA256: strings.TrimPrefix(input.Snapshot.Fingerprint, "sha256:"), SubmittedAt: time.Now().UTC()}
		bound.SHA256, _ = workerResultHash(bound)
		next.WorkerResults = append(next.WorkerResults, bound)
		next.EvidenceRevision++
		next.UpdatedAt = bound.SubmittedAt
		next.Verification = nil
	})
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "worker finding f1 reopens itself") {
		t.Fatalf("Load = %v, want the reopens refusal", err)
	}
}

// Oracle: the escalation holds for a stored revision: a hand-written third
// fix round on an open finding without the owner's words fails to load
// with the escalation message, judged against the previous revision.
func TestStoredThirdFixRoundNeedsTheOwnersWords(t *testing.T) {
	record, _ := fixRoundTask(t, FixRef{Finding: "i0/f1"}, false)
	assigned := WorkerAssignment{TaskID: record.ID, Brief: fixBrief("i3", FixRef{Finding: "i0/f1"}), Snapshot: currentWorkerSnapshot(t, record), AssignedAt: time.Now().UTC()}
	var err error
	if assigned.SHA256, err = assignmentHash(assigned); err != nil {
		t.Fatal(err)
	}
	appendWorkerRevision(t, record, func(r *Record) {
		r.Assignments = append(r.Assignments, assigned)
		r.EvidenceRevision++
		r.Verification = nil
	})
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "stored worker assignment i3 fixes a finding or requirement past its fix rounds without the owner's approval") {
		t.Fatalf("Load = %v, want the stored escalation refusal", err)
	}
}

// Oracle: R3's rule that words go only with a limit holds for escalation
// too: words on a second fix round, which escalates nothing, are refused
// as invalid input.
func TestOwnerWordsOnAFixRoundWithinItsLimitAreRefused(t *testing.T) {
	record, ref := stageTask(t, false)
	record = returnFinding(t, mustAssignWorker(t, record, fixBrief("i0")), ref, "i0", "")
	record = returnFinding(t, mustAssignWorker(t, record, fixBrief("i1", FixRef{Finding: "i0/f1"})), ref, "i1", "")
	if _, err := productionEnv.AssignApprovedWorker(t.Context(), record.Directory, fixBrief("i2", FixRef{Finding: "i0/f1"}), "yes"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("words on a second fix round = %v, want ErrInvalidInput", err)
	}
}
