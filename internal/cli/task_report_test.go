package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// Oracle: a handoff whose result names a report prints, in its summary, the
// stored copy's name relative to the task directory, report-<assignment>.md,
// and that file holds the report's bytes.
func TestHandoffSummaryNamesTheStoredReport(t *testing.T) {
	h := newSummaryCLIHarness(t)
	start := parseSummaryLineForTest(t, "start", h.start(true), "task_id", "profile", "advisers", "requirements", "checks")
	h.task = h.directoryOf(summaryStringForTest(t, start, "task_id"))
	h.taskInput("assign", summaryCLIBrief(), true)
	report := []byte("# Inspection\n\nThe declaration is on line 1.\n")
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, report, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(report)
	result := summaryCLIResult()
	result["report"] = map[string]any{"path": path, "sha256": hex.EncodeToString(sum[:])}
	handoff := parseSummaryLineForTest(t, "handoff", h.taskInput("handoff", result, true),
		"task_id", "assignment_id", "result_sha256", "status", "check_runs", "report")
	if got := summaryStringForTest(t, handoff, "report"); got != "report-inspect.md" {
		t.Fatalf("handoff report = %q, want report-inspect.md", got)
	}
	if stored, err := os.ReadFile(filepath.Join(h.task, "report-inspect.md")); err != nil || !bytes.Equal(stored, report) {
		t.Fatalf("stored report = %q, %v; want the report's bytes", stored, err)
	}
}
