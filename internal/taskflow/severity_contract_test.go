package taskflow

import (
	"strings"
	"testing"
)

// Independent contract tests for worker finding severity:
//
//   - severity is blocker, major or minor; an invalid value is refused for any
//     role, a missing value is refused for review handoffs only;
//   - stored findings without severity read as major;
//   - verify lists every rejected blocker or major finding as a limitation,
//     minor gives nothing;
//   - a rejected blocker is a problem until a later accepted independent
//     review whose bundle lists it as rejected and whose result does not
//     raise the same finding id again.

func severityFinding(id, severity string, ref SourceRef) WorkerFinding {
	return WorkerFinding{ID: id, Summary: "The changed line drops an error.", Severity: severity, Evidence: []SourceRef{ref}}
}

func severityRejected(id string, ref SourceRef) FindingDisposition {
	return FindingDisposition{FindingID: id, Status: "rejected", Reason: "The first line returns the error.", Evidence: []SourceRef{ref}}
}

func severityMentions(lines []string, fragment string) []string {
	var found []string
	for _, line := range lines {
		if strings.Contains(line, fragment) {
			found = append(found, line)
		}
	}
	return found
}

func severityRejectedLimitations(verification Verification, id string) []string {
	var found []string
	for _, limitation := range severityMentions(verification.Limitations, id) {
		if IsRejectedFindingLimitation(limitation) {
			found = append(found, limitation)
		}
	}
	return found
}

func severityVerify(t *testing.T, record Record) Verification {
	t.Helper()
	// Verify reports an incomplete task as an error alongside the
	// verification; only a missing verification is fatal here.
	verification, err := Verify(t.Context(), record.Directory)
	if verification.TaskID == "" {
		t.Fatalf("verify returned no verification: %v", err)
	}
	return verification
}

// severitySubmitRefused asserts the result is refused with an error naming
// severity and that no task file changed.
func severitySubmitRefused(t *testing.T, record Record, result WorkerResult) {
	t.Helper()
	before := productionTaskBytes(t, record.Directory)
	_, err := SubmitWorkerResult(t.Context(), record.Directory, result)
	if err == nil {
		t.Fatalf("result with findings %+v accepted", result.Findings)
	}
	if !strings.Contains(err.Error(), "severity") {
		t.Errorf("refusal %q does not name severity", err)
	}
	after := productionTaskBytes(t, record.Directory)
	if len(before) != len(after) {
		t.Errorf("refused result changed the task files: %d before, %d after", len(before), len(after))
	}
	for name, data := range before {
		if after[name] != data {
			t.Errorf("refused result changed %s", name)
		}
	}
}

