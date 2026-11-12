package taskflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

// Contract: cw-task-autofill-summary revision 1, requirements R1-R7.

const autofillTestWrongHash = "0000000000000000000000000000000000000000000000000000000000000000"

func autofillTestLoad(t *testing.T, dir string) Record {
	t.Helper()
	record, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

// autofillTestInspection starts a task and assigns one evidence worker "inspect" that owns source.txt.
func autofillTestInspection(t *testing.T, plan Plan) (Record, SourceRef) {
	t.Helper()
	record, ref := fixtureTask(t, plan)
	record = mustAssignWorker(t, record, workerBrief("inspect", "/root/project_explorer", "evidence", []string{"source.txt"}))
	return record, ref
}

func autofillTestFileDigest(t *testing.T, record Record, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(record.Baseline.Root, name))
	if err != nil {
		t.Fatal(err)
	}
	return digest(data)
}

func autofillTestRequireRejected(t *testing.T, label, dir string, err error, wantText string, results, acceptances int) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: invalid input was accepted", label)
	}
	if wantText != "" && !strings.Contains(err.Error(), wantText) {
		t.Fatalf("%s: error %q does not contain %q", label, err, wantText)
	}
	stored := autofillTestLoad(t, dir)
	if len(stored.WorkerResults) != results || len(stored.WorkerAcceptances) != acceptances {
		t.Fatalf("%s: rejected input changed the record: %d results and %d acceptances, want %d and %d",
			label, len(stored.WorkerResults), len(stored.WorkerAcceptances), results, acceptances)
	}
}

// autofillTestRequireBoundResult checks the R1 binding fields of the stored result for assignment id.
func autofillTestRequireBoundResult(t *testing.T, stored Record, id, agent string, snapshot projectctx.Snapshot) BoundWorkerResult {
	t.Helper()
	bound := findWorkerResult(stored, id)
	if bound == nil {
		t.Fatalf("result for %s was not stored", id)
	}
	assigned := findAssignment(stored, id)
	if assigned == nil {
		t.Fatalf("assignment %s was not stored", id)
	}
	got := bound.Result
	if got.TaskID != stored.ID || got.TaskID == "" {
		t.Fatalf("stored task_id = %q, want record ID %q", got.TaskID, stored.ID)
	}
	if got.AssignmentSHA256 != assigned.SHA256 || got.AssignmentSHA256 == "" {
		t.Fatalf("stored assignment_sha256 = %q, want %q", got.AssignmentSHA256, assigned.SHA256)
	}
	if got.AgentID != assigned.Brief.AgentID || got.AgentID != agent {
		t.Fatalf("stored agent_id = %q, want %q", got.AgentID, agent)
	}
	if got.Snapshot != snapshot || got.Snapshot.Fingerprint == "" {
		t.Fatalf("stored snapshot = %+v, want current snapshot %+v", got.Snapshot, snapshot)
	}
	return *bound
}

func autofillTestOmitResultBinding(input *WorkerResult) {
	input.TaskID = ""
	input.AssignmentSHA256 = ""
	input.AgentID = ""
	input.Snapshot = projectctx.Snapshot{}
}

// R1
func TestAutofillHandoffFillsOmittedBindingFields(t *testing.T) {
	cases := []struct {
		name string
		omit func(*WorkerResult)
	}{
		{"all", autofillTestOmitResultBinding},
		{"task_id", func(input *WorkerResult) { input.TaskID = "" }},
		{"assignment_sha256", func(input *WorkerResult) { input.AssignmentSHA256 = "" }},
		{"agent_id", func(input *WorkerResult) { input.AgentID = "" }},
		{"snapshot", func(input *WorkerResult) { input.Snapshot = projectctx.Snapshot{} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
			input := workerResult(t, record, "inspect", "completed", ref)
			tc.omit(&input)
			mustSubmitWorker(t, record, input)
			stored := autofillTestLoad(t, record.Directory)
			bound := autofillTestRequireBoundResult(t, stored, "inspect", "/root/project_explorer", currentWorkerSnapshot(t, stored))
			mustAcceptWorker(t, stored, workerAcceptance(t, stored, "inspect", "accepted", ref))
			stored = autofillTestLoad(t, record.Directory)
			if len(stored.WorkerAcceptances) != 1 || stored.WorkerAcceptances[0].Acceptance.ResultSHA256 != bound.SHA256 {
				t.Fatal("autofilled handoff was not accepted like an explicit one")
			}
		})
	}
}

