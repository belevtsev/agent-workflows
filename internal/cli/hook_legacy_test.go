package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Tests that a task record written by cw v2.0.66 to v2.0.96, which this cw
// refuses to load (taskflow.ErrLegacyRecord), keeps gating git commit and
// push as it did under v3.0.0-rc.1.

// hkToV2096Shape rewrites every revision file of the task at directory into
// the shape cw v2.0.96 writes: no packs, no test_rules and no verification
// source_sha256, chained again by SHA-256. It requires Load to refuse the
// result as a record to replace with a new task.
func hkToV2096Shape(t *testing.T, directory string) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(directory, "record-*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no revision files in %s: %v", directory, err)
	}
	slices.Sort(names)
	previous := ""
	for _, name := range names {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		members := map[string]jsontext.Value{}
		if err := json.Unmarshal(data, &members); err != nil {
			t.Fatal(err)
		}
		delete(members, "packs")
		delete(members, "test_rules")
		if raw, ok := members["verification"]; ok {
			verification := map[string]jsontext.Value{}
			if err := json.Unmarshal(raw, &verification); err != nil {
				t.Fatal(err)
			}
			delete(verification, "source_sha256")
			if members["verification"], err = json.Marshal(verification, json.Deterministic(true)); err != nil {
				t.Fatal(err)
			}
		}
		if members["previous_sha256"], err = json.Marshal(previous); err != nil {
			t.Fatal(err)
		}
		if data, err = json.Marshal(members, json.Deterministic(true)); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, data, 0o600); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(data)
		previous = hex.EncodeToString(sum[:])
	}
	if _, err := taskflow.Load(directory); err == nil || !strings.Contains(err.Error(), "start a new task") {
		t.Fatalf("Load of the v2.0.96-shaped record: %v, want the refusal", err)
	}
}

// The hook denies git commit while an open substantial record in the shape
// cw v2.0.96 writes governs the repository. The reason names the record and
// the way out (move its directory out of the store, then start a new task),
// and no verify or abandon command, since neither can load it. A record that
// cw v2.0.96 verified as complete does not gate (oracle: a deny with that
// reason, then an allow).
func TestHookPrePublishGatesOnLegacyRecords(t *testing.T) {
	h := newHKHarness(t)

	open := hkRepo(t)
	record := h.start(t, open, hkSubstantialPlan)
	hkToV2096Shape(t, record.Directory)
	decision, reason := hkDecision(t, h.prePublish(t, open, "git commit -m x"))
	if decision != "deny" {
		t.Fatalf("permissionDecision %q for an open legacy record, want deny; reason %q", decision, reason)
	}
	for _, want := range []string{record.ID, record.Plan.Title, "move " + record.Directory + " out of the task store, then start a new task with `cw task start`"} {
		if !strings.Contains(reason, want) {
			t.Errorf("deny reason %q does not contain %q", reason, want)
		}
	}
	for _, refused := range []string{"cw task verify --task " + record.Directory, "cw task abandon --task " + record.Directory} {
		if strings.Contains(reason, refused) {
			t.Errorf("deny reason %q offers %q, which cannot load a legacy record", reason, refused)
		}
	}

	done := hkRepo(t)
	source := []byte("verified source\n")
	if err := os.WriteFile(filepath.Join(done, "source.txt"), source, 0o600); err != nil {
		t.Fatal(err)
	}
	finished := h.start(t, done, hkSubstantialPlan)
	h.assessAndVerify(t, finished, source)
	hkToV2096Shape(t, finished.Directory)
	hkExpectAllow(t, h.prePublish(t, done, "git commit -m x"))
}
