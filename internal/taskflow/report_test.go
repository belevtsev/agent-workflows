package taskflow

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// reportTask starts a task with the default profile, or, with legacy, one
// without a profile, and assigns it the evidence brief ev.
func reportTask(t *testing.T, legacy bool) (Record, SourceRef) {
	t.Helper()
	record, ref := stageTask(t, legacy)
	record = mustAssignWorker(t, record, workerBrief("ev", "/root/ev", "evidence", []string{"."}))
	return record, ref
}

// findingText is a one-line finding summary of n bytes for finding i.
func findingText(i, n int) string {
	prefix := fmt.Sprintf("F%d: the empty case returns the wrong error ", i)
	return prefix + strings.Repeat("x", n-len(prefix))
}

// oversizeResult is a completed result of ev whose summary and findings each
// fit their caps (one-line findings of 380 bytes) but which encodes to at
// least 7 KiB, with the findings it holds.
func oversizeResult(t *testing.T, record Record, ref SourceRef) WorkerResult {
	t.Helper()
	result := workerResult(t, record, "ev", "completed", ref)
	for i := 1; ; i++ {
		data, err := encode(result)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) >= 7<<10 {
			if len(data) >= 8<<10 {
				t.Fatalf("precondition: result encodes to %d bytes, want 7 KiB to 8 KiB", len(data))
			}
			return result
		}
		result.Findings = append(result.Findings, WorkerFinding{ID: fmt.Sprintf("F%d", i), Summary: findingText(i, 380), Severity: "minor", Evidence: []SourceRef{ref}})
	}
}

// writeReport writes data to a fresh file and returns its ReportRef.
func writeReport(t *testing.T, data []byte) *ReportRef {
	t.Helper()
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return &ReportRef{Path: path, SHA256: digest(data)}
}

// Oracle: D15 caps a handoff of a task with a profile at 6 KiB encoded. A
// completed result of 7 KiB to 8 KiB, whose summary and one-line findings
// each fit their own caps, is refused as invalid input; the error names the
// 6144-byte limit and the report member, and no revision is written.
func TestHandoffOverSixKiBIsRefusedNamingTheReport(t *testing.T) {
	record, ref := reportTask(t, false)
	before := revisions(t, record)
	_, err := SubmitWorkerResult(t.Context(), record.Directory, oversizeResult(t, record, ref))
	if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "6144-byte") || !strings.Contains(err.Error(), "report member") {
		t.Fatalf("SubmitWorkerResult = %v; want invalid input naming the 6144-byte limit and the report member", err)
	}
	if after := revisions(t, record); after != before {
		t.Fatalf("revisions = %d, want %d", after, before)
	}
}

// Oracle: the content of the refused 7 KiB result, split into a result that
// keeps two findings and a report file holding every finding's text, is
// accepted. The stored copy report-ev.md holds the report's exact bytes,
// which hash to the recorded report sha256; the handoff writes it, and Load
// refuses the task once the copy's bytes change.
func TestHandoffSplitIntoResultAndReportIsStoredAndVerified(t *testing.T) {
	record, ref := reportTask(t, false)
	full := oversizeResult(t, record, ref)
	var report bytes.Buffer
	report.WriteString("# Evidence report\n\n")
	for _, finding := range full.Findings {
		fmt.Fprintf(&report, "## %s\n\n%s\n\n", finding.ID, finding.Summary)
	}
	split := full
	split.Findings = full.Findings[:2]
	split.Report = writeReport(t, report.Bytes())
	record = mustSubmitWorker(t, record, split)

	stored := filepath.Join(record.Directory, ReportFileName("ev"))
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatal(err)
	}
	recorded := findWorkerResult(record, "ev").Result.Report
	if recorded == nil || !bytes.Equal(data, report.Bytes()) || digest(data) != recorded.SHA256 {
		t.Fatalf("stored report: recorded %+v, bytes equal %v, sha256 %s", recorded, bytes.Equal(data, report.Bytes()), digest(data))
	}
	mustLoad(t, record.Directory)

	if err := os.WriteFile(stored, append(data, "tampered\n"...), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "stored report report-ev.md") {
		t.Fatalf("Load after tampering = %v; want the stored report refused", err)
	}
}