// R1, and the positive control for R5.
func TestAutofillReviewHandoffWithOmittedBindingCanBeAccepted(t *testing.T) {
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review", "/root/independent-reviewer", "review", []string{"."}, "focused"))
	input := workerResult(t, record, "review", "completed", ref)
	autofillTestOmitResultBinding(&input)
	mustSubmitWorker(t, record, input)
	stored := autofillTestLoad(t, record.Directory)
	bound := autofillTestRequireBoundResult(t, stored, "review", "/root/independent-reviewer", currentWorkerSnapshot(t, stored))
	mustAcceptWorker(t, stored, workerAcceptance(t, stored, "review", "accepted", ref))
	stored = autofillTestLoad(t, record.Directory)
	if len(stored.WorkerAcceptances) != 1 || stored.WorkerAcceptances[0].Acceptance.ResultSHA256 != bound.SHA256 {
		t.Fatal("autofilled review handoff was not accepted like an explicit one")
	}
}

// R2: result, finding and acceptance evidence.
func TestAutofillFillsBlankWorkerSourceRefHashes(t *testing.T) {
	blank := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1}

	t.Run("result and finding evidence", func(t *testing.T) {
		record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
		want := autofillTestFileDigest(t, record, "source.txt")
		if want != ref.SHA256 {
			t.Fatalf("fixture digest mismatch: %s != %s", want, ref.SHA256)
		}
		input := workerResult(t, record, "inspect", "completed", blank)
		input.Findings = []WorkerFinding{{ID: "first-line", Summary: "The first line names the inspected source.", Severity: "major", Evidence: []SourceRef{blank}}}
		mustSubmitWorker(t, record, input)
		bound := findWorkerResult(autofillTestLoad(t, record.Directory), "inspect")
		if bound == nil {
			t.Fatal("result was not stored")
		}
		evidence := bound.Result.Evidence
		if len(evidence) != 1 || evidence[0].SHA256 != want || evidence[0].StartLine != 1 || evidence[0].EndLine != 1 {
			t.Fatalf("stored result evidence = %+v, want whole-file sha256 %s", evidence, want)
		}
		findings := bound.Result.Findings
		if len(findings) != 1 || len(findings[0].Evidence) != 1 || findings[0].Evidence[0].SHA256 != want {
			t.Fatalf("stored finding evidence = %+v, want whole-file sha256 %s", findings, want)
		}
	})

	t.Run("acceptance evidence", func(t *testing.T) {
		record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
		mustSubmitWorker(t, record, workerResult(t, record, "inspect", "completed", ref))
		record = autofillTestLoad(t, record.Directory)
		mustAcceptWorker(t, record, workerAcceptance(t, record, "inspect", "accepted", blank))
		stored := autofillTestLoad(t, record.Directory)
		if len(stored.WorkerAcceptances) != 1 {
			t.Fatal("acceptance was not stored")
		}
		evidence := stored.WorkerAcceptances[0].Acceptance.Evidence
		if len(evidence) != 1 || evidence[0].SHA256 != ref.SHA256 {
			t.Fatalf("stored acceptance evidence = %+v, want whole-file sha256 %s", evidence, ref.SHA256)
		}
	})
}