func TestSeverityContractValidationByRole(t *testing.T) {
	t.Parallel()
	invalid := []string{"critical", "Major", "BLOCKER", " minor", "minor ", "info"}

	t.Run("review", func(t *testing.T) {
		record, ref := preparedChangedTask(t)
		record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
		for _, severity := range append([]string{""}, invalid...) {
			result := workerResult(t, record, "review", "completed", ref)
			result.Findings = []WorkerFinding{severityFinding("F1", severity, ref)}
			severitySubmitRefused(t, record, result)
		}
		// One bad finding among valid ones still refuses the whole result.
		result := workerResult(t, record, "review", "completed", ref)
		result.Findings = []WorkerFinding{severityFinding("F1", "major", ref), severityFinding("F2", "", ref)}
		severitySubmitRefused(t, record, result)

		result = workerResult(t, record, "review", "completed", ref)
		result.Findings = []WorkerFinding{severityFinding("F1", "blocker", ref), severityFinding("F2", "major", ref), severityFinding("F3", "minor", ref)}
		mustSubmitWorker(t, record, result)
	})

	t.Run("implementation", func(t *testing.T) {
		record, ref := preparedChangedTask(t)
		record = mustAssignWorker(t, record, workerBrief("impl", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
		for _, severity := range invalid {
			result := workerResult(t, record, "impl", "completed", ref)
			result.Findings = []WorkerFinding{severityFinding("F1", severity, ref)}
			severitySubmitRefused(t, record, result)
		}
		// A non-review handoff may omit severity.
		result := workerResult(t, record, "impl", "completed", ref)
		result.Findings = []WorkerFinding{severityFinding("F1", "", ref)}
		mustSubmitWorker(t, record, result)
	})
}

// A stored finding without severity reads as major: rejected, it is listed as
// a limitation and is not a blocker problem.
func TestSeverityContractMissingReadsAsMajor(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, severity string
		limitation     bool
	}{
		{"missing", "", true},
		{"major", "major", true},
		{"minor", "minor", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			record, ref := preparedChangedTask(t)
			id := "impl-finding-" + tc.name
			record = mustAssignWorker(t, record, workerBrief("impl", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
			result := workerResult(t, record, "impl", "completed", ref)
			result.Findings = []WorkerFinding{severityFinding(id, tc.severity, ref)}
			record = mustSubmitWorker(t, record, result)
			acceptance := workerAcceptance(t, record, "impl", "accepted", ref)
			acceptance.Findings = []FindingDisposition{severityRejected(id, ref)}
			record = mustAcceptWorker(t, record, acceptance)

			verification := severityVerify(t, record)
			got := severityRejectedLimitations(verification, id)
			if tc.limitation && len(got) != 1 {
				t.Errorf("rejected %s finding limitations %q, want one\nlimitations: %q", tc.name, got, verification.Limitations)
			}
			if !tc.limitation && len(severityMentions(verification.Limitations, id)) != 0 {
				t.Errorf("rejected minor finding listed: %q", severityMentions(verification.Limitations, id))
			}
			if problems := severityMentions(verification.Problems, id); len(problems) != 0 {
				t.Errorf("rejected %s finding is a problem: %q", tc.name, problems)
			}
		})
	}
}

// severityBlockerTask returns a task whose first review raised a blocker the
// coordinator rejected.
func severityBlockerTask(t *testing.T, id string) (Record, SourceRef) {
	t.Helper()
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review-1", "/root/reviewer-1", "review", []string{"."}, "focused"))
	result := workerResult(t, record, "review-1", "completed", ref)
	result.Findings = []WorkerFinding{severityFinding(id, "blocker", ref)}
	record = mustSubmitWorker(t, record, result)
	acceptance := workerAcceptance(t, record, "review-1", "accepted", ref)
	acceptance.Findings = []FindingDisposition{severityRejected(id, ref)}
	record = mustAcceptWorker(t, record, acceptance)
	verification := severityVerify(t, record)
	if len(severityMentions(verification.Problems, id)) == 0 {
		t.Fatalf("a rejected blocker without a later review is not a problem\nproblems: %q", verification.Problems)
	}
	if len(severityRejectedLimitations(verification, id)) != 1 {
		t.Fatalf("a rejected blocker is not one limitation\nlimitations: %q", verification.Limitations)
	}
	return record, ref
}

func TestSeverityContractRejectedBlockerNeedsLaterReview(t *testing.T) {
	t.Parallel()
	const id = "blocker-finding-9"

	t.Run("cleared by a later accepted review without the finding", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		record = mustAssignWorker(t, record, workerBrief("review-2", "/root/reviewer-2", "review", []string{"."}, "focused"))
		record = mustSubmitWorker(t, record, workerResult(t, record, "review-2", "completed", ref))
		record = mustAcceptWorker(t, record, workerAcceptance(t, record, "review-2", "accepted", ref))
		verification := severityVerify(t, record)
		if problems := severityMentions(verification.Problems, id); len(problems) != 0 {
			t.Errorf("later accepted review left problems %q", problems)
		}
		if len(severityRejectedLimitations(verification, id)) != 1 {
			t.Errorf("the cleared blocker is no longer one limitation\nlimitations: %q", verification.Limitations)
		}
	})

	t.Run("not cleared by a later review the coordinator rejected", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		record = mustAssignWorker(t, record, workerBrief("review-2", "/root/reviewer-2", "review", []string{"."}, "focused"))
		record = mustSubmitWorker(t, record, workerResult(t, record, "review-2", "completed", ref))
		record = mustAcceptWorker(t, record, workerAcceptance(t, record, "review-2", "rejected", ref))
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Error("a rejected later review cleared the blocker")
		}
	})

	t.Run("not cleared by a later review that re-raises the id", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		record = mustAssignWorker(t, record, workerBrief("review-2", "/root/reviewer-2", "review", []string{"."}, "focused"))
		result := workerResult(t, record, "review-2", "completed", ref)
		// Same id, different severity and summary: still the same finding.
		result.Findings = []WorkerFinding{{ID: id, Summary: "Raised again in other words.", Severity: "minor", Evidence: []SourceRef{ref}}}
		record = mustSubmitWorker(t, record, result)
		acceptance := workerAcceptance(t, record, "review-2", "accepted", ref)
		acceptance.Findings = []FindingDisposition{severityRejected(id, ref)}
		record = mustAcceptWorker(t, record, acceptance)
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Error("a later review that re-raised the id cleared the blocker")
		}
	})

	t.Run("not cleared by an implementation result", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		record = mustAssignWorker(t, record, workerBrief("impl", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
		record = mustSubmitWorker(t, record, workerResult(t, record, "impl", "completed", ref))
		record = mustAcceptWorker(t, record, workerAcceptance(t, record, "impl", "accepted", ref))
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Error("an accepted implementation result cleared the blocker")
		}
	})
}

