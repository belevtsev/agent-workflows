package taskflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// stageTask starts a small task under the default profile, or, with
// legacy, rewrites its only revision without the profile, as a record
// started before profiles existed.
func stageTask(t *testing.T, legacy bool) (Record, SourceRef) {
	t.Helper()
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if record.Profile == nil || record.Profile.Name != "lite" {
		t.Fatalf("precondition: task records lite, got %+v", record.Profile)
	}
	if legacy {
		record = rewriteInitialRevision(t, record, func(r *Record) { r.Profile, r.Plan.Profile = nil, "" })
	}
	return record, ref
}

// stageBrief is a fixture brief of role with stage and owned path.
func stageBrief(id, role, stage, owned string) WorkerBrief {
	b := workerBrief(id, "/root/"+id, role, []string{owned})
	b.Stage = stage
	return b
}

// returnRound records a cancelled result of assignment id, with one
// finding f1, so a later round of its stage is a fix or a repeat.
func returnRound(t *testing.T, record Record, ref SourceRef, id string) Record {
	t.Helper()
	result := workerResult(t, record, id, "cancelled")
	result.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}}}
	return mustSubmitWorker(t, record, result)
}

// revisions counts the task's stored revisions.
func revisions(t *testing.T, record Record) int {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(record.Directory, "record-*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return len(names)
}

// Oracle: worker-handoffs.md "Assign before dispatch". Under a profile an
// implementation or review brief names a stage of the profile, and an
// author-stage brief after an earlier brief of its stage returned a result
// names the findings or requirements it fixes or the assignment it repeats.
// Each refusal is invalid input, names the missing or wrong member, and
// writes no revision.
func TestAssignRefusesABriefWithoutItsStageOrRound(t *testing.T) {
	for _, c := range []struct {
		name    string
		setup   func(t *testing.T, record Record, ref SourceRef) Record
		brief   func() WorkerBrief
		refused string
	}{
		{"second tests round without fixes or repeats", func(t *testing.T, record Record, ref SourceRef) Record {
			record = mustAssignWorker(t, record, stageBrief("t1", "implementation", StageTests, "source.txt"))
			return returnRound(t, record, ref, "t1")
		}, func() WorkerBrief { return stageBrief("t2", "implementation", StageTests, "source.txt") },
			"worker brief of stage tests needs fixes or repeats: assignment t1 of that stage has returned a result"},
		{"spec under lite", nil, func() WorkerBrief { return stageBrief("s1", "implementation", StageSpec, "source.txt") },
			"worker brief stage spec is not in profile lite: use one of tests, implementation, test-review, code-review"},
		{"implementation without a stage", nil, func() WorkerBrief { return stageBrief("i1", "implementation", "", "source.txt") },
			"worker brief of role implementation needs a stage under profile lite"},
		{"review without a stage", nil, func() WorkerBrief { return stageBrief("r1", "review", "", ".") },
			"worker brief of role review needs a stage under profile lite"},
		{"unknown stage", nil, func() WorkerBrief { return stageBrief("u1", "implementation", "deploy", "source.txt") },
			`worker brief stage "deploy" is not one of spec, spec-review`},
		{"repeats an unknown assignment", nil, func() WorkerBrief {
			b := stageBrief("t1", "implementation", StageTests, "source.txt")
			b.Repeats = "ghost"
			return b
		}, "worker brief repeats ghost, which is not an earlier assignment of the task"},
		{"repeats another stage", func(t *testing.T, record Record, _ SourceRef) Record {
			return mustAssignWorker(t, record, stageBrief("i1", "implementation", StageImplementation, "source.txt"))
		}, func() WorkerBrief {
			b := stageBrief("t1", "implementation", StageTests, "other.txt")
			b.Repeats = "i1"
			return b
		}, `worker brief repeats i1 of stage "implementation", but its stage is "tests"`},
		{"fixes an unknown finding", func(t *testing.T, record Record, ref SourceRef) Record {
			record = mustAssignWorker(t, record, stageBrief("t1", "implementation", StageTests, "source.txt"))
			return returnRound(t, record, ref, "t1")
		}, func() WorkerBrief {
			b := stageBrief("t2", "implementation", StageTests, "source.txt")
			b.Fixes = []FixRef{{Finding: "t1/f9"}}
			return b
		}, "worker brief fixes finding t1/f9, which no recorded worker result holds"},
		{"fixes entry naming both", nil, func() WorkerBrief {
			b := stageBrief("t1", "implementation", StageTests, "source.txt")
			b.Fixes = []FixRef{{Finding: "a/b", Requirement: "behavior"}}
			return b
		}, "each worker brief fixes entry names exactly one finding or requirement"},
		{"fixes an unknown requirement", nil, func() WorkerBrief {
			b := stageBrief("t1", "implementation", StageTests, "source.txt")
			b.Fixes = []FixRef{{Requirement: "missing"}}
			return b
		}, "worker brief fixes requirement missing, which the plan does not hold"},
	} {
		t.Run(c.name, func(t *testing.T) {
			record, ref := stageTask(t, false)
			if c.setup != nil {
				record = c.setup(t, record, ref)
			}
			before := revisions(t, record)
			_, err := AssignWorker(t.Context(), record.Directory, c.brief())
			if err == nil || !strings.HasPrefix(err.Error(), c.refused) || !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("assign = %v, want ErrInvalidInput starting %q", err, c.refused)
			}
			if after := revisions(t, record); after != before {
				t.Errorf("a refused assign wrote %d revisions", after-before)
			}
		})
	}
}