// R2: requirement assessment and decision evidence.
func TestAutofillFillsBlankAssessmentAndDecisionHashes(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, record.Contexts["before"], nil)); err != nil {
		t.Fatal(err)
	}
	ref := changedWorkerSource(t, record)
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	blank := ref
	blank.SHA256 = ""
	if _, err := Assess(t.Context(), record.Directory, assessment(blank, "focused")); err != nil {
		t.Fatal(err)
	}
	stored := autofillTestLoad(t, record.Directory)
	want := autofillTestFileDigest(t, stored, "source.txt")
	if want != ref.SHA256 {
		t.Fatalf("fixture digest mismatch: %s != %s", want, ref.SHA256)
	}
	bound, ok := stored.Assessments["behavior"]
	if !ok || len(bound.Assessment.Evidence) != 1 || bound.Assessment.Evidence[0].SHA256 != want {
		t.Fatalf("stored assessment evidence = %+v, want sha256 %s", bound.Assessment.Evidence, want)
	}
	if stored.Decision == nil || len(stored.Decision.Decision.Evidence) != 1 || stored.Decision.Decision.Evidence[0].SHA256 != want {
		t.Fatalf("stored decision = %+v, want evidence sha256 %s", stored.Decision, want)
	}
}

// R2 and R3: changed-file hashes.
func TestAutofillChangedFileHash(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementer", "implementation", []string{"source.txt"}))
	ref := changedWorkerSource(t, record)
	input := workerResult(t, record, "implement", "completed", ref)

	input.ChangedFiles = []WorkerChangedFile{{Path: "source.txt", SHA256: autofillTestWrongHash}}
	_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
	autofillTestRequireRejected(t, "wrong explicit changed-file sha256", record.Directory, err, "", 0, 0)

	input.ChangedFiles = []WorkerChangedFile{{Path: "source.txt"}}
	mustSubmitWorker(t, record, input)
	stored := autofillTestLoad(t, record.Directory)
	bound := findWorkerResult(stored, "implement")
	if bound == nil {
		t.Fatal("result was not stored")
	}
	want := autofillTestFileDigest(t, stored, "source.txt")
	files := bound.Result.ChangedFiles
	if len(files) != 1 || files[0].Path != "source.txt" || files[0].Deleted || files[0].SHA256 != want || want != ref.SHA256 {
		t.Fatalf("stored changed files = %+v, want source.txt with sha256 %s", files, want)
	}
}

// R4
func TestAutofillRejectsBlankHashForMissingChangedFile(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	notes := []byte("notes about the removed source\n")
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "notes.txt"), notes, 0600); err != nil {
		t.Fatal(err)
	}
	evidence := SourceRef{Path: "notes.txt", StartLine: 1, EndLine: 1, SHA256: digest(notes)}
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementer", "implementation", []string{"source.txt"}))
	if err := os.Remove(filepath.Join(record.Baseline.Root, "source.txt")); err != nil {
		t.Fatal(err)
	}
	input := workerResult(t, record, "implement", "completed", evidence)

	input.ChangedFiles = []WorkerChangedFile{{Path: "source.txt"}}
	_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
	autofillTestRequireRejected(t, "absent changed file without deleted", record.Directory, err, "deleted: true", 0, 0)

	input.ChangedFiles = []WorkerChangedFile{{Path: "source.txt", Deleted: true}}
	mustSubmitWorker(t, record, input)
	bound := findWorkerResult(autofillTestLoad(t, record.Directory), "implement")
	if bound == nil || len(bound.Result.ChangedFiles) != 1 || !bound.Result.ChangedFiles[0].Deleted {
		t.Fatal("deleted changed file was not stored")
	}
}

// R5
func TestAutofillReviewRejectsOmittedSnapshotAfterSourceChanged(t *testing.T) {
	record, _ := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review", "/root/independent-reviewer", "review", []string{"."}, "focused"))
	data := []byte("source changed after the review assignment\n")
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	ref := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: digest(data)}
	input := workerResult(t, record, "review", "completed", ref)
	input.Snapshot = projectctx.Snapshot{}
	_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
	autofillTestRequireRejected(t, "review after source change", record.Directory, err, "review source changed", 0, 0)
}

