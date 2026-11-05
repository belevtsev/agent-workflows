package taskflow

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// legacyRecordMessage is the exact R3.1 refusal for version-1 task records.
const legacyRecordMessage = "task record version 1 is no longer supported; start a new task with 'cw task start'"

// legacyRecordVariant selects which revisions of a real, hash-chained task
// history are rewritten as version 1.
type legacyRecordVariant struct {
	name string
	// extend adds context, check and assessment revisions after Start.
	extend bool
	// legacy reports whether a revision (of last) is rewritten as version 1.
	legacy func(revision, last int) bool
	// clearCoordinator reproduces the version-1 shape, which had no coordinator.
	clearCoordinator bool
}

func legacyRecordVariants() []legacyRecordVariant {
	return []legacyRecordVariant{
		{name: "single_revision", legacy: func(int, int) bool { return true }, clearCoordinator: true},
		{name: "all_revisions", extend: true, legacy: func(int, int) bool { return true }, clearCoordinator: true},
		{name: "first_revision", extend: true, legacy: func(revision, _ int) bool { return revision == 1 }},
		{name: "middle_revision", extend: true, legacy: func(revision, _ int) bool { return revision == 2 }},
		{name: "last_revision", extend: true, legacy: func(revision, last int) bool { return revision == last }},
	}
}

// legacyRecordTask builds a native chain whose plan also holds a "probe" check
// that would create marker if it ever executed, then rewrites the selected
// revisions as version 1 with valid hash chaining.
func legacyRecordTask(t *testing.T, variant legacyRecordVariant, marker string) (Record, SourceRef) {
	t.Helper()
	plan := helperPlan(t, "substantial", "pass")
	probe := append(slices.Clone(plan.Checks[0].Argv[:3]), "mutate", marker)
	plan.Checks = append(plan.Checks, Check{ID: "probe", Argv: probe, TimeoutSeconds: 10})
	record, ref := fixtureTask(t, plan)
	if variant.extend {
		if _, err := PrepareContext(t.Context(), record.Directory, "before"); err != nil {
			t.Fatal(err)
		}
		mustAssess(t, record, ref)
	}
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if variant.extend && record.Revision < 3 {
		t.Fatalf("history too short for %s: revision %d", variant.name, record.Revision)
	}
	rewritten := 0
	productionRewriteHistory(t, record, func(revision int, historical *Record) {
		if variant.legacy(revision, record.Revision) {
			rewritten++
			historical.Version = 1
			if variant.clearCoordinator {
				historical.Plan.CoordinatorID = ""
			}
		}
	})
	if rewritten == 0 {
		t.Fatalf("%s rewrote no revision", variant.name)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe ran while building the fixture: %v", err)
	}
	return record, ref
}

// R3.1: loading a chain with any version-1 revision fails with the version-1
// guidance for read and mutating operations alike. No record file changes, no
// artifact or .lock remains, and no planned check child runs.
func TestVersionOneTaskRecordsAreRefusedWithoutWrites(t *testing.T) {
	for _, variant := range legacyRecordVariants() {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			record, ref := legacyRecordTask(t, variant, marker)
			brief := workerBrief("worker", "/root/worker", "evidence", []string{"source.txt"}, "focused")
			result := WorkerResult{TaskID: record.ID, AssignmentID: "worker", AssignmentSHA256: strings.Repeat("a", 64), AgentID: "/root/worker", Snapshot: record.Baseline, Status: "completed", Summary: "Inspected the assigned source and actual task evidence.", Evidence: []SourceRef{ref}, ChangedFiles: []WorkerChangedFile{}, CheckIDs: []string{"focused"}, Findings: []WorkerFinding{}, Uncertainties: []string{}}
			acceptance := WorkerAcceptance{TaskID: record.ID, AssignmentID: "worker", ResultSHA256: strings.Repeat("b", 64), CoordinatorID: "/root", Status: "accepted", Reason: "Coordinator checked the declared scope and the retained source/check evidence.", Evidence: []SourceRef{ref}, CheckIDs: []string{"focused"}, Findings: []FindingDisposition{}}
			consultation := writeConsultation(t, record.Contexts["before"], nil)
			operations := []struct {
				name string
				call func() error
			}{
				{"load", func() error { _, err := Load(record.Directory); return err }},
				{"verify", func() error {
					verified, err := Verify(t.Context(), record.Directory)
					if verified.Complete {
						t.Error("version-1 chain verified complete")
					}
					return err
				}},
				{"context_before", func() error { _, err := PrepareContext(t.Context(), record.Directory, "before"); return err }},
				{"context_final", func() error { _, err := PrepareContext(t.Context(), record.Directory, "final"); return err }},
				{"assess", func() error { _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); return err }},
				{"run_probe", func() error {
					run, err := Run(t.Context(), record.Directory, "probe")
					if run.ID != "" || run.Log != "" {
						t.Errorf("refused run returned a check record: id=%q log=%q", run.ID, run.Log)
					}
					return err
				}},
				{"run_focused", func() error { _, err := Run(t.Context(), record.Directory, "focused"); return err }},
				{"consult", func() error {
					_, err := AttachConsultation(t.Context(), record.Directory, "before", consultation)
					return err
				}},
				{"assign", func() error { _, err := AssignWorker(t.Context(), record.Directory, brief); return err }},
				{"handoff", func() error { _, err := SubmitWorkerResult(t.Context(), record.Directory, result); return err }},
				{"accept", func() error { _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); return err }},
			}
			for _, operation := range operations {
				before := productionTaskBytes(t, record.Directory)
				err := operation.call()
				if err == nil || !strings.Contains(err.Error(), legacyRecordMessage) {
					t.Errorf("%s: version-1 chain not refused with version-1 guidance: %v", operation.name, err)
				}
				if errors.Is(err, errTaskLocked) || errors.Is(err, errTaskRevisionLimit) {
					t.Errorf("%s: refusal reported as lock or capacity: %v", operation.name, err)
				}
				if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
					t.Errorf("%s: refused version-1 chain changed task files", operation.name)
				}
				if _, err := os.Lstat(filepath.Join(record.Directory, ".lock")); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("%s: refusal left a task lock: %v", operation.name, err)
				}
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("refused version-1 chain executed a planned check: %v", err)
			}
		})
	}
}
