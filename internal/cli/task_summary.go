package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"slices"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// The --summary results below are printed as one JSON line. Each names the task
// directory and only what the coordinator needs for its next step; the whole
// record stays available through 'cw task show'.

type taskStartSummary struct {
	Task         string            `json:"task"`
	TaskID       string            `json:"task_id"`
	Advisers     map[string]string `json:"advisers"`
	Requirements []string          `json:"requirements"`
	Checks       []string          `json:"checks"`
}

func startSummary(record taskflow.Record) taskStartSummary {
	value := taskStartSummary{Task: record.Directory, TaskID: record.ID, Advisers: record.Advisers, Requirements: []string{}, Checks: []string{}}
	if value.Advisers == nil {
		value.Advisers = map[string]string{}
	}
	for _, requirement := range record.Plan.Requirements {
		value.Requirements = append(value.Requirements, requirement.ID)
	}
	for _, check := range record.Plan.Checks {
		value.Checks = append(value.Checks, check.ID)
	}
	return value
}

type taskRunSummary struct {
	CheckID  string `json:"check_id"`
	RunID    string `json:"run_id"`
	Status   string `json:"status"`
	ExitCode *int   `json:"exit_code,omitempty"`
	Failure  string `json:"failure,omitempty"`
	Log      string `json:"log"`
	// A failed or timed-out run also quotes bounded excerpts of its stored log.
	FailingTests       []string `json:"failing_tests,omitempty"`
	ErrorLines         []string `json:"error_lines,omitempty"`
	LogTail            []string `json:"log_tail,omitempty"`
	LogTruncated       bool     `json:"log_truncated,omitzero"`
	ExcerptUnavailable string   `json:"excerpt_unavailable,omitempty"`
}

func runSummary(task string, run taskflow.CheckRun) taskRunSummary {
	value := taskRunSummary{CheckID: run.CheckID, RunID: run.ID, Status: run.Status, ExitCode: run.ExitCode, Failure: run.Failure, Log: filepath.Join(task, run.Log)}
	if run.Status != "failed" && run.Status != "timed_out" {
		return value
	}
	excerpt := excerptLog(task, run.Log)
	value.FailingTests, value.ErrorLines, value.LogTail, value.ExcerptUnavailable = excerpt.FailingTests, excerpt.ErrorLines, excerpt.Tail, excerpt.Unavailable
	value.LogTruncated = run.Truncated
	// A run with per-test results names its failing tests from them, not
	// from the log, which for a -json run holds JSON events.
	if run.TestResults != "" {
		if results, err := taskflow.ReadTestResults(task, run); err == nil {
			value.FailingTests = nil
			for _, test := range results.Tests {
				if test.Fail > 0 && len(value.FailingTests) < maxFailingTests && !slices.Contains(value.FailingTests, test.Test) {
					value.FailingTests = append(value.FailingTests, test.Test)
				}
			}
		}
	}
	return value
}

type taskAssessSummary struct {
	Task         string            `json:"task"`
	Requirements map[string]string `json:"requirements"`
	Decision     string            `json:"decision,omitempty"`
}

func assessSummary(record taskflow.Record, input taskflow.AssessmentInput) taskAssessSummary {
	value := taskAssessSummary{Task: record.Directory, Requirements: map[string]string{}}
	for _, assessment := range input.Requirements {
		if bound, ok := record.Assessments[assessment.ID]; ok {
			value.Requirements[assessment.ID] = bound.Assessment.Status
		}
	}
	if input.Decision != nil && record.Decision != nil {
		value.Decision = record.Decision.Decision.Status
	}
	return value
}

type taskAssignSummary struct {
	Task             string `json:"task"`
	AssignmentID     string `json:"assignment_id"`
	AssignmentSHA256 string `json:"assignment_sha256"`
	Fingerprint      string `json:"fingerprint"`
}

func assignSummary(record taskflow.Record, id string) taskAssignSummary {
	value := taskAssignSummary{Task: record.Directory, AssignmentID: id}
	for _, assignment := range record.Assignments {
		if assignment.Brief.ID == id {
			value.AssignmentSHA256 = assignment.SHA256
			value.Fingerprint = assignment.Snapshot.Fingerprint
		}
	}
	return value
}

type taskHandoffSummary struct {
	Task         string   `json:"task"`
	AssignmentID string   `json:"assignment_id"`
	ResultSHA256 string   `json:"result_sha256"`
	Status       string   `json:"status"`
	CheckRuns    []string `json:"check_runs"`
}

func handoffSummary(record taskflow.Record, id string) taskHandoffSummary {
	value := taskHandoffSummary{Task: record.Directory, AssignmentID: id, CheckRuns: []string{}}
	for _, result := range record.WorkerResults {
		if result.Result.AssignmentID == id {
			value.ResultSHA256 = result.SHA256
			value.Status = result.Result.Status
			if result.CheckRuns != nil {
				value.CheckRuns = result.CheckRuns
			}
		}
	}
	return value
}

type taskAcceptSummary struct {
	Task         string            `json:"task"`
	AssignmentID string            `json:"assignment_id"`
	Status       string            `json:"status"`
	Findings     map[string]string `json:"findings"`
}

func acceptSummary(record taskflow.Record, id string) taskAcceptSummary {
	value := taskAcceptSummary{Task: record.Directory, AssignmentID: id, Findings: map[string]string{}}
	for _, acceptance := range record.WorkerAcceptances {
		if acceptance.Acceptance.AssignmentID != id {
			continue
		}
		value.Status = acceptance.Acceptance.Status
		value.Findings = map[string]string{}
		for _, finding := range acceptance.Acceptance.Findings {
			value.Findings[finding.FindingID] = finding.Status
		}
	}
	return value
}

type taskConsultSummary struct {
	Task           string            `json:"task"`
	Phase          string            `json:"phase"`
	DecisionStatus string            `json:"decision_status"`
	Providers      map[string]string `json:"providers"`
}

func consultSummary(record taskflow.Record, phase string) taskConsultSummary {
	attachment := record.Consultations[phase]
	value := taskConsultSummary{Task: record.Directory, Phase: phase, DecisionStatus: attachment.DecisionStatus, Providers: attachment.ProviderStatuses}
	if value.Providers == nil {
		value.Providers = map[string]string{}
	}
	return value
}

type taskBundleSummary struct {
	Bundle       string `json:"bundle"`
	SHA256       string `json:"sha256"`
	ReviewID     string `json:"review_id"`
	Kind         string `json:"kind"`
	Rows         int    `json:"rows"`
	ChangedFiles int    `json:"changed_files"`
}

// bundleSummary names the written bundle and its hash; the coordinator fills
// the rows, so the brief needs the hash of the filled file, not this one.
func bundleSummary(path string, data []byte, bundle taskflow.Bundle) taskBundleSummary {
	sum := sha256.Sum256(data)
	return taskBundleSummary{Bundle: path, SHA256: hex.EncodeToString(sum[:]), ReviewID: bundle.ReviewID, Kind: bundle.Kind, Rows: len(bundle.Reach), ChangedFiles: len(bundle.ChangedFiles)}
}