// R3: results and acceptances.
func TestAutofillStillValidatesExplicitBindingValues(t *testing.T) {
	record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
	wrongRef := ref
	wrongRef.SHA256 = autofillTestWrongHash

	resultCases := []struct {
		name   string
		mutate func(*WorkerResult)
	}{
		{"agent_id", func(input *WorkerResult) { input.AgentID = "/root/different_agent" }},
		{"task_id", func(input *WorkerResult) { input.TaskID = "different-task" }},
		{"assignment_sha256", func(input *WorkerResult) { input.AssignmentSHA256 = autofillTestWrongHash }},
		{"snapshot", func(input *WorkerResult) { input.Snapshot.Fingerprint = "sha256:" + autofillTestWrongHash }},
		{"evidence sha256", func(input *WorkerResult) { input.Evidence = []SourceRef{wrongRef} }},
		{"finding evidence sha256", func(input *WorkerResult) {
			input.Findings = []WorkerFinding{{ID: "first-line", Summary: "The first line names the inspected source.", Severity: "major", Evidence: []SourceRef{wrongRef}}}
		}},
	}
	for _, tc := range resultCases {
		input := workerResult(t, record, "inspect", "completed", ref)
		tc.mutate(&input)
		_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
		autofillTestRequireRejected(t, "result with wrong explicit "+tc.name, record.Directory, err, "", 0, 0)
	}
	mustSubmitWorker(t, record, workerResult(t, record, "inspect", "completed", ref))
	record = autofillTestLoad(t, record.Directory)

	acceptanceCases := []struct {
		name   string
		mutate func(*WorkerAcceptance)
	}{
		{"result_sha256", func(input *WorkerAcceptance) { input.ResultSHA256 = autofillTestWrongHash }},
		{"coordinator_id", func(input *WorkerAcceptance) { input.CoordinatorID = "/root/different_coordinator" }},
		{"evidence sha256", func(input *WorkerAcceptance) { input.Evidence = []SourceRef{wrongRef} }},
	}
	for _, tc := range acceptanceCases {
		input := workerAcceptance(t, record, "inspect", "accepted", ref)
		tc.mutate(&input)
		_, err := AcceptWorkerResult(t.Context(), record.Directory, input)
		autofillTestRequireRejected(t, "acceptance with wrong explicit "+tc.name, record.Directory, err, "", 1, 0)
	}
	mustAcceptWorker(t, record, workerAcceptance(t, record, "inspect", "accepted", ref))
}

// R3: requirement assessment and decision evidence.
func TestAutofillStillValidatesExplicitAssessmentHashes(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, record.Contexts["before"], nil)); err != nil {
		t.Fatal(err)
	}
	ref := changedWorkerSource(t, record)
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	before := autofillTestLoad(t, record.Directory)
	wrongRef := ref
	wrongRef.SHA256 = autofillTestWrongHash

	wrongRequirement := assessment(ref, "focused")
	wrongRequirement.Requirements[0].Evidence = []SourceRef{wrongRef}
	wrongDecision := assessment(ref, "focused")
	wrongDecision.Decision.Evidence = []SourceRef{wrongRef}
	for name, input := range map[string]AssessmentInput{"requirement": wrongRequirement, "decision": wrongDecision} {
		if _, err := Assess(t.Context(), record.Directory, input); err == nil {
			t.Fatalf("%s evidence with a wrong explicit sha256 was accepted", name)
		}
		stored := autofillTestLoad(t, record.Directory)
		if len(stored.Assessments) != len(before.Assessments) || (stored.Decision == nil) != (before.Decision == nil) {
			t.Fatalf("rejected %s assessment changed the record", name)
		}
	}
	if _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); err != nil {
		t.Fatalf("correct explicit sha256 was rejected: %v", err)
	}
}