// Oracle: a report of 512 KiB is stored; one of 512 KiB plus one byte is
// refused with the 524288-byte limit named, and leaves neither a revision nor
// a stored copy.
func TestHandoffReportOverHalfAMebibyteIsRefused(t *testing.T) {
	record, ref := reportTask(t, false)
	result := workerResult(t, record, "ev", "completed", ref)
	result.Report = writeReport(t, bytes.Repeat([]byte("r"), 512<<10+1))
	before := revisions(t, record)
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); err == nil || !strings.Contains(err.Error(), "524288-byte") {
		t.Fatalf("SubmitWorkerResult = %v; want the 524288-byte limit named", err)
	}
	if after := revisions(t, record); after != before {
		t.Fatalf("revisions = %d, want %d", after, before)
	}
	if _, err := os.Lstat(filepath.Join(record.Directory, ReportFileName("ev"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("report copy = %v; want none", err)
	}
	result.Report = writeReport(t, bytes.Repeat([]byte("r"), 512<<10))
	mustSubmitWorker(t, record, result)
	mustLoad(t, record.Directory)
}

// Oracle: in a task with a profile a summary of 1,200 bytes is accepted and
// one of 1,201 bytes refused; a one-line finding of 400 bytes is accepted and
// one of 401 bytes, or one that spans two lines, refused. Each refusal is
// invalid input naming its limit and the report member, and writes nothing.
func TestHandoffSummaryAndFindingCaps(t *testing.T) {
	cases := []struct {
		name    string
		summary int
		finding string
		limit   string
		refused bool
	}{
		{name: "summary at cap", summary: 1200, finding: findingText(1, 400)},
		{name: "summary over cap", summary: 1201, finding: findingText(1, 100), limit: "1200-byte", refused: true},
		{name: "finding over cap", summary: 100, finding: findingText(1, 401), limit: "at most 400 bytes", refused: true},
		{name: "finding on two lines", summary: 100, finding: "F1: first line\nsecond line", limit: "one line", refused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, ref := reportTask(t, false)
			result := workerResult(t, record, "ev", "completed", ref)
			result.Summary = strings.Repeat("s", tc.summary)
			result.Findings = []WorkerFinding{{ID: "F1", Summary: tc.finding, Severity: "minor", Evidence: []SourceRef{ref}}}
			before := revisions(t, record)
			_, err := SubmitWorkerResult(t.Context(), record.Directory, result)
			if !tc.refused {
				if err != nil {
					t.Fatalf("SubmitWorkerResult = %v; want accepted", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), tc.limit) || !strings.Contains(err.Error(), "report member") {
				t.Fatalf("SubmitWorkerResult = %v; want invalid input naming %q and the report member", err, tc.limit)
			}
			if after := revisions(t, record); after != before {
				t.Fatalf("revisions = %d, want %d", after, before)
			}
		})
	}
}

// Oracle: under a profile each free-text member of a reach run (mode,
// command, observed, reason) is capped at 400 bytes like a finding: 401 bytes
// in any one is refused as invalid input with the exact error naming the
// row's path, the member, the limit and the report, and writes nothing; 400
// bytes in each is accepted. Without a profile 401 bytes is accepted.
func TestHandoffReachRunTextIsCapped(t *testing.T) {
	review := func(t *testing.T, legacy bool) (Record, WorkerResult) {
		t.Helper()
		record, _ := stageTask(t, legacy)
		ref := changedWorkerSource(t, record)
		record = mustAssignWorker(t, record, workerBrief("rv", "/root/rv", "review", []string{"."}))
		result := workerResult(t, record, "rv", "completed", ref)
		if len(result.ReachRuns) == 0 {
			t.Fatal("precondition: the review's bundle has a reach row")
		}
		return record, result
	}
	set := func(run *ReachRun, member string, value string) {
		switch member {
		case "mode":
			run.Mode = value
		case "command":
			run.Command = value
		case "observed":
			run.Observed = value
		case "reason":
			run.Status, run.Command, run.Observed, run.Reason = "not_run", "", "", value
		}
	}
	for _, member := range []string{"mode", "command", "observed", "reason"} {
		t.Run(member, func(t *testing.T) {
			record, result := review(t, false)
			set(&result.ReachRuns[0], member, strings.Repeat("x", 401))
			before := revisions(t, record)
			want := fmt.Sprintf("reach_runs entry %s %s is 401 bytes, over the 400-byte limit; move the detail into the file the result's report member names", result.ReachRuns[0].Path, member)
			if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); !errors.Is(err, ErrInvalidInput) || !strings.HasSuffix(err.Error(), want) {
				t.Fatalf("SubmitWorkerResult = %v; want invalid input %q", err, want)
			}
			if after := revisions(t, record); after != before {
				t.Fatalf("revisions = %d, want %d", after, before)
			}
			set(&result.ReachRuns[0], member, strings.Repeat("x", 400))
			mustSubmitWorker(t, record, result)
		})
	}
	record, result := review(t, true)
	set(&result.ReachRuns[0], "observed", strings.Repeat("x", 401))
	mustSubmitWorker(t, record, result)
}

// Oracle: a task without a profile keeps the limits it had: a 7 KiB result
// with a 1,201-byte summary and a 401-byte finding on two lines is accepted
// and loads, and a report member, which such a task never took, is refused.
func TestHandoffOfATaskWithoutAProfileIsUncapped(t *testing.T) {
	record, ref := reportTask(t, true)
	result := oversizeResult(t, record, ref)
	result.Summary = strings.Repeat("s", 1201)
	result.Findings[0].Summary = "F1: first line\n" + strings.Repeat("x", 400)
	reported := result
	reported.Report = writeReport(t, []byte("# Report\n"))
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, reported); !errors.Is(err, ErrInvalidInput) || !strings.Contains(err.Error(), "needs a task with a profile") {
		t.Fatalf("SubmitWorkerResult with report = %v; want invalid input naming the profile", err)
	}
	record = mustSubmitWorker(t, record, result)
	if loaded := mustLoad(t, record.Directory); loaded.Profile != nil || findWorkerResult(loaded, "ev") == nil {
		t.Fatalf("Load = profile %+v, result %v; want none and the result", loaded.Profile, findWorkerResult(loaded, "ev"))
	}
	if _, err := os.Lstat(filepath.Join(record.Directory, ReportFileName("ev"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("report copy = %v; want none", err)
	}
}
