package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tests that FindOpen keeps a record it cannot load, because cw v2.0.66 to
// v2.0.96 wrote it without packs and test_rules, gating as v3.0.0-rc.1 did.

// v2096Members are the members of a stored revision as cw v2.0.96 writes it
// (taskflow.Record at tag v2.0.96). A v3.0.0-rc.1 cw adds abandonment when it
// abandons such a record, and source_sha256 to a verification it writes.
var v2096Members = []string{"version", "id", "directory", "revision", "evidence_revision", "previous_sha256", "created_at", "updated_at", "plan", "baseline", "baseline_manifest", "advisers", "contexts", "checks", "assessments", "decision", "consultations", "verification", "assignments", "worker_results", "worker_acceptances"}

// toV2096Shape rewrites every revision of record's chain, with valid hash
// chaining, into the shape cw v2.0.96 writes: no packs or test_rules, and no
// verification source_sha256 unless keepDigest (a v3.0.0-rc.1 cw verified
// it). It requires each revision to hold only v2096Members and abandonment,
// and Load to refuse the chain as a record to replace with a new task.
func toV2096Shape(t *testing.T, record Record, keepDigest bool) {
	t.Helper()
	previous := ""
	for revision := 1; revision <= record.Revision; revision++ {
		path := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", revision))
		var historical Record
		if err := Decode(bytes.NewReader(productionTaskRead(t, path)), &historical); err != nil {
			t.Fatal(err)
		}
		historical.Packs, historical.TestRules = nil, ""
		if historical.Verification != nil && !keepDigest {
			historical.Verification.SourceSHA256 = ""
		}
		historical.PreviousSHA256 = previous
		data, err := encode(historical)
		if err != nil {
			t.Fatal(err)
		}
		members := map[string]jsontext.Value{}
		if err := json.Unmarshal(data, &members); err != nil {
			t.Fatal(err)
		}
		for name := range members {
			if !slices.Contains(v2096Members, name) && name != "abandonment" {
				t.Fatalf("revision %d has member %q, which cw v2.0.96 never writes", revision, name)
			}
		}
		productionTaskWrite(t, path, data)
		previous = digest(data)
	}
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "start a new task") {
		t.Fatalf("Load of the v2.0.96-shaped record: %v, want the refusal", err)
	}
}

// legacyGateProblem is the exact problem FindOpen gives a legacy record that
// gates: it names the directory and the way out.
func legacyGateProblem(directory string) string {
	return "this cw cannot read its record, which cw v2.0.66 to v2.0.96 or a v3 build before v3.0.0-rc.1 wrote; move " + directory + " out of the task store, then start a new task with `cw task start`"
}

// cw v2.0.66 to v2.0.96 wrote version 3 records without packs and test_rules,
// and the rc.1 upgrade notes tell v2 users to keep their task store. Load
// refuses such a record, but FindOpen must keep it gating as v3.0.0-rc.1
// did, and must not let a finished one gate.
func TestFindOpenGatesOnLegacyRecords(t *testing.T) {
	t.Run("an open legacy record gates and names the way out", func(t *testing.T) {
		// Oracle: the record is returned gating, with its ID, directory and
		// title, and the exact move-it-out problem.
		store, repo := foFixture(t)
		now := time.Now()
		open := foStartAt(t, store, repo, "v2 open work", "substantial", now.Add(-time.Hour))
		toV2096Shape(t, open, false)
		got := foFind(t, store, foAtRoot(open.Baseline.Root), now, (&foStub{digest: foDigestA}).identify())
		foExpect(t, got, foGating(open))
		if got[0].Problem != legacyGateProblem(open.Directory) {
			t.Fatalf("legacy record returned with problem %q, want %q", got[0].Problem, legacyGateProblem(open.Directory))
		}
	})

	t.Run("an open legacy record with a garbage newest revision still gates", func(t *testing.T) {
		// Oracle: Load refuses the record at revision 1 as legacy, so it
		// gates from that revision with the move-it-out problem, and does
		// not turn into an unreadable record; before, the garbage revision
		// hid it.
		store, repo := foFixture(t)
		now := time.Now()
		open := foStartAt(t, store, repo, "v2 open work", "substantial", now.Add(-time.Hour))
		toV2096Shape(t, open, false)
		foWrite(t, open.Directory, "record-000002.json", "{\n")
		got := foFind(t, store, foAtRoot(open.Baseline.Root), now, (&foStub{digest: foDigestA}).identify())
		foExpect(t, got, foGating(open))
		if !got[0].Legacy || got[0].Problem != legacyGateProblem(open.Directory) {
			t.Fatalf("legacy record returned with Legacy %v, problem %q; want Legacy and %q", got[0].Legacy, got[0].Problem, legacyGateProblem(open.Directory))
		}
	})

	t.Run("finished legacy records do not gate", func(t *testing.T) {
		// Oracle: beside an open legacy record, which still gates, a record
		// verified by cw v2.0.96 (complete, no source digest) and one a
		// v3.0.0-rc.1 cw abandoned are not returned, and one a v3.0.0-rc.1
		// cw verified on the current source is returned without gating.
		store, repo := foFixture(t)
		now := time.Now()
		open := foStartAt(t, store, repo, "v2 open work", "substantial", now.Add(-4*time.Hour))
		toV2096Shape(t, open, false)
		v2Verified := foVerify(t, foStartAt(t, store, repo, "verified by v2.0.96", "substantial", now.Add(-3*time.Hour)), true, foDigestB)
		toV2096Shape(t, v2Verified, false)
		abandoned, err := Abandon(foStartAt(t, store, repo, "abandoned by rc.1", "substantial", now.Add(-2*time.Hour)).Directory, "finished in v2")
		if err != nil {
			t.Fatal(err)
		}
		toV2096Shape(t, abandoned, false)
		current := foVerify(t, foStartAt(t, store, repo, "verified by rc.1 on this source", "substantial", now.Add(-time.Hour)), true, foDigestA)
		toV2096Shape(t, current, true)
		got := foFind(t, store, foAtRoot(open.Baseline.Root), now, (&foStub{digest: foDigestA}).identify())
		foExpect(t, got, foCurrent(current), foGating(open))
		if got[1].Problem != legacyGateProblem(open.Directory) {
			t.Fatalf("open legacy record returned with problem %q, want %q", got[1].Problem, legacyGateProblem(open.Directory))
		}
	})
}