// R6
func TestAutofillAcceptanceFillsOmittedBindingFields(t *testing.T) {
	omitAll := func(input *WorkerAcceptance) {
		input.TaskID = ""
		input.CoordinatorID = ""
		input.ResultSHA256 = ""
	}
	cases := []struct {
		name        string
		coordinator string
		want        string
		omit        func(*WorkerAcceptance)
	}{
		{"all with default coordinator", "", "/root", omitAll},
		{"all with plan coordinator", "/root/lead", "/root/lead", omitAll},
		{"task_id", "", "/root", func(input *WorkerAcceptance) { input.TaskID = "" }},
		{"coordinator_id", "/root/lead", "/root/lead", func(input *WorkerAcceptance) { input.CoordinatorID = "" }},
		{"result_sha256", "", "/root", func(input *WorkerAcceptance) { input.ResultSHA256 = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := helperPlan(t, "small", "pass")
			plan.CoordinatorID = tc.coordinator
			record, ref := autofillTestInspection(t, plan)
			mustSubmitWorker(t, record, workerResult(t, record, "inspect", "completed", ref))
			record = autofillTestLoad(t, record.Directory)
			input := workerAcceptance(t, record, "inspect", "accepted", ref)
			input.CoordinatorID = tc.want
			tc.omit(&input)
			mustAcceptWorker(t, record, input)
			stored := autofillTestLoad(t, record.Directory)
			result := findWorkerResult(stored, "inspect")
			if result == nil || len(stored.WorkerAcceptances) != 1 {
				t.Fatal("acceptance was not stored")
			}
			got := stored.WorkerAcceptances[0].Acceptance
			if got.TaskID != stored.ID || got.TaskID == "" {
				t.Fatalf("stored task_id = %q, want %q", got.TaskID, stored.ID)
			}
			if got.CoordinatorID != tc.want {
				t.Fatalf("stored coordinator_id = %q, want %q", got.CoordinatorID, tc.want)
			}
			if got.ResultSHA256 != result.SHA256 || got.ResultSHA256 == "" {
				t.Fatalf("stored result_sha256 = %q, want %q", got.ResultSHA256, result.SHA256)
			}
		})
	}
}

// R7
func TestAutofillDoesNotDefaultOmittedCollections(t *testing.T) {
	record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
	resultCases := []struct {
		name   string
		mutate func(*WorkerResult)
	}{
		{"evidence", func(input *WorkerResult) { input.Evidence = nil }},
		{"changed_files", func(input *WorkerResult) { input.ChangedFiles = nil }},
		{"check_ids", func(input *WorkerResult) { input.CheckIDs = nil }},
		{"findings", func(input *WorkerResult) { input.Findings = nil }},
		{"uncertainties", func(input *WorkerResult) { input.Uncertainties = nil }},
	}
	for _, tc := range resultCases {
		input := workerResult(t, record, "inspect", "completed", ref)
		tc.mutate(&input)
		_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
		autofillTestRequireRejected(t, "result without "+tc.name, record.Directory, err, "explicit arrays", 0, 0)
	}
	mustSubmitWorker(t, record, workerResult(t, record, "inspect", "completed", ref))
	record = autofillTestLoad(t, record.Directory)

	acceptanceCases := []struct {
		name   string
		mutate func(*WorkerAcceptance)
	}{
		{"evidence", func(input *WorkerAcceptance) { input.Evidence = nil }},
		{"check_ids", func(input *WorkerAcceptance) { input.CheckIDs = nil }},
		{"findings", func(input *WorkerAcceptance) { input.Findings = nil }},
	}
	for _, tc := range acceptanceCases {
		input := workerAcceptance(t, record, "inspect", "accepted", ref)
		tc.mutate(&input)
		_, err := AcceptWorkerResult(t.Context(), record.Directory, input)
		autofillTestRequireRejected(t, "acceptance without "+tc.name, record.Directory, err, "explicit arrays", 1, 0)
	}
}

// Contract revision 2 additions.

const autofillTestReviewGate = "requires an accepted completed independent review"

func autofillTestReviewGateProblems(t *testing.T, dir string) []string {
	t.Helper()
	verification, _ := Verify(t.Context(), dir)
	var matches []string
	for _, problem := range verification.Problems {
		if strings.Contains(problem, autofillTestReviewGate) {
			matches = append(matches, problem)
		}
	}
	return matches
}

