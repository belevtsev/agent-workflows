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
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// refusedVersionMessage is the exact refusal for a stored task record revision
// of a version this cw does not read: below 3 it says to start a new task,
// above 4 to upgrade cw (D11).
func refusedVersionMessage(version int) string {
	if version > 5 {
		return fmt.Sprintf("task record version %d was written by a newer cw; this cw reads versions 3 to 5; upgrade cw to read it", version)
	}
	return fmt.Sprintf("task record version %d is not supported; this cw reads versions 3 to 5; start a new task with 'cw task start'", version)
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
	return append(variants, unsupportedVersionVariant{"v6/last_revision", 6, true, last})
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

// D5 and D11 version: loading a chain with any revision of version 1 or 2, or
// a last revision of version 6, fails with the message naming that version,
// for read and mutating operations alike. Nothing is partially accepted: no
// record file changes, no artifact or .lock remains, and no planned check
// child runs.
func TestUnsupportedTaskRecordVersionsAreRefusedWithoutWrites(t *testing.T) {
	for _, variant := range unsupportedVersionVariants() {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			record, ref := unsupportedVersionTask(t, variant, marker)
			requireRefusedWithoutWrites(t, record, ref, marker, refusedVersionMessage(variant.version))
		})
	}
}

// missingStartMembersMessage is the exact refusal for a stored version 3
// record without the members cw task start writes; names lists the missing
// members in record order (packs, test_rules, baseline_manifest).
func missingStartMembersMessage(names string) string {
	return "task record has no " + names + ", so cw v2.0.66 to v2.0.96 or a v3 build before v3.0.0-rc.1 wrote it; this cw cannot read it: move the task directory out of the task store, then start a new task with 'cw task start'"
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

// OD-5: a stored version 3 record whose revision 1 lacks packs, test_rules or
// baseline_manifest is refused with the message naming the missing members
// and telling the user to move the task and start a new one, for read and
// mutating operations alike, and nothing is written or run (oracle: the
// exact message, an error matching ErrLegacyRecord, unchanged task files, no
// lock and no probe marker). A later revision that drops a member is caught
// by the immutability check instead (l4_rules_test.go).
func TestRecordsWithoutStartMembersAreRefusedWithoutWrites(t *testing.T) {
	every := func(int, int) bool { return true }
	first := func(revision, _ int) bool { return revision == 1 }
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
		{"first_revision_without_test_rules", []string{"test_rules"}, first, "test_rules"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			record, ref := startMembersTask(t, variant.drop, variant.rewrite, marker)
			if _, err := Load(record.Directory); !errors.Is(err, ErrLegacyRecord) {
				t.Errorf("Load error %v does not match ErrLegacyRecord", err)
			}
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
			run, err := RunCheck(t.Context(), record.Directory, "probe")
			if run.ID != "" || run.Log != "" {
				t.Errorf("refused run returned a check record: id=%q log=%q", run.ID, run.Log)
			}
			return err
		}},
		{"run_focused", func() error { _, err := RunCheck(t.Context(), record.Directory, "focused"); return err }},
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

// rc1Fixture is a task that cw v3.0.0-rc.1 (commit 13b6103) started, ran its
// one check for, assessed and verified complete: kind small, no advisers, in a
// folder outside Git that holds only source.txt (rc1FixtureSource, mode 0644).
// Its five revisions are version 3 and carry packs, test_rules and
// baseline_manifest. The values that depend on where rc.1 wrote it are
// placeholders, and placeRC1Fixture fills them in for a new location:
// @TASK_DIRECTORY@, @REPOSITORY@, @FINGERPRINT@ (the source fingerprint, which
// hashes the repository path) and @PREVIOUS_SHA256@ (the hash chain over the
// filled-in revisions). Every other byte is as rc.1 wrote it.
const (
	rc1Fixture       = "testdata/rc1-small-task"
	rc1FixtureID     = "9182674324e3d330d62d1a3858c8462d"
	rc1FixtureSource = "first line\nsecond line\n"
)

// placeRC1Fixture writes the rc.1 task into a new private store, over a new
// repository holding the same source, and returns the task directory.
func placeRC1Fixture(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte(rc1FixtureSource), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot, err := projectctx.Capture(t.Context(), repo)
	if err != nil || snapshot.SelectedPath != snapshot.Root || snapshot.Fingerprint == "" {
		t.Fatalf("capture the fixture repository: %+v %v", snapshot, err)
	}
	store, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(store, rc1FixtureID)
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	quoted := func(value string) []byte {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	entries, err := os.ReadDir(rc1Fixture)
	if err != nil {
		t.Fatal(err)
	}
	previous := ""
	revisions := 0
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(rc1Fixture, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if revisionName.MatchString(entry.Name()) {
			revisions++
			data = bytes.ReplaceAll(data, []byte(`"@TASK_DIRECTORY@"`), quoted(directory))
			data = bytes.ReplaceAll(data, []byte(`"@REPOSITORY@"`), quoted(snapshot.Root))
			data = bytes.ReplaceAll(data, []byte(`"@FINGERPRINT@"`), quoted(snapshot.Fingerprint))
			data = bytes.ReplaceAll(data, []byte(`"@PREVIOUS_SHA256@"`), quoted(previous))
			if bytes.Contains(data, []byte(`"@`)) {
				t.Fatalf("%s keeps a placeholder", entry.Name())
			}
			previous = digest(data)
		}
		productionTaskWrite(t, filepath.Join(directory, entry.Name()), data)
	}
	if revisions != 5 {
		t.Fatalf("fixture holds %d revisions, want 5", revisions)
	}
	return directory
}

// storedVersion is the version member of one revision file.
func storedVersion(t *testing.T, directory string, revision int) int {
	t.Helper()
	var stored struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(productionTaskRead(t, filepath.Join(directory, fmt.Sprintf("record-%06d.json", revision))), &stored); err != nil {
		t.Fatal(err)
	}
	return stored.Version
}

// D11, OD-5: a version 3 record that cw v3.0.0-rc.1 wrote, with packs,
// test_rules and baseline_manifest, loads and verifies unchanged, and the
// first revision this cw appends is version 5. Oracle: Load returns the rc.1
// record at version 3; Verify returns the verification rc.1 stored, apart from
// its time; the appended revision 6 is version 5, the record still loads, and
// the rc.1 revisions keep their bytes.
func TestRecordWrittenByRC1LoadsAndVerifiesUnchanged(t *testing.T) {
	directory := placeRC1Fixture(t)
	written := productionTaskBytes(t, directory)
	loaded, err := Load(directory)
	if err != nil {
		t.Fatalf("rc.1 record does not load: %v", err)
	}
	if loaded.Version != 3 || loaded.Revision != 5 || loaded.Packs == nil || loaded.TestRules != TestRulesV1 || loaded.BaselineManifest == nil {
		t.Fatalf("rc.1 record loaded as version %d revision %d, packs %v, test_rules %q, baseline_manifest %v", loaded.Version, loaded.Revision, loaded.Packs, loaded.TestRules, loaded.BaselineManifest)
	}
	if loaded.Verification == nil || !loaded.Verification.Complete {
		t.Fatalf("rc.1 record holds no complete verification: %+v", loaded.Verification)
	}
	stored := *loaded.Verification
	verified, err := Verify(t.Context(), directory)
	if err != nil {
		t.Fatalf("verify the rc.1 record: %v", err)
	}
	verified.CheckedAt, stored.CheckedAt = time.Time{}, time.Time{}
	// The record stores the findings' text only, not their codes.
	verified.ProblemFindings, verified.LimitationFindings = nil, nil
	if !reflect.DeepEqual(verified, stored) {
		t.Fatalf("Verify = %+v, want the verification rc.1 stored, %+v", verified, stored)
	}
	if version := storedVersion(t, directory, 6); version != 5 {
		t.Fatalf("the revision this cw appended is version %d, want 5", version)
	}
	if after, err := Load(directory); err != nil || after.Revision != 6 || after.Version != 5 {
		t.Fatalf("after verify: revision %d version %d: %v; want revision 6, version 5", after.Revision, after.Version, err)
	}
	now := productionTaskBytes(t, directory)
	for name, data := range written {
		if now[name] != data {
			t.Errorf("%s changed when this cw appended a revision", name)
		}
	}
}

// versionSequenceTask starts a small task and rewrites its chain so that
// revision i has versions[i-1], with valid hash chaining.
func versionSequenceTask(t *testing.T, versions ...int) string {
	t.Helper()
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = rewriteInitialRevision(t, record, func(first *Record) { first.Version = versions[0] })
	for _, version := range versions[1:] {
		appendWorkerRevision(t, record, func(next *Record) { next.Version = version })
		record.Revision++
	}
	for revision, version := range versions {
		if got := storedVersion(t, record.Directory, revision+1); got != version {
			t.Fatalf("revision %d stores version %d, want %d", revision+1, got, version)
		}
	}
	return record.Directory
}

// D11: this cw reads record versions 3 to 5, a revision never has a lower
// version than the one before it, and a version above 5 is refused as written
// by a newer cw. Oracle: the error identity Load returns for each sequence of
// revision versions (nil: it loads, at the last revision's version); version 5
// is the first record change after rc.2, which wrote 4 (go_env, F1).
func TestRecordVersionSequences(t *testing.T) {
	if Version != 5 || MinVersion != 3 {
		t.Errorf("Version %d, MinVersion %d; want 5 and 3", Version, MinVersion)
	}
	for _, c := range []struct {
		name     string
		versions []int
		want     error
	}{
		{"3", []int{3}, nil},
		{"3,3,4", []int{3, 3, 4}, nil},
		{"4,4", []int{4, 4}, nil},
		{"4,3", []int{4, 3}, errVersionDecreased},
		{"3,4,3", []int{3, 4, 3}, errVersionDecreased},
		{"4,5,5", []int{4, 5, 5}, nil},
		{"5,4", []int{5, 4}, errVersionDecreased},
		{"6", []int{6}, ErrNewerRecord},
		{"4,5,6", []int{4, 5, 6}, ErrNewerRecord},
	} {
		t.Run(c.name, func(t *testing.T) {
			directory := versionSequenceTask(t, c.versions...)
			loaded, err := Load(directory)
			switch {
			case c.want == nil && err != nil:
				t.Fatalf("Load: %v, want the record", err)
			case c.want == nil && loaded.Version != c.versions[len(c.versions)-1]:
				t.Fatalf("loaded version %d, want %d", loaded.Version, c.versions[len(c.versions)-1])
			case c.want != nil && !errors.Is(err, c.want):
				t.Fatalf("Load: %v, want an error matching %v", err, c.want)
			}
		})
	}
}
