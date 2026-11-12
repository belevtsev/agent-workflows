package taskflow

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// Tests that a record whose pack set binds go.package-test/v1, which only v3
// builds before v3.0.0-rc.1 recorded, is refused like a record without start
// members, and that FindOpen keeps an open one gating.

// retiredGoRuleMessage is the exact refusal for a record that binds
// go.package-test/v1.
const retiredGoRuleMessage = "task record binds evidence rule go.package-test/v1, which only v3 builds before v3.0.0-rc.1 recorded and this cw no longer applies; this cw cannot read it: move the task directory out of the task store, then start a new task with 'cw task start'"

// bindRetiredGoRule rewrites the revisions of record's chain that rewrite
// selects, with valid hash chaining, so that their pack set applies the go
// pack with go.package-test/v1, as the v3 builds of 2026-11-10 recorded it.
// record must be the latest revision.
func bindRetiredGoRule(t *testing.T, record Record, rewrite func(revision int) bool) {
	t.Helper()
	previous := ""
	for revision := 1; revision <= record.Revision; revision++ {
		path := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision))
		var historical Record
		if err := Decode(bytes.NewReader(productionTaskRead(t, path)), &historical); err != nil {
			t.Fatal(err)
		}
		if rewrite(revision) {
			historical.Packs = &PackSet{Selection: SelectionDefault, Applied: []PackBinding{{ID: "go", Detected: "go.mod", EvidenceRule: "go.package-test/v1"}}}
		}
		historical.PreviousSHA256 = previous
		data, err := encode(historical)
		if err != nil {
			t.Fatal(err)
		}
		productionTaskWrite(t, path, data)
		previous = digest(data)
	}
}

// A record whose revision 1 binds go.package-test/v1 is refused at load with
// ErrLegacyRecord and the move-it-out message, for read and mutating
// operations alike, and nothing is written or run (oracle: the exact message,
// an error matching ErrLegacyRecord, unchanged task files, no lock and no
// probe marker). Revision 1 decides: with only revision 1 rewritten, the
// refusal is the same, not the immutable-baseline error a later revision
// would give. Before this cw, such a record loaded and verified under v1.
func TestRecordsBindingTheRetiredGoRuleAreRefusedWithoutWrites(t *testing.T) {
	for _, variant := range []struct {
		name    string
		rewrite func(revision int) bool
	}{
		{"every_revision", func(int) bool { return true }},
		{"first_revision", func(revision int) bool { return revision == 1 }},
	} {
		t.Run(variant.name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "probe-must-not-run")
			plan := helperPlan(t, "substantial", "pass")
			probe := append(slices.Clone(plan.Checks[0].Argv[:3]), "mutate", marker)
			plan.Checks = append(plan.Checks, Check{ID: "probe", Argv: probe, TimeoutSeconds: 10})
			record, ref := fixtureTask(t, plan)
			if _, err := PrepareContext(t.Context(), record.Directory, "before"); err != nil {
				t.Fatal(err)
			}
			mustAssess(t, record, ref)
			record, err := Load(record.Directory)
			if err != nil {
				t.Fatal(err)
			}
			if record.Revision < 3 {
				t.Fatalf("history too short: revision %d", record.Revision)
			}
			bindRetiredGoRule(t, record, variant.rewrite)
			if _, err := Load(record.Directory); !errors.Is(err, ErrLegacyRecord) {
				t.Errorf("Load error %v does not match ErrLegacyRecord", err)
			}
			requireRefusedWithoutWrites(t, record, ref, marker, retiredGoRuleMessage)
		})
	}
}

// An open record that binds go.package-test/v1 still gates through FindOpen,
// with the move-it-out problem, since verify and abandon cannot load it; one
// verified on the current source is finished and does not gate (oracle: the
// returned records in order, their gating state, Legacy and the exact
// problem). Before this cw, the open one gated as an ordinary record: with
// "it has no passing cw task verify" and Legacy unset.
func TestFindOpenGatesOnRecordsBindingTheRetiredGoRule(t *testing.T) {
	store, repo := foFixture(t)
	now := time.Now()
	open := foStartAt(t, store, repo, "v1 open work", "substantial", now.Add(-2*time.Hour))
	bindRetiredGoRule(t, open, func(int) bool { return true })
	verified := foVerify(t, foStartAt(t, store, repo, "v1 verified on this source", "substantial", now.Add(-time.Hour)), true, foDigestA)
	bindRetiredGoRule(t, verified, func(int) bool { return true })
	got := foFind(t, store, foAtRoot(open.Baseline.Root), now, (&foStub{digest: foDigestA}).identify())
	foExpect(t, got, foCurrent(verified), foGating(open))
	if !got[1].Legacy || got[1].Problem != legacyGateProblem(open.Directory) {
		t.Fatalf("open v1 record returned with Legacy %v, problem %q; want Legacy and %q", got[1].Legacy, got[1].Problem, legacyGateProblem(open.Directory))
	}
	if !got[0].Legacy {
		t.Fatalf("verified v1 record returned without Legacy: %+v", got[0])
	}
}