// Oracle: the same rules accept what they allow: an evidence brief without a
// stage; parallel first-round implementers on separate paths; a later round
// naming the finding it fixes, the requirement it fixes or the assignment it
// repeats. The stored brief keeps stage, fixes and repeats exactly, and the
// record loads.
func TestAssignAcceptsStagedRounds(t *testing.T) {
	record, ref := stageTask(t, false)
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "other.txt"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, stageBrief("look", "evidence", "", "source.txt"))
	record = mustAssignWorker(t, record, stageBrief("i1", "implementation", StageImplementation, "source.txt"))
	record = mustAssignWorker(t, record, stageBrief("i2", "implementation", StageImplementation, "other.txt"))
	record = returnRound(t, record, ref, "i1")
	record = returnRound(t, record, ref, "i2")
	fix := stageBrief("i3", "implementation", StageImplementation, "source.txt")
	fix.Fixes = []FixRef{{Finding: "i1/f1"}, {Requirement: "behavior"}}
	record = mustAssignWorker(t, record, fix)
	again := stageBrief("i4", "implementation", StageImplementation, "other.txt")
	again.Repeats = "i2"
	record = mustAssignWorker(t, record, again)

	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []WorkerBrief{fix, again} {
		got := findAssignment(loaded, want.ID)
		if got == nil || !reflect.DeepEqual(got.Brief, want) {
			t.Errorf("stored brief %s = %+v, want %+v", want.ID, got, want)
		}
	}
}

// Oracle: a record without a profile accepts briefs as before profiles
// existed: a review without a stage, and a second round of a stage, after
// the first returned, without fixes or repeats; and it still loads.
func TestRecordWithoutAProfileAcceptsBriefsAsBefore(t *testing.T) {
	record, ref := stageTask(t, true)
	record = mustAssignWorker(t, record, stageBrief("w1", "implementation", StageImplementation, "source.txt"))
	record = returnRound(t, record, ref, "w1")
	record = mustAssignWorker(t, record, stageBrief("w2", "implementation", StageImplementation, "source.txt"))
	record = mustAssignWorker(t, record, stageBrief("rv", "review", "", "."))
	if loaded, err := Load(record.Directory); err != nil || loaded.Profile != nil || len(loaded.Assignments) != 3 {
		t.Fatalf("Load = %d assignments, profile %+v, %v; want 3, none, nil", len(loaded.Assignments), loaded.Profile, err)
	}
}

// Oracle: whether an earlier brief of a stage had returned a result when a
// later one was assigned is a fact of the revision chain, not of the clock.
// With a clock that steps back, a first-round brief assigned before its
// sibling's result exists still loads although that result's time is earlier
// than the brief's; and a later brief assigned without fixes or repeats once a
// result exists is still refused at load, however the times compare. The
// clock reads 99s for the result and 101s for the revision that records it,
// so the record's own updated_at rule, which predates stages, holds.
func TestBriefRoundRuleFollowsTheRecordNotTheClock(t *testing.T) {
	record, ref := stageTask(t, false)
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "other.txt"), []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var readings []time.Duration
	env := productionEnv
	env.Clock = func() time.Time {
		offset := readings[0]
		if len(readings) > 1 {
			readings = readings[1:]
		}
		return record.CreatedAt.Add(offset)
	}
	at := func(offsets ...time.Duration) { readings = offsets }

	at(50 * time.Second)
	if _, err := env.AssignWorker(t.Context(), record.Directory, stageBrief("b1", "implementation", StageTests, "source.txt")); err != nil {
		t.Fatal(err)
	}
	at(100 * time.Second)
	if _, err := env.AssignWorker(t.Context(), record.Directory, stageBrief("b2", "implementation", StageTests, "other.txt")); err != nil {
		t.Fatalf("a first-round sibling before any result: %v", err)
	}
	// The clock steps back: b1's result is stamped before b2's assignment.
	at(99*time.Second, 101*time.Second)
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	result := workerResult(t, record, "b1", "cancelled")
	result.Findings = []WorkerFinding{{ID: "f1", Summary: "The empty case is wrong.", Evidence: []SourceRef{ref}}}
	if _, err := env.SubmitWorkerResult(t.Context(), record.Directory, result); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err != nil {
		t.Fatalf("Load after a clock step back = %v, want the record", err)
	}

	// A third round after b1 returned, appended without fixes or repeats
	// and stamped before b1's result, is refused at load all the same.
	appendWorkerRevision(t, mustLoad(t, record.Directory), func(r *Record) {
		r.UpdatedAt = record.CreatedAt.Add(102 * time.Second)
		brief := stageBrief("b3", "implementation", StageTests, "source.txt")
		assignment := WorkerAssignment{TaskID: r.ID, Brief: brief, Snapshot: r.Assignments[0].Snapshot, AssignedAt: record.CreatedAt.Add(98 * time.Second)}
		var hashErr error
		if assignment.SHA256, hashErr = assignmentHash(assignment); hashErr != nil {
			t.Fatal(hashErr)
		}
		r.Assignments = append(r.Assignments, assignment)
		r.EvidenceRevision++
	})
	const refused = "worker brief of stage tests needs fixes or repeats: assignment b1 of that stage has returned a result"
	if _, err := Load(record.Directory); err == nil || !strings.HasPrefix(err.Error(), refused) {
		t.Fatalf("Load with an unnamed third round = %v, want %q", err, refused)
	}
}

func mustLoad(t *testing.T, directory string) Record {
	t.Helper()
	record, err := Load(directory)
	if err != nil {
		t.Fatal(err)
	}
	return record
}
