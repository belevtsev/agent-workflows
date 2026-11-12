package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// unsupportedTaskVersionMessage is the exact D5 refusal for a stored task
// record whose version is not 3.
func unsupportedTaskVersionMessage(version int) string {
	return fmt.Sprintf("task record version %d is not supported; this cw reads version 3 records; start a new task with 'cw task start'", version)
}

// unsupportedVersionVariant selects which revisions of a real, hash-chained
// task history are rewritten with another version.
type unsupportedVersionVariant struct {
	name    string
	version int
	// extend adds context, check and assessment revisions after Start.
	extend bool
	// rewrite reports whether a revision (of last) carries the other version.
	rewrite func(revision, last int) bool
}

func unsupportedVersionVariants() []unsupportedVersionVariant {
	every := func(int, int) bool { return true }
	first := func(revision, _ int) bool { return revision == 1 }
	middle := func(revision, _ int) bool { return revision == 2 }
	last := func(revision, last int) bool { return revision == last }
	var variants []unsupportedVersionVariant
	for _, version := range []int{1, 2} {
		variants = append(variants,
			unsupportedVersionVariant{fmt.Sprintf("v%d/single_revision", version), version, false, every},
			unsupportedVersionVariant{fmt.Sprintf("v%d/all_revisions", version), version, true, every},
			unsupportedVersionVariant{fmt.Sprintf("v%d/first_revision", version), version, true, first},
			unsupportedVersionVariant{fmt.Sprintf("v%d/middle_revision", version), version, true, middle},
			unsupportedVersionVariant{fmt.Sprintf("v%d/last_revision", version), version, true, last},
		)
	}
	return append(variants, unsupportedVersionVariant{"v4/last_revision", 4, true, last})
}

// unsupportedVersionTask builds a native chain whose plan also holds a "probe"
// check that would create marker if it ever executed, then rewrites the
// selected revisions with valid hash chaining. Revisions rewritten as version
// 1 or 2 take that version's shape: no "advisers" member, and version 1 also
// has no coordinator.
func unsupportedVersionTask(t *testing.T, variant unsupportedVersionVariant, marker string) (Record, SourceRef) {
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
	previous := ""
	for revision := 1; revision <= record.Revision; revision++ {
		path := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision))
		var historical Record
		if err := Decode(bytes.NewReader(productionTaskRead(t, path)), &historical); err != nil {
			t.Fatal(err)
		}
		rewrite := variant.rewrite(revision, record.Revision)
		if rewrite {
			rewritten++
			historical.Version = variant.version
			if variant.version == 1 {
				historical.Plan.CoordinatorID = ""
			}
		}
		historical.PreviousSHA256 = previous
		data, err := encode(historical)
		if err != nil {
			t.Fatal(err)
		}
		if rewrite && variant.version < 3 {
			fields := map[string]jsontext.Value{}
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			if _, ok := fields["advisers"]; !ok {
				t.Fatal("version-3 revision has no \"advisers\" member to remove")
			}
			delete(fields, "advisers")
			if data, err = json.Marshal(fields, json.Deterministic(true)); err != nil {
				t.Fatal(err)
			}
		}
		productionTaskWrite(t, path, data)
		previous = digest(data)
	}
	if rewritten == 0 {
		t.Fatalf("%s rewrote no revision", variant.name)
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("probe ran while building the fixture: %v", err)
	}
	return record, ref
}

// D5 version: loading a chain with any revision of version 1, 2 or 4 fails
// with the D5 message naming that version, for read and mutating operations
// alike. Nothing is partially accepted: no record file changes, no artifact or
// .lock remains, and no planned check child runs.
func TestUnsupportedTaskRecordVersionsAreRefusedWithoutWrites(t *testing.T) {
	for _, variant := range unsupportedVersionVariants() {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			record, ref := unsupportedVersionTask(t, variant, marker)
			requireRefusedWithoutWrites(t, record, ref, marker, unsupportedTaskVersionMessage(variant.version))
		})
	}
}

// missingStartMembersMessage is the exact refusal for a stored version 3
// record without the members cw task start writes; names lists the missing
// members in record order (packs, test_rules, baseline_manifest).
func missingStartMembersMessage(names string) string {
	return "task record has no " + names + "; a cw build before v3.0.0-rc.1 wrote it, and this cw does not read such records; start a new task with 'cw task start'"
}

