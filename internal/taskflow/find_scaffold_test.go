package taskflow

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Scaffold for tests of record readers such as FindOpen. These helpers write
// record revisions directly, bypassing the recording commands, so a test can
// describe a stored state (a passing verification, an old creation time) in a
// few lines. They are approved test-only seams; production code never uses
// them.

// openPlan is a minimal plan whose requirement allows non-execution, with no
// required adviser checkpoints. kind is "small" or "substantial"; a
// substantial plan binds one check, which it never needs to run.
func openPlan(title, kind string) Plan {
	plan := Plan{Title: title, Kind: kind, Requirements: []Requirement{{ID: "source", Description: "Inspect actual source", CheckIDs: []string{}, AllowNonExecution: true}}, Checks: []Check{}}
	if kind == "substantial" {
		plan.Checkpoints = &CheckpointPolicy{Required: false, Reason: "scaffold"}
		plan.Requirements[0].CheckIDs = []string{"unit"}
		plan.Checks = []Check{{ID: "unit", Argv: []string{"go", "test", "./..."}}}
	}
	return plan
}

// startIn starts a record for repo in store with a minimal plan of the given
// kind and no advisers, and returns it. store must be private (0700) or not
// yet exist, for example filepath.Join(t.TempDir(), "tasks"); Start creates
// a missing store with private permissions.
func startIn(t *testing.T, store, repo, title, kind string) Record {
	t.Helper()
	record, err := Start(t.Context(), repo, store, openPlan(title, kind), nil)
	if err != nil {
		t.Fatalf("start %s: %v", title, err)
	}
	return record
}

// appendRevision writes the next revision of record's chain with mutate
// applied and returns the reloaded record. The immutable members (id,
// created_at, plan, baseline, advisers) must stay unchanged.
func appendRevision(t *testing.T, record Record, mutate func(*Record)) Record {
	t.Helper()
	previous, err := os.ReadFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision)))
	if err != nil {
		t.Fatal(err)
	}
	next := record
	mutate(&next)
	next.Revision = record.Revision + 1
	next.PreviousSHA256 = digest(previous)
	data, err := encode(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", next.Revision)), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("appended revision does not load: %v", err)
	}
	return loaded
}

// rewriteFirstRevision replaces revision 1 of a single-revision record with
// mutate applied (for example to age created_at) and returns the reloaded
// record. updated_at follows created_at when it would otherwise precede it.
func rewriteFirstRevision(t *testing.T, record Record, mutate func(*Record)) Record {
	t.Helper()
	if record.Revision != 1 {
		t.Fatalf("rewriteFirstRevision needs a single-revision record, got revision %d", record.Revision)
	}
	next := record
	mutate(&next)
	if next.UpdatedAt.Before(next.CreatedAt) {
		next.UpdatedAt = next.CreatedAt
	}
	data, err := encode(next)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000001.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatalf("rewritten revision does not load: %v", err)
	}
	return loaded
}