// R1: an autofilled review satisfies the same completion gate as an explicit one.
func TestAutofillReviewSatisfiesCompletionGateLikeExplicitReview(t *testing.T) {
	for _, explicit := range []bool{true, false} {
		name := "autofilled"
		if explicit {
			name = "explicit"
		}
		t.Run(name, func(t *testing.T) {
			record, ref := preparedChangedTask(t)
			if _, err := Verify(t.Context(), record.Directory); err == nil {
				t.Fatal("task without a review verified as complete")
			}
			if len(autofillTestReviewGateProblems(t, record.Directory)) == 0 {
				t.Fatalf("missing review was not reported as %q", autofillTestReviewGate)
			}
			if explicit {
				completeIndependentReview(t, record, ref, "review")
			} else {
				record = mustAssignWorker(t, record, workerBrief("review", "/root/independent-reviewer", "review", []string{"."}, "focused"))
				blank := ref
				blank.SHA256 = ""
				input := workerResult(t, record, "review", "completed", blank)
				autofillTestOmitResultBinding(&input)
				mustSubmitWorker(t, record, input)
				record = autofillTestLoad(t, record.Directory)
				acceptance := workerAcceptance(t, record, "review", "accepted", blank)
				acceptance.TaskID, acceptance.CoordinatorID, acceptance.ResultSHA256 = "", "", ""
				mustAcceptWorker(t, record, acceptance)
			}
			if problems := autofillTestReviewGateProblems(t, record.Directory); len(problems) != 0 {
				t.Fatalf("accepted %s review still fails the review gate: %v", name, problems)
			}
		})
	}
}

// R2 and R3: finding disposition evidence.
func TestAutofillFindingDispositionEvidenceHash(t *testing.T) {
	record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
	input := workerResult(t, record, "inspect", "completed", ref)
	input.Findings = []WorkerFinding{{ID: "first-line", Summary: "The first line names the inspected source.", Severity: "major", Evidence: []SourceRef{ref}}}
	mustSubmitWorker(t, record, input)
	record = autofillTestLoad(t, record.Directory)
	disposition := func(sha string) []FindingDisposition {
		evidence := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 1, SHA256: sha}
		return []FindingDisposition{{FindingID: "first-line", Status: "resolved", Reason: "The first line was checked against the source.", Evidence: []SourceRef{evidence}}}
	}

	wrong := workerAcceptance(t, record, "inspect", "accepted", ref)
	wrong.Findings = disposition(autofillTestWrongHash)
	_, err := AcceptWorkerResult(t.Context(), record.Directory, wrong)
	autofillTestRequireRejected(t, "disposition with wrong explicit sha256", record.Directory, err, "", 1, 0)

	blank := workerAcceptance(t, record, "inspect", "accepted", ref)
	blank.Findings = disposition("")
	mustAcceptWorker(t, record, blank)
	stored := autofillTestLoad(t, record.Directory)
	if len(stored.WorkerAcceptances) != 1 {
		t.Fatal("acceptance was not stored")
	}
	findings := stored.WorkerAcceptances[0].Acceptance.Findings
	want := autofillTestFileDigest(t, stored, "source.txt")
	if len(findings) != 1 || findings[0].FindingID != "first-line" || len(findings[0].Evidence) != 1 ||
		findings[0].Evidence[0].SHA256 != want || want != ref.SHA256 {
		t.Fatalf("stored dispositions = %+v, want evidence sha256 %s", findings, want)
	}
}

// R1 with planned checks: the stored result binds the passed run at the current source.
func TestAutofillHandoffBindsPlannedCheckRuns(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	record = mustAssignWorker(t, record, workerBrief("inspect", "/root/project_explorer", "evidence", []string{"source.txt"}, "focused"))
	run, err := RunCheck(t.Context(), record.Directory, "focused")
	if err != nil {
		t.Fatal(err)
	}
	input := workerResult(t, record, "inspect", "completed", ref)
	if len(input.CheckIDs) != 1 || input.CheckIDs[0] != "focused" {
		t.Fatalf("result check_ids = %v, want [focused]", input.CheckIDs)
	}
	autofillTestOmitResultBinding(&input)
	mustSubmitWorker(t, record, input)
	stored := autofillTestLoad(t, record.Directory)
	bound := autofillTestRequireBoundResult(t, stored, "inspect", "/root/project_explorer", currentWorkerSnapshot(t, stored))
	if len(bound.CheckRuns) != 1 || bound.CheckRuns[0] != run.ID || run.ID == "" {
		t.Fatalf("stored check runs = %v, want [%s]", bound.CheckRuns, run.ID)
	}
}
