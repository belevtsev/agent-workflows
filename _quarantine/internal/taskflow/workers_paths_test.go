package taskflow

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerPathsRejectIntermediateSymlinkAliases(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.Mkdir(filepath.Join(record.Baseline.Root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	data := []byte("owned source\n")
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "src", "file.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src", filepath.Join(record.Baseline.Root, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	record = mustAssignWorker(t, record, workerBrief("writer", "/root/writer", "implementation", []string{"src/file.txt"}))
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("alias_writer", "/root/alias", "implementation", []string{"alias/file.txt"})); err == nil || !strings.Contains(err.Error(), "symlink components") {
		t.Fatalf("intermediate ownership alias accepted: %v", err)
	}
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("reader_alias", "/root/reader", "evidence", []string{"alias/missing.txt"})); err == nil {
		t.Fatal("future path behind symlink ownership accepted")
	}
	record = mustAssignWorker(t, record, workerBrief("reader", "/root/reader", "evidence", []string{"source.txt"}))
	aliasRef := SourceRef{Path: "alias/file.txt", StartLine: 1, EndLine: 1, SHA256: digest(data)}
	result := workerResult(t, record, "reader", "completed", aliasRef)
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); err == nil || !strings.Contains(err.Error(), "symlink components") {
		t.Fatalf("intermediate evidence alias accepted: %v", err)
	}
	result.Evidence = []SourceRef{ref}
	result.Findings = []WorkerFinding{{ID: "alias", Summary: "Aliased finding evidence.", Severity: "major", Evidence: []SourceRef{aliasRef}}}
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); err == nil {
		t.Fatal("intermediate finding alias accepted")
	}
	result.Findings = []WorkerFinding{}
	record = mustSubmitWorker(t, record, result)
	acceptance := workerAcceptance(t, record, "reader", "accepted", aliasRef)
	if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
		t.Fatal("intermediate coordinator evidence alias accepted")
	}

	// Whole-project ownership must not permit declared edits or deletions
	// through an alias, even though lexical scope checking would allow them.
	other, otherRef := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.Mkdir(filepath.Join(other.Baseline.Root, "src"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other.Baseline.Root, "src", "file.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src", filepath.Join(other.Baseline.Root, "alias")); err != nil {
		t.Fatal(err)
	}
	other = mustAssignWorker(t, other, workerBrief("whole", "/root/writer", "implementation", []string{"."}))
	for _, changed := range []WorkerChangedFile{{Path: "alias/file.txt", SHA256: digest(data)}, {Path: "alias/missing.txt", Deleted: true}} {
		result := workerResult(t, other, "whole", "completed", otherRef)
		result.ChangedFiles = []WorkerChangedFile{changed}
		if _, err := SubmitWorkerResult(t.Context(), other.Directory, result); err == nil || !strings.Contains(err.Error(), "symlink components") {
			t.Fatalf("declared alias edit/deletion accepted: %v", err)
		}
	}
}

