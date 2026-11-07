package taskflow

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

var findingSeverities = []string{"blocker", "major", "minor"}

// rejectedFindingPrefix opens every limitation that reports a rejected
// blocker or major finding.
const rejectedFindingPrefix = "rejected finding "

// IsRejectedFindingLimitation reports whether a verify limitation reports a
// rejected blocker or major finding, so a compact view can show it first.
func IsRejectedFindingLimitation(limitation string) bool {
	return strings.HasPrefix(limitation, rejectedFindingPrefix)
}

// severity returns the finding's severity. A finding without one was recorded
// before review handoffs had to set it, or came from a non-review role; it
// reads as major so that its rejection is still disclosed.
func (finding WorkerFinding) severity() string {
	if finding.Severity == "" {
		return "major"
	}
	return finding.Severity
}

// reviewSeverities requires a review handoff to give every finding a
// severity. It runs only on submission, so stored results still load.
func reviewSeverities(role string, findings []WorkerFinding) error {
	if role != "review" {
		return nil
	}
	for _, finding := range findings {
		if finding.Severity == "" {
			return fmt.Errorf("review finding %s needs severity blocker, major or minor", finding.ID)
		}
	}
	return nil
}

// rejectedFindingReport reads each assignment's latest acceptance. Every
// rejected blocker or major finding is a limitation, because the coordinator
// rejected it alone. A rejected blocker is also a problem until a later review
// rebinds the rejection (see rebound).
func rejectedFindingReport(root *os.Root, record Record) (problems, limitations []string) {
	for index, assigned := range record.Assignments {
		accepted := latestAcceptance(record, assigned.Brief.ID)
		result := findWorkerResult(record, assigned.Brief.ID)
		if accepted == nil || result == nil {
			continue
		}
		for _, disposition := range accepted.Acceptance.Findings {
			if disposition.Status != "rejected" {
				continue
			}
			at := slices.IndexFunc(result.Result.Findings, func(finding WorkerFinding) bool { return finding.ID == disposition.FindingID })
			if at < 0 {
				continue
			}
			severity := result.Result.Findings[at].severity()
			if severity == "minor" {
				continue
			}
			limitations = append(limitations, fmt.Sprintf("%s%s (%s) of worker assignment %s: %s", rejectedFindingPrefix, disposition.FindingID, severity, assigned.Brief.ID, disposition.Reason))
			if severity == "blocker" && !rebound(root, record, index, disposition.FindingID) {
				problems = append(problems, fmt.Sprintf("rejected blocker finding %s of worker assignment %s needs a later accepted independent review whose bundle lists it as rejected and whose result does not re-raise it (same finding id)", disposition.FindingID, assigned.Brief.ID))
			}
		}
	}
	return problems, limitations
}

// rebound reports whether a review assigned after record.Assignments[index]
// rebinds the rejection of its finding: the review is independent, its latest
// acceptance is accepted, its stored bundle lists the finding as rejected (so
// the reviewer saw the rejection and its reason), and its result raises no
// finding with the same ID.
func rebound(root *os.Root, record Record, index int, findingID string) bool {
	original := record.Assignments[index].Brief.ID
	for _, later := range record.Assignments[index+1:] {
		if later.Brief.Role != "review" || later.BundleFile == "" || !independentReviewer(record, later) {
			continue
		}
		accepted := latestAcceptance(record, later.Brief.ID)
		result := findWorkerResult(record, later.Brief.ID)
		if accepted == nil || accepted.Acceptance.Status != "accepted" || result == nil {
			continue
		}
		if slices.ContainsFunc(result.Result.Findings, func(finding WorkerFinding) bool { return finding.ID == findingID }) {
			continue
		}
		bundle, err := readStoredBundle(root, later)
		if err != nil {
			continue
		}
		if slices.ContainsFunc(bundle.PriorFindings, func(prior BundleFinding) bool {
			return prior.AssignmentID == original && prior.FindingID == findingID && prior.Disposition == "rejected"
		}) {
			return true
		}
	}
	return false
}