// startMembersTask builds a native chain on the current start path whose plan
// also holds a "probe" check that would create marker if it ever executed,
// then rewrites the selected revisions without the named start members, with
// valid hash chaining.
func startMembersTask(t *testing.T, drop []string, rewrite func(revision, last int) bool, marker string) (Record, SourceRef) {
	t.Helper()
	plan := helperPlan(t, "substantial", "pass")
	probe := append(slices.Clone(plan.Checks[0].Argv[:3]), "mutate", marker)
	plan.Checks = append(plan.Checks, Check{ID: "probe", Argv: probe, TimeoutSeconds: 10})
	repo := t.TempDir()
	data := []byte("first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ref := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(data)}
	record, err := StartWithPacks(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, fixtureBinding(), SelectionExplicit, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "before"); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	if record, err = Load(record.Directory); err != nil {
		t.Fatal(err)
	}
	previous := ""
	for revision := 1; revision <= record.Revision; revision++ {
		path := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision))
		var historical Record
		if err := Decode(bytes.NewReader(productionTaskRead(t, path)), &historical); err != nil {
			t.Fatal(err)
		}
		if historical.Packs == nil || historical.TestRules == "" || historical.BaselineManifest == nil {
			t.Fatalf("revision %d on the current start path lacks a start member", revision)
		}
		if rewrite(revision, record.Revision) {
			for _, name := range drop {
				switch name {
				case "packs":
					historical.Packs = nil
				case "test_rules":
					historical.TestRules = ""
				case "baseline_manifest":
					historical.BaselineManifest = nil
				}
			}
		}
		historical.PreviousSHA256 = previous
		data, err := encode(historical)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range drop {
			if rewrite(revision, record.Revision) && bytes.Contains(data, []byte(`"`+name+`"`)) {
				t.Fatalf("revision %d still has a %q member", revision, name)
			}
		}
		productionTaskWrite(t, path, data)
		previous = digest(data)
	}
	return record, ref
}

// OD-5: a stored version 3 record without packs, test_rules or
// baseline_manifest, whichever revision lacks it, is refused with the message
// naming the missing members and telling the user to start a new task, for
// read and mutating operations alike, and nothing is written or run (oracle:
// the exact message, unchanged task files, no lock and no probe marker).
func TestRecordsWithoutStartMembersAreRefusedWithoutWrites(t *testing.T) {
	every := func(int, int) bool { return true }
	last := func(revision, last int) bool { return revision == last }
	for _, variant := range []struct {
		name    string
		drop    []string
		rewrite func(revision, last int) bool
		names   string
	}{
		{"no_packs", []string{"packs"}, every, "packs"},
		{"no_test_rules", []string{"test_rules"}, every, "test_rules"},
		{"no_baseline_manifest", []string{"baseline_manifest"}, every, "baseline_manifest"},
		{"no_packs_or_test_rules", []string{"packs", "test_rules"}, every, "packs or test_rules"},
		{"none_of_the_three", []string{"packs", "test_rules", "baseline_manifest"}, every, "packs, test_rules or baseline_manifest"},
		{"last_revision_without_test_rules", []string{"test_rules"}, last, "test_rules"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			record, ref := startMembersTask(t, variant.drop, variant.rewrite, marker)
			requireRefusedWithoutWrites(t, record, ref, marker, missingStartMembersMessage(variant.names))
		})
	}
}

// A record on the current start path carries all three start members, loads
// and verifies (oracle: Load succeeds with the members, and Verify judges that
// task: it returns its verification, which a refused record never gets).
func TestRecordsWithStartMembersLoad(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	mustAssess(t, record, ref)
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("current-format record does not load: %v", err)
	}
	if loaded.Packs == nil || loaded.TestRules != TestRulesV1 || loaded.BaselineManifest == nil {
		t.Fatalf("current-format record lacks a start member: packs %v, test_rules %q, baseline_manifest %v", loaded.Packs, loaded.TestRules, loaded.BaselineManifest)
	}
	if verification, err := Verify(t.Context(), record.Directory); verification.TaskID != record.ID {
		t.Fatalf("current-format record was not verified: %v", err)
	}
}

// requireRefusedWithoutWrites runs every read and mutating operation on the
// task at record and requires each to fail with message, without changing
// task files, leaving a lock, or running the probe check that creates marker.
func requireRefusedWithoutWrites(t *testing.T, record Record, ref SourceRef, marker, message string) {
	t.Helper()
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
				t.Error("refused chain verified complete")
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
		if err == nil || !strings.Contains(err.Error(), message) {
			t.Errorf("%s: chain not refused with %q: %v", operation.name, message, err)
		}
		if errors.Is(err, errTaskLocked) || errors.Is(err, errTaskRevisionLimit) {
			t.Errorf("%s: refusal reported as lock or capacity: %v", operation.name, err)
		}
		if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
			t.Errorf("%s: refused chain changed task files", operation.name)
		}
		if _, err := os.Lstat(filepath.Join(record.Directory, ".lock")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: refusal left a task lock: %v", operation.name, err)
		}
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused chain executed a planned check: %v", err)
	}
}