func TestWorkerOwnershipRechecksPathAliasesAtHandoffAndFreshness(t *testing.T) {
	t.Parallel()
	for _, completed := range []bool{false, true} {
		t.Run(map[bool]string{false: "handoff", true: "freshness"}[completed], func(t *testing.T) {
			record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
			if err := os.Mkdir(filepath.Join(record.Baseline.Root, "src"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(record.Baseline.Root, "src", "file.txt"), []byte("owned\n"), 0600); err != nil {
				t.Fatal(err)
			}
			record = mustAssignWorker(t, record, workerBrief("worker", "/root/worker", "implementation", []string{"src/file.txt"}))
			if completed {
				record = mustSubmitWorker(t, record, workerResult(t, record, "worker", "completed", ref))
				record = mustAcceptWorker(t, record, workerAcceptance(t, record, "worker", "accepted", ref))
			}
			if err := os.Rename(filepath.Join(record.Baseline.Root, "src"), filepath.Join(record.Baseline.Root, "actual")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("actual", filepath.Join(record.Baseline.Root, "src")); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			mustAssess(t, record, ref)
			if completed {
				verified, err := Verify(t.Context(), record.Directory)
				if err == nil || !strings.Contains(strings.Join(verified.Problems, " "), "symlink components") {
					t.Fatalf("freshness followed intermediate alias: %+v %v", verified, err)
				}
			} else {
				if _, err := SubmitWorkerResult(t.Context(), record.Directory, workerResult(t, record, "worker", "completed", ref)); err == nil || !strings.Contains(err.Error(), "symlink components") {
					t.Fatalf("handoff followed intermediate alias: %v", err)
				}
				record = mustSubmitWorker(t, record, workerResult(t, record, "worker", "cancelled"))
			}
			record = mustAcceptWorker(t, record, workerAcceptance(t, record, "worker", "unused"))
			if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
				t.Fatalf("alias-invalidated work could not close as unused: %+v %v", verified, err)
			}
		})
	}
}

func TestWorkerOwnershipRejectsDirectHardlinkAliasesAndRechecksThem(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.Link(filepath.Join(record.Baseline.Root, "source.txt"), filepath.Join(record.Baseline.Root, "linked.txt")); err != nil {
		t.Skipf("hardlinks unavailable: %v", err)
	}
	record = mustAssignWorker(t, record, workerBrief("original", "/root/original", "implementation", []string{"source.txt"}))
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("linked", "/root/linked", "implementation", []string{"linked.txt"})); err == nil || !strings.Contains(err.Error(), "overlaps active") {
		t.Fatalf("direct hardlink writer alias accepted: %v", err)
	}
	record = mustAssignWorker(t, record, workerBrief("other", "/root/other", "implementation", []string{"other.txt"}))
	if err := os.Link(filepath.Join(record.Baseline.Root, "source.txt"), filepath.Join(record.Baseline.Root, "other.txt")); err != nil {
		t.Fatal(err)
	}
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, workerResult(t, record, "original", "completed", ref)); err == nil || !strings.Contains(err.Error(), "overlaps active") {
		t.Fatalf("new direct hardlink alias bypassed handoff check: %v", err)
	}
	record = mustSubmitWorker(t, record, workerResult(t, record, "original", "cancelled"))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "original", "unused"))
}

func TestWorkerPathsUseExactExistingFilesystemSpelling(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	upper := filepath.Join(record.Baseline.Root, "Canonical.txt")
	lower := filepath.Join(record.Baseline.Root, "canonical.txt")
	if err := os.WriteFile(upper, []byte("upper\n"), 0600); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("upper", "/root/upper", "implementation", []string{"Canonical.txt"}))
	upperInfo, err := os.Stat(upper)
	if err != nil {
		t.Fatal(err)
	}
	lowerInfo, err := os.Stat(lower)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(lower, []byte("lower\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("lower", "/root/lower", "implementation", []string{"canonical.txt"})); err != nil {
			t.Fatalf("distinct case-sensitive source names rejected: %v", err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(upperInfo, lowerInfo) {
		t.Skip("filesystem returned distinct unexpected case entries")
	}
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("lower", "/root/lower", "implementation", []string{"canonical.txt"})); err == nil || !strings.Contains(err.Error(), "exact on-disk") {
		t.Fatalf("case alias accepted: %v", err)
	}
}

func TestPersistedAcceptanceOnlyCheckRunCannotBeRewritten(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	mustAssess(t, record, ref)
	record = mustAssignWorker(t, record, workerBrief("evidence", "/root/evidence", "evidence", []string{"source.txt"}))
	record = mustSubmitWorker(t, record, workerResult(t, record, "evidence", "completed", ref))
	input := workerAcceptance(t, record, "evidence", "accepted", ref)
	input.CheckIDs = []string{"focused"}
	record = mustAcceptWorker(t, record, input)
	if len(record.WorkerResults[0].CheckRuns) != 0 || len(record.WorkerAcceptances[0].CheckRuns) != 1 {
		t.Fatal("acceptance-only check fixture was not isolated")
	}
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("valid acceptance-only run failed: %+v %v", verified, err)
	}
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("fabricated replacement output\n")
	if err := os.WriteFile(filepath.Join(record.Directory, "replacement.log"), data, 0600); err != nil {
		t.Fatal(err)
	}
	appendWorkerRevision(t, record, func(next *Record) {
		next.Checks[0].Log = "replacement.log"
		next.Checks[0].LogSHA256, next.Checks[0].OutputSHA256 = digest(data), digest(data)
		next.Checks[0].OutputBytes = int64(len(data))
		next.EvidenceRevision++
	})
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "worker-bound terminal check") {
		t.Fatalf("acceptance-only recorded run rewrite accepted: %v", err)
	}
	if _, err := Verify(t.Context(), record.Directory); err == nil {
		t.Fatal("acceptance-only check rewrite reached completion")
	}
}