// The record order refuses accepting an earlier review once a later review is
// assigned, so a review whose bundle predates a rejection can never be the
// review that clears it.
func TestSeverityContractEarlierReviewAcceptanceRefusedAfterLaterAssignment(t *testing.T) {
	const id = "blocker-finding-11"
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review-1", "/root/reviewer-1", "review", []string{"."}, "focused"))
	result := workerResult(t, record, "review-1", "completed", ref)
	result.Findings = []WorkerFinding{severityFinding(id, "blocker", ref)}
	record = mustSubmitWorker(t, record, result)
	record = mustAssignWorker(t, record, workerBrief("review-2", "/root/reviewer-2", "review", []string{"."}, "focused"))

	acceptance := workerAcceptance(t, record, "review-1", "accepted", ref)
	acceptance.Findings = []FindingDisposition{severityRejected(id, ref)}
	before := productionTaskBytes(t, record.Directory)
	if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
		t.Fatal("accepting review-1 after review-2 was assigned was not refused")
	}
	after := productionTaskBytes(t, record.Directory)
	if len(before) != len(after) {
		t.Errorf("refused acceptance changed the task files: %d before, %d after", len(before), len(after))
	}
	for name, data := range before {
		if after[name] != data {
			t.Errorf("refused acceptance changed %s", name)
		}
	}
}

// severityTryReview records a review by agent that raises no finding, as far
// as the record order allows, and returns the latest record. A refusal is
// logged: a review that cannot be recorded cannot clear anything.
func severityTryReview(t *testing.T, record Record, id, agent string, ref SourceRef) Record {
	t.Helper()
	brief := workerBrief(id, agent, "review", []string{"."}, "focused")
	brief.Bundle = fixtureReviewBundle(t, record, id)
	updated, err := AssignWorker(t.Context(), record.Directory, brief)
	if err != nil {
		t.Logf("review by %s refused at assignment: %v", agent, err)
		return record
	}
	record = updated
	if updated, err = SubmitWorkerResult(t.Context(), record.Directory, workerResult(t, record, id, "completed", ref)); err != nil {
		t.Logf("review by %s refused at handoff: %v", agent, err)
		return record
	}
	record = updated
	if updated, err = AcceptWorkerResult(t.Context(), record.Directory, workerAcceptance(t, record, id, "accepted", ref)); err != nil {
		t.Logf("review by %s refused at acceptance: %v", agent, err)
		return record
	}
	return updated
}

// Clearing a rejected blocker needs an independent reviewer: a later
// accepted review by the coordinator or by an implementation worker's agent
// leaves it a problem.
func TestSeverityContractRejectedBlockerNeedsIndependentReviewer(t *testing.T) {
	t.Parallel()
	const id = "blocker-finding-13"

	t.Run("coordinator as reviewer", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		coordinator := record.Plan.CoordinatorID
		if coordinator == "" {
			t.Fatal("the plan records no coordinator")
		}
		record = severityTryReview(t, record, "review-2", coordinator, ref)
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Errorf("a review by the coordinator %s cleared the blocker", coordinator)
		}
	})

	t.Run("implementation worker as reviewer", func(t *testing.T) {
		record, ref := severityBlockerTask(t, id)
		const implementer = "/root/implementation"
		record = mustAssignWorker(t, record, workerBrief("impl", implementer, "implementation", []string{"source.txt"}, "focused"))
		record = mustSubmitWorker(t, record, workerResult(t, record, "impl", "completed", ref))
		record = mustAcceptWorker(t, record, workerAcceptance(t, record, "impl", "accepted", ref))
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Fatal("precondition: the blocker is not a problem after the implementation result")
		}
		record = severityTryReview(t, record, "review-2", implementer, ref)
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) == 0 {
			t.Errorf("a review by the implementation worker %s cleared the blocker", implementer)
		}

		// Control: in the same record, an independent reviewer clears it.
		record = severityTryReview(t, record, "review-3", "/root/reviewer-3", ref)
		if problems := severityMentions(severityVerify(t, record).Problems, id); len(problems) != 0 {
			t.Errorf("control: an independent later review left problems %q", problems)
		}
	})
}
