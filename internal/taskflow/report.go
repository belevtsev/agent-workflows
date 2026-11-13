package taskflow

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// Handoff caps (D15). A result submitted to a task with a profile encodes to
// at most MaxResultBytes (see handoffCaps for the members it leaves out), its
// summary holds at most MaxResultSummaryBytes and
// each finding's summary is one line of at most MaxFindingBytes; the full
// reasoning goes in the report file its report member names, at most
// MaxReportBytes. The caps apply when a result is submitted, never to a
// stored one, and a task without a profile keeps the boundedText limits.
const (
	MaxResultBytes        = 6 << 10
	MaxResultSummaryBytes = 1200
	MaxFindingBytes       = 400
	MaxReportBytes        = 512 << 10
)

// ReportRef names a worker's full report: a regular file, inside or outside
// the project, with the SHA-256 of its bytes. handoff stores a copy in the
// task directory as ReportFileName of the assignment.
type ReportRef struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// ReportFileName is the name, in the task directory, of the stored copy of
// the report of the result of assignment id.
func ReportFileName(id string) string { return "report-" + id + ".md" }

// validateReportRef checks the shape of a result's report member: only a
// task with a profile takes one, with a clean absolute path and the
// lowercase SHA-256 of its bytes.
func validateReportRef(record Record, ref *ReportRef) error {
	if ref == nil {
		return nil
	}
	if record.Profile == nil {
		return errors.New("worker result report needs a task with a profile; give the reasoning in summary and findings")
	}
	if !filepath.IsAbs(ref.Path) || filepath.Clean(ref.Path) != ref.Path || len(ref.Path) > 4096 || strings.ContainsFunc(ref.Path, unicode.IsControl) || !lowerSHA256(ref.SHA256) {
		return errors.New("worker result report needs a clean absolute path and the lowercase SHA-256 of its bytes")
	}
	return nil
}

// handoffCaps applies the handoff caps to a result submitted to a task with a
// profile. Load never calls it, so results stored before the caps still load.
func handoffCaps(record Record, result WorkerResult) error {
	if record.Profile == nil {
		return nil
	}
	if len(result.Summary) > MaxResultSummaryBytes {
		return fmt.Errorf("worker result summary is %d bytes, over the %d-byte limit; keep the outcome in summary and move the detail into the file the result's report member names", len(result.Summary), MaxResultSummaryBytes)
	}
	for _, finding := range result.Findings {
		if len(finding.Summary) > MaxFindingBytes || strings.ContainsAny(finding.Summary, "\r\n") {
			return fmt.Errorf("worker finding %s summary must be one line of at most %d bytes (it is %d); move its reasoning into the file the result's report member names", finding.ID, MaxFindingBytes, len(finding.Summary))
		}
	}
	// The change sets how many changed_files and reach_runs entries a result
	// needs, and the brief how many requirement_map entries (R11 allows 256
	// tests per requirement); cw checks each against the source, the bundle or
	// the brief, and none can move to the report, so the cap leaves them out.
	measured := result
	measured.ChangedFiles, measured.ReachRuns, measured.RequirementMap = []WorkerChangedFile{}, nil, nil
	data, err := encode(measured)
	if err != nil {
		return err
	}
	if len(data) > MaxResultBytes {
		return fmt.Errorf("worker result encodes to %d bytes without changed_files, reach_runs and requirement_map, over the %d-byte (6 KiB) limit; keep summary and findings short and put the full report in the file the result's report member names", len(data), MaxResultBytes)
	}
	return nil
}

// storeReport copies the report ref names into the task directory as
// ReportFileName(id), after checking its size and hash. The caller holds the
// task lock and has found no result of id, so a copy already there was left by
// a submission that failed before its revision was published, and is
// replaced.
func storeReport(root *os.Root, id string, ref ReportRef) (string, error) {
	data, err := readInputFile("report "+ref.Path, ref.Path)
	if err != nil {
		return "", err
	}
	if len(data) > MaxReportBytes {
		return "", fmt.Errorf("report %s is %d bytes, over the %d-byte (512 KiB) limit", ref.Path, len(data), MaxReportBytes)
	}
	if digest(data) != ref.SHA256 {
		return "", fmt.Errorf("report %s sha256 differs from the file's bytes", ref.Path)
	}
	name := ReportFileName(id)
	if err := root.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if err := writeNew(root, name, data); err != nil {
		return "", err
	}
	return name, nil
}

// storedReportProblem checks that the stored copy of each recorded report
// exists and hashes to its result's report sha256.
func storedReportProblem(root *os.Root, record Record) error {
	for _, bound := range record.WorkerResults {
		ref := bound.Result.Report
		if ref == nil {
			continue
		}
		data, err := privateRead(root, ReportFileName(bound.Result.AssignmentID), MaxReportBytes)
		if err != nil || digest(data) != ref.SHA256 {
			return fmt.Errorf("stored report %s of worker result %s is missing or differs from its sha256", ReportFileName(bound.Result.AssignmentID), bound.Result.AssignmentID)
		}
	}
	return nil
}
