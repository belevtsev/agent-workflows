package taskflow

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
)

func workerBrief(id, agent, role string, owned []string, checks ...string) WorkerBrief {
	if checks == nil {
		checks = []string{}
	}
	return WorkerBrief{ID: id, AgentID: agent, Role: role, Goal: "Complete the bounded assigned work.", OwnedPaths: owned, Constraints: []string{"Preserve other workers' edits."}, CheckIDs: checks, StopCondition: "Return a terminal source-bound handoff with actual evidence."}
}

// mustAssignWorker assigns the brief. A review brief without a bundle in a
// record that has a baseline manifest gets a generated bundle whose facets
// are all "n/a: test fixture" (see fixtureReviewBundle in bundle_test.go).
func mustAssignWorker(t *testing.T, record Record, brief WorkerBrief) Record {
	t.Helper()
	if brief.Role == "review" && brief.Bundle == nil && record.BaselineManifest != nil {
		brief.Bundle = fixtureReviewBundle(t, record, brief.ID)
	}
	updated, err := AssignWorker(t.Context(), record.Directory, brief)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func currentWorkerSnapshot(t *testing.T, record Record) projectctx.Snapshot {
	t.Helper()
	snapshot, err := projectctx.Capture(t.Context(), record.Baseline.SelectedPath)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func workerResult(t *testing.T, record Record, id, status string, refs ...SourceRef) WorkerResult {
	t.Helper()
	if refs == nil {
		refs = []SourceRef{}
	}
	assigned := findAssignment(record, id)
	if assigned == nil {
		t.Fatal("assignment missing")
	}
	result := WorkerResult{TaskID: record.ID, AssignmentID: id, AssignmentSHA256: assigned.SHA256, AgentID: assigned.Brief.AgentID, Snapshot: currentWorkerSnapshot(t, record), Status: status, Summary: "Inspected the assigned source and actual task evidence.", Evidence: refs, CheckIDs: assigned.Brief.CheckIDs, ChangedFiles: []WorkerChangedFile{}, Findings: []WorkerFinding{}, Uncertainties: []string{}}
	// RR5: a completed review of an assignment with a stored bundle answers
	// every non-deleted reach row with one fixture "ran" entry.
	if assigned.Brief.Role == "review" && assigned.BundleFile != "" && status == "completed" {
		root, err := os.OpenRoot(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		bundle, err := readStoredBundle(root, *assigned)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range bundle.Reach {
			if row.Deleted {
				continue
			}
			result.ReachRuns = append(result.ReachRuns, ReachRun{Path: row.Path, Mode: "test fixture mode", Status: "ran", Command: "fixture command", Observed: "fixture observed output"})
		}
	}
	return result
}

func workerAcceptance(t *testing.T, record Record, id, status string, refs ...SourceRef) WorkerAcceptance {
	t.Helper()
	if refs == nil {
		refs = []SourceRef{}
	}
	result := findWorkerResult(record, id)
	if result == nil {
		t.Fatal("result missing")
	}
	return WorkerAcceptance{TaskID: record.ID, AssignmentID: id, ResultSHA256: result.SHA256, CoordinatorID: record.Plan.CoordinatorID, Status: status, Reason: "Coordinator checked the declared scope and the retained source/check evidence.", Evidence: refs, CheckIDs: result.Result.CheckIDs, Findings: []FindingDisposition{}}
}

func mustSubmitWorker(t *testing.T, record Record, input WorkerResult) Record {
	t.Helper()
	updated, err := SubmitWorkerResult(t.Context(), record.Directory, input)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func mustAcceptWorker(t *testing.T, record Record, input WorkerAcceptance) Record {
	t.Helper()
	updated, err := AcceptWorkerResult(t.Context(), record.Directory, input)
	if err != nil {
		t.Fatal(err)
	}
	return updated
}

func changedWorkerSource(t *testing.T, record Record) SourceRef {
	t.Helper()
	data := []byte("changed first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(data)}
}

func preparedChangedTask(t *testing.T) (Record, SourceRef) {
	t.Helper()
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, record.Contexts["before"], nil)); err != nil {
		t.Fatal(err)
	}
	ref := changedWorkerSource(t, record)
	mustAssess(t, record, ref)
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return record, ref
}

func completeIndependentReview(t *testing.T, record Record, ref SourceRef, id string) Record {
	t.Helper()
	record = mustAssignWorker(t, record, workerBrief(id, "/root/independent-reviewer", "review", []string{"."}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, id, "completed", ref))
	return mustAcceptWorker(t, record, workerAcceptance(t, record, id, "accepted", ref))
}

func TestWorkerLifecycleBindsImmutableBriefResultAndCoordinatorAcceptance(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "substantial", "pass"))
	if Version != 3 || record.Version != Version || record.Plan.CoordinatorID != "/root" {
		t.Fatalf("new task version/default: %+v", record.Plan)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "before", writeConsultation(t, record.Contexts["before"], nil)); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("implement", "/root/implementation", "implementation", []string{"source.txt"}, "focused"))
	ref := changedWorkerSource(t, record)
	mustAssess(t, record, ref)
	result := workerResult(t, record, "implement", "completed", ref)
	result.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	record = mustSubmitWorker(t, record, result)
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "acceptance") {
		t.Fatalf("unaccepted work passed final context: %v", err)
	}
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "implement", "accepted", ref))
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "independent review") {
		t.Fatalf("missing independent review passed: %v", err)
	}
	record = completeIndependentReview(t, record, ref, "review")
	final, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, final, nil)); err != nil {
		t.Fatal(err)
	}
	if result, err := Verify(t.Context(), record.Directory); err != nil || !result.Complete {
		t.Fatalf("completed lifecycle: %+v %v", result, err)
	}
	stored, err := Load(record.Directory)
	if err != nil || len(stored.Assignments) != 2 || len(stored.WorkerResults) != 2 || len(stored.WorkerAcceptances) != 2 {
		t.Fatalf("worker history: %+v %v", stored, err)
	}
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, result); err == nil {
		t.Fatal("terminal result rewritten")
	}
	if _, err := AssignWorker(t.Context(), record.Directory, record.Assignments[0].Brief); err == nil {
		t.Fatal("immutable assignment reused")
	}
	if _, err := Run(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	if _, err := Assess(t.Context(), record.Directory, assessment(ref, "focused")); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "independent review") {
		t.Fatalf("old review survived new check evidence: %v", err)
	}
	if verified, err := Verify(t.Context(), record.Directory); err == nil || verified.Complete {
		t.Fatal("final consultation survived worker/check evidence mutation")
	}
	record, err = Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	record = completeIndependentReview(t, record, ref, "fresh_review")
	refreshed, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil || refreshed.EvidenceSHA256 == final.EvidenceSHA256 {
		t.Fatalf("fresh review binding: %+v %v", refreshed, err)
	}
}

func TestWorkerOwnershipAndInputValidation(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	for _, bad := range []string{"../outside", "/absolute", "dir/../source.txt", "dir\\source.txt", ".git/config", "source.txt/", "C:/outside"} {
		if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("invalid", "/root/writer", "implementation", []string{bad})); err == nil {
			t.Errorf("unsafe scope accepted: %s", bad)
		}
	}
	for _, bad := range []string{"", " ", "/root\nwriter", strings.Repeat("a", 257)} {
		if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("invalid", bad, "implementation", []string{"source.txt"})); err == nil {
			t.Errorf("invalid declared identity accepted: %q", bad)
		}
	}
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("invalid", "/root/writer", "implementation", []string{"dir", "dir/file"})); err == nil {
		t.Fatal("overlap within brief accepted")
	}
	if _, err := AssignWorker(t.Context(), record.Directory, workerBrief("invalid", "/root/writer", "implementation", []string{"source.txt"}, "unplanned")); err == nil {
		t.Fatal("unplanned check accepted")
	}
	record = mustAssignWorker(t, record, workerBrief("writer", "/root/writer", "implementation", []string{"source.txt"}, "focused"))
	for i, scope := range []string{"source.txt", ".", "source.txt/nested"} {
		if _, err := AssignWorker(t.Context(), record.Directory, workerBrief(fmt.Sprintf("overlap%d", i), "/root/other", "implementation", []string{scope})); err == nil {
			t.Errorf("active writer overlap accepted: %s", scope)
		}
	}
	record = mustAssignWorker(t, record, workerBrief("sibling", "/root/other", "implementation", []string{"other.txt"}))
	record = mustAssignWorker(t, record, workerBrief("reader", "/root/reader", "evidence", []string{"source.txt"}))
	mustAssess(t, record, ref)
	valid := workerResult(t, record, "writer", "completed", ref)
	tests := map[string]func(*WorkerResult){
		"wrong task":          func(v *WorkerResult) { v.TaskID = strings.Repeat("a", 32) },
		"wrong assignment":    func(v *WorkerResult) { v.AssignmentID = "missing" },
		"wrong brief hash":    func(v *WorkerResult) { v.AssignmentSHA256 = strings.Repeat("a", 64) },
		"wrong worker":        func(v *WorkerResult) { v.AgentID = "/root/intruder" },
		"wrong source":        func(v *WorkerResult) { v.Snapshot.Fingerprint = "sha256:" + strings.Repeat("b", 64) },
		"nonterminal":         func(v *WorkerResult) { v.Status = "running" },
		"unplanned check":     func(v *WorkerResult) { v.CheckIDs = []string{"unplanned"} },
		"missing brief check": func(v *WorkerResult) { v.CheckIDs = nil },
		"outside edit":        func(v *WorkerResult) { v.ChangedFiles = []WorkerChangedFile{{Path: "other.txt", SHA256: ref.SHA256}} },
		"traversal edit": func(v *WorkerResult) {
			v.ChangedFiles = []WorkerChangedFile{{Path: "../source.txt", SHA256: ref.SHA256}}
		},
		"false edit hash": func(v *WorkerResult) {
			v.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: strings.Repeat("b", 64)}}
		},
		"false deletion": func(v *WorkerResult) { v.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, Deleted: true}} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := SubmitWorkerResult(t.Context(), record.Directory, input); err == nil {
				t.Fatal("invalid result accepted")
			}
		})
	}
	readonly := workerResult(t, record, "reader", "completed", ref)
	readonly.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, readonly); err == nil {
		t.Fatal("read-only edit authority accepted")
	}
	for _, raw := range []string{`{"id":"a","id":"b"}`, `{"id":"a","edit_authority":true}`} {
		var brief WorkerBrief
		if Decode(strings.NewReader(raw), &brief) == nil {
			t.Fatal("non-strict brief JSON accepted")
		}
	}
}

func TestBlockedCancelledAndUnusedHandoffsNeverClaimCompletion(t *testing.T) {
	for _, status := range []string{"blocked", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			record, ref := fixtureTask(t, helperPlan(t, "small", "fail"))
			record = mustAssignWorker(t, record, workerBrief("worker", "/root/worker", "implementation", []string{"source.txt"}, "focused"))
			if verified, err := Verify(t.Context(), record.Directory); err == nil || !strings.Contains(strings.Join(verified.Problems, " "), "no terminal handoff") {
				t.Fatal("orphan assignment did not block completion")
			}
			run, err := Run(t.Context(), record.Directory, "focused")
			if err == nil || run.Status != "failed" {
				t.Fatal("failure fixture did not fail")
			}
			input := workerResult(t, record, "worker", "completed", ref)
			if _, err := SubmitWorkerResult(t.Context(), record.Directory, input); err == nil {
				t.Fatal("failed check claimed completed work")
			}
			input.Status = status
			record = mustSubmitWorker(t, record, input)
			if findWorkerResult(record, "worker").CheckRuns[0] != run.ID || record.Checks[0].Status != "failed" {
				t.Fatal("actual failure lost")
			}
			acceptance := workerAcceptance(t, record, "worker", "accepted", ref)
			if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
				t.Fatal("incomplete result accepted as completion")
			}
			acceptance.Status, acceptance.CheckIDs, acceptance.Evidence, acceptance.Reason = "unused", []string{}, []SourceRef{}, ""
			if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
				t.Fatal("unused result without rationale accepted")
			}
			acceptance.Reason = "The worker stopped without usable completion evidence; coordinator will reassign the work."
			record = mustAcceptWorker(t, record, acceptance)
			record = mustAssignWorker(t, record, workerBrief("replacement", "/root/replacement", "implementation", []string{"source.txt"}))
			if verified, err := Verify(t.Context(), record.Directory); err == nil || verified.Complete {
				t.Fatal("cancelled/blocked work supplied missing completion evidence")
			}
		})
	}
}

func TestCancelledUnusedHandoffCanCloseWithoutACompletionClaim(t *testing.T) {
	plan := Plan{Title: "Read source", Kind: "small", Requirements: []Requirement{{ID: "behavior", Description: "Source inspection", AllowNonExecution: true}}}
	record, ref := fixtureTask(t, plan)
	input := assessment(ref)
	input.Requirements[0].NonExecutionReason, input.Decision.NonExecutionReason = "Read-only inspection.", "Read-only inspection."
	if _, err := Assess(t.Context(), record.Directory, input); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("unused", "/root/reader", "evidence", []string{"."}))
	record = mustSubmitWorker(t, record, workerResult(t, record, "unused", "cancelled"))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "unused", "unused"))
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("explicitly unused cancellation did not close: %+v %v", verified, err)
	}
}

func TestIndependentReviewCannotBeRelabeledForNewerSource(t *testing.T) {
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "later.sql"), []byte("SELECT 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, workerResult(t, record, "review", "completed", ref)); err == nil || !strings.Contains(err.Error(), "since its immutable assignment") {
		t.Fatalf("review relabeled with newer source: %v", err)
	}
	cancelled := workerResult(t, record, "review", "cancelled")
	cancelled.CheckIDs = []string{}
	record = mustSubmitWorker(t, record, cancelled)
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "review", "unused"))
	record = completeIndependentReview(t, record, ref, "replacement_review")
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err != nil {
		t.Fatalf("cancelled stale review could not be replaced: %v", err)
	}
}

func TestReviewDependenciesRejectLaterWorkerMutations(t *testing.T) {
	record, ref := preparedChangedTask(t)
	record = completeIndependentReview(t, record, ref, "review")
	first, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AttachConsultation(t.Context(), record.Directory, "final", writeConsultation(t, first, nil)); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("late_evidence", "/root/evidence", "evidence", []string{"source.txt"}))
	record = mustSubmitWorker(t, record, workerResult(t, record, "late_evidence", "cancelled"))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "late_evidence", "unused"))
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "independent review") {
		t.Fatalf("new worker state reused old review: %v", err)
	}
	if verified, err := Verify(t.Context(), record.Directory); err == nil || verified.Complete {
		t.Fatal("new worker state reused old final consultation")
	}
	record = completeIndependentReview(t, record, ref, "fresh_review")
	second, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil || second.EvidenceSHA256 == first.EvidenceSHA256 {
		t.Fatalf("fresh worker review did not refresh final binding: %+v %v", second, err)
	}
}

func TestUnusedObsoleteReviewCanCloseWithoutInventingFindingResolution(t *testing.T) {
	record, ref := preparedChangedTask(t)
	if findingSourceObsolete(record.Baseline, []SourceRef{ref}) {
		t.Fatal("unchanged finding source was treated as obsolete")
	}
	unavailable := record.Baseline
	unavailable.Root = filepath.Join(t.TempDir(), "unavailable")
	if findingSourceObsolete(unavailable, []SourceRef{ref}) {
		t.Fatal("unavailable source was treated as obsolete")
	}
	record = mustAssignWorker(t, record, workerBrief("review", "/root/reviewer", "review", []string{"."}, "focused"))
	result := workerResult(t, record, "review", "completed", ref)
	result.Findings = []WorkerFinding{{ID: "old_concern", Summary: "Finding tied to these source bytes.", Severity: "major", Evidence: []SourceRef{ref}}}
	record = mustSubmitWorker(t, record, result)
	acceptance := workerAcceptance(t, record, "review", "unused")
	acceptance.CheckIDs = []string{}
	acceptance.Findings = []FindingDisposition{{FindingID: "old_concern", Status: "accepted", Reason: "Acknowledged pending source correction.", Evidence: []SourceRef{}}}
	record = mustAcceptWorker(t, record, acceptance)
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("unused label hid a finding at current source: %v", err)
	}
	data := []byte("source revised after the obsolete finding\nsecond line\n")
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, ref.Path), data, 0600); err != nil {
		t.Fatal(err)
	}
	ref.SHA256 = digest(data)
	mustAssess(t, record, ref)
	acceptance.Reason = "The original source bytes have been replaced; retire this obsolete review and require a fresh independent review."
	record = mustAcceptWorker(t, record, acceptance)
	record = completeIndependentReview(t, record, ref, "fresh_review")
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err != nil {
		t.Fatalf("explicit obsolete finding retirement blocked fresh review: %v", err)
	}
}

func TestReviewRequiresIndependentIdentityFullScopeAndResolvedFindings(t *testing.T) {
	record, ref := preparedChangedTask(t)
	record = mustAssignWorker(t, record, workerBrief("implementation", "/root/shared", "implementation", []string{"source.txt"}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, "implementation", "completed", ref))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "implementation", "accepted", ref))
	for _, test := range []struct {
		id, agent string
		owned     []string
	}{{"coordinator_review", "/root", []string{"."}}, {"self_review", "/root/shared", []string{"."}}, {"partial_review", "/root/independent", []string{"source.txt"}}} {
		record = mustAssignWorker(t, record, workerBrief(test.id, test.agent, "review", test.owned, "focused"))
		record = mustSubmitWorker(t, record, workerResult(t, record, test.id, "completed", ref))
		record = mustAcceptWorker(t, record, workerAcceptance(t, record, test.id, "accepted", ref))
		if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil {
			t.Fatalf("invalid mandatory review satisfied gate: %s", test.id)
		}
	}
	record = mustAssignWorker(t, record, workerBrief("independent", "/root/reviewer", "review", []string{"."}, "focused"))
	result := workerResult(t, record, "independent", "completed", ref)
	result.Findings = []WorkerFinding{{ID: "concern", Summary: "Potential source behavior needs adjudication.", Severity: "major", Evidence: []SourceRef{ref}}}
	record = mustSubmitWorker(t, record, result)
	acceptance := workerAcceptance(t, record, "independent", "accepted", ref)
	if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
		t.Fatal("review finding silently omitted")
	}
	acceptance.Findings = []FindingDisposition{{FindingID: "concern", Status: "rejected", Reason: "Source contradicts this finding.", Evidence: []SourceRef{}}}
	if _, err := AcceptWorkerResult(t.Context(), record.Directory, acceptance); err == nil {
		t.Fatal("finding rejected without evidence")
	}
	acceptance.Findings[0].Status = "accepted"
	record = mustAcceptWorker(t, record, acceptance)
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "unresolved") {
		t.Fatalf("acknowledged unresolved finding passed: %v", err)
	}
	acceptance.Findings[0].Status, acceptance.Findings[0].Evidence = "rejected", []SourceRef{ref}
	record = mustAcceptWorker(t, record, acceptance)
	if _, err := PrepareContext(t.Context(), record.Directory, "final"); err != nil {
		t.Fatalf("coordinator source-backed rejection was vetoed: %v", err)
	}
}

func TestImplementationHandoffSurvivesUnrelatedNonGoSourceChanges(t *testing.T) {
	plan := helperPlan(t, "small", "pass")
	repo := t.TempDir()
	data := []byte("<h1>Before</h1>\n")
	if err := os.WriteFile(filepath.Join(repo, "index.html"), data, 0600); err != nil {
		t.Fatal(err)
	}
	record, err := Start(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), plan, fixtureBinding())
	if err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("html", "/root/web", "implementation", []string{"index.html"}, "focused"))
	data = []byte("<h1>After</h1>\n")
	if err := os.WriteFile(filepath.Join(repo, "index.html"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ref := SourceRef{Path: "index.html", StartLine: 1, EndLine: 1, SHA256: digest(data)}
	mustAssess(t, record, ref)
	input := workerResult(t, record, "html", "completed", ref)
	input.ChangedFiles = []WorkerChangedFile{{Path: ref.Path, SHA256: ref.SHA256}}
	record = mustSubmitWorker(t, record, input)
	if err := os.WriteFile(filepath.Join(repo, "query.sql"), []byte("SELECT 1;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	// Acceptance itself may follow unrelated changes; the original actual
	// check run and scoped bytes remain recorded rather than relabeled.
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "html", "accepted", ref))
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("unrelated source invalidated bounded writer evidence: %+v %v", verified, err)
	}
	data = []byte("<h1>Later edit</h1>\n")
	if err := os.WriteFile(filepath.Join(repo, "index.html"), data, 0600); err != nil {
		t.Fatal(err)
	}
	ref.SHA256 = digest(data)
	mustAssess(t, record, ref)
	if verified, err := Verify(t.Context(), record.Directory); err == nil || !strings.Contains(strings.Join(verified.Problems, " "), "ownership scope is stale") {
		t.Fatalf("related edit did not invalidate handoff: %+v %v", verified, err)
	}
}

func TestWorkerDeletionAndRetainedArtifactBindings(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "removed.txt"), []byte("delete\n"), 0600); err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("delete", "/root/delete", "implementation", []string{"removed.txt"}, "focused"))
	if err := os.Remove(filepath.Join(record.Baseline.Root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	mustAssess(t, record, ref)
	input := workerResult(t, record, "delete", "completed", ref)
	input.ChangedFiles = []WorkerChangedFile{{Path: "removed.txt", Deleted: true}}
	record = mustSubmitWorker(t, record, input)
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "delete", "accepted", ref))
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("valid explicit deletion failed: %+v %v", verified, err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, record.Checks[0].Log), []byte("tampered output"), 0600); err != nil {
		t.Fatal(err)
	}
	if verified, err := Verify(t.Context(), record.Directory); err == nil || verified.Complete {
		t.Fatal("retained worker check artifact tampering passed")
	}
}

func appendWorkerRevision(t *testing.T, record Record, mutate func(*Record)) {
	t.Helper()
	previous, err := os.ReadFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision)))
	if err != nil {
		t.Fatal(err)
	}
	record.Revision++
	record.PreviousSHA256 = digest(previous)
	record.UpdatedAt = time.Now().UTC()
	mutate(&record)
	data, err := encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision)), data, 0600); err != nil {
		t.Fatal(err)
	}
}

// rewriteInitialRevision replaces record-000001.json in place; later revisions
// appended by appendWorkerRevision chain to the rewritten bytes.
func rewriteInitialRevision(t *testing.T, record Record, mutate func(*Record)) Record {
	t.Helper()
	mutate(&record)
	data, err := encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Directory, "record-000001.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestWorkerPersistedStateRejectsVersionSwitchesAndRewrittenHistory(t *testing.T) {
	// D5: a revision of any other version anywhere in a chain is refused with
	// the D5 message naming that version, not a generic version-switch error.
	for _, test := range []struct {
		name          string
		first, second int
	}{{"v1_then_v3", 1, Version}, {"v2_then_v3", 2, Version}, {"v3_then_v2", Version, 2}, {"v3_then_v4", Version, Version + 1}} {
		t.Run(test.name, func(t *testing.T) {
			record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
			coordinator := func(version int) string {
				if version == 1 {
					return ""
				}
				return "/root"
			}
			if test.first != Version {
				record = rewriteInitialRevision(t, record, func(first *Record) {
					first.Version, first.Plan.CoordinatorID = test.first, coordinator(test.first)
				})
			}
			appendWorkerRevision(t, record, func(next *Record) {
				next.Version, next.Plan.CoordinatorID = test.second, coordinator(test.second)
			})
			before := productionTaskBytes(t, record.Directory)
			_, err := Load(record.Directory)
			if err == nil {
				t.Fatal("mixed-version chain accepted")
			}
			unsupported := test.first
			if unsupported == Version {
				unsupported = test.second
			}
			if message := unsupportedTaskVersionMessage(unsupported); !strings.Contains(err.Error(), message) {
				t.Fatalf("version %d revision not refused with %q: %v", unsupported, message, err)
			}
			if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
				t.Fatal("refused chain was rewritten")
			}
		})
	}
	t.Run("rewritten brief", func(t *testing.T) {
		record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
		record = mustAssignWorker(t, record, workerBrief("worker", "/root/worker", "implementation", []string{"source.txt"}))
		appendWorkerRevision(t, record, func(next *Record) {
			next.Assignments[0].Brief.Goal = "Rewritten goal"
			next.Assignments[0].SHA256, _ = assignmentHash(next.Assignments[0])
		})
		if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "immutable worker history") {
			t.Fatalf("rewritten brief accepted: %v", err)
		}
	})
	t.Run("false completed check", func(t *testing.T) {
		record, ref := fixtureTask(t, helperPlan(t, "small", "fail"))
		record = mustAssignWorker(t, record, workerBrief("worker", "/root/worker", "implementation", []string{"source.txt"}, "focused"))
		if _, err := Run(t.Context(), record.Directory, "focused"); err == nil {
			t.Fatal("failure fixture passed")
		}
		record, err := Load(record.Directory)
		if err != nil {
			t.Fatal(err)
		}
		input := workerResult(t, record, "worker", "completed", ref)
		appendWorkerRevision(t, record, func(next *Record) {
			bound := BoundWorkerResult{Result: input, CheckRuns: []string{next.Checks[0].ID}, SubmittedAt: time.Now().UTC()}
			bound.ScopeSHA256, _ = scopeBinding(input.Snapshot, []string{"source.txt"})
			bound.SHA256, _ = workerResultHash(bound)
			next.WorkerResults = append(next.WorkerResults, bound)
			next.EvidenceRevision++
			next.UpdatedAt = time.Now().UTC()
		})
		if _, err := Load(record.Directory); err == nil {
			t.Fatal("persisted false passed-check claim accepted")
		}
	})
}

func TestEvidenceBindingIncludesWorkerRecords(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	mustAssess(t, record, ref)
	before, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil {
		t.Fatal(err)
	}
	record = mustAssignWorker(t, record, workerBrief("evidence", "/root/evidence", "evidence", []string{"source.txt"}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, "evidence", "completed", ref))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "evidence", "accepted", ref))
	after, err := PrepareContext(t.Context(), record.Directory, "final")
	if err != nil || before.EvidenceSHA256 == after.EvidenceSHA256 {
		t.Fatalf("worker changes omitted from final evidence: %+v %v", after, err)
	}
	data, err := encode(record)
	if err != nil || !bytes.Contains(data, []byte(`"scope_sha256"`)) {
		t.Fatal("manager result binding missing")
	}
}

func withoutWorkerCollection(t *testing.T, input any, field string, null bool) []byte {
	t.Helper()
	data, err := encode(input)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]jsontext.Value
	if err := decodeBytes(data, &fields); err != nil {
		t.Fatal(err)
	}
	if null {
		fields[field] = jsontext.Value("null")
	} else {
		delete(fields, field)
	}
	data, err = encode(fields)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestWorkerInputsRequireExplicitNonNullCollections(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	for _, null := range []bool{false, true} {
		for _, field := range []string{"constraints", "check_ids"} {
			var input WorkerBrief
			if err := Decode(bytes.NewReader(withoutWorkerCollection(t, workerBrief("worker", "/root/worker", "implementation", []string{"source.txt"}, "focused"), field, null)), &input); err != nil {
				t.Fatal(err)
			}
			if _, err := AssignWorker(t.Context(), record.Directory, input); err == nil {
				t.Fatalf("brief %s missing/null=%v accepted", field, null)
			}
		}
	}
	record = mustAssignWorker(t, record, workerBrief("worker", "/root/worker", "implementation", []string{"source.txt"}, "focused"))
	mustAssess(t, record, ref)
	valid := workerResult(t, record, "worker", "completed", ref)
	for _, null := range []bool{false, true} {
		for _, field := range []string{"evidence", "changed_files", "check_ids", "findings", "uncertainties"} {
			var input WorkerResult
			if err := Decode(bytes.NewReader(withoutWorkerCollection(t, valid, field, null)), &input); err != nil {
				t.Fatal(err)
			}
			if _, err := SubmitWorkerResult(t.Context(), record.Directory, input); err == nil {
				t.Fatalf("result %s missing/null=%v accepted", field, null)
			}
		}
	}
	record = mustSubmitWorker(t, record, valid)
	for _, null := range []bool{false, true} {
		for _, field := range []string{"evidence", "check_ids", "findings"} {
			var input WorkerAcceptance
			if err := Decode(bytes.NewReader(withoutWorkerCollection(t, workerAcceptance(t, record, "worker", "accepted", ref), field, null)), &input); err != nil {
				t.Fatal(err)
			}
			if _, err := AcceptWorkerResult(t.Context(), record.Directory, input); err == nil {
				t.Fatalf("acceptance %s missing/null=%v accepted", field, null)
			}
		}
	}
	// The central record decoder must enforce this contract too, even if a
	// syntactically valid persisted result has the same normalized hash.
	name := filepath.Join(record.Directory, fmt.Sprintf("record-%06d.json", record.Revision))
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]jsontext.Value
	if err := decodeBytes(data, &fields); err != nil {
		t.Fatal(err)
	}
	var results []map[string]jsontext.Value
	if err := decodeBytes(fields["worker_results"], &results); err != nil {
		t.Fatal(err)
	}
	var resultFields map[string]jsontext.Value
	if err := decodeBytes(results[0]["result"], &resultFields); err != nil {
		t.Fatal(err)
	}
	delete(resultFields, "findings")
	results[0]["result"], _ = encode(resultFields)
	fields["worker_results"], _ = encode(results)
	data, err = encode(fields)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(record.Directory); err == nil || !strings.Contains(err.Error(), "explicit arrays") {
		t.Fatalf("persisted omitted worker findings accepted: %v", err)
	}
}

func TestConservativeScopeFailureCanBeCancelledAndReplaced(t *testing.T) {
	record, ref := fixtureTask(t, helperPlan(t, "small", "pass"))
	if err := os.MkdirAll(filepath.Join(record.Baseline.Root, "web", "node_modules"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(record.Baseline.Root, "web", "app.js"), []byte("export const ready = true;\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../source.txt", filepath.Join(record.Baseline.Root, "web", "node_modules", "linked")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	record = mustAssignWorker(t, record, workerBrief("broad", "/root/web", "implementation", []string{"web"}, "focused"))
	mustAssess(t, record, ref)
	if _, err := SubmitWorkerResult(t.Context(), record.Directory, workerResult(t, record, "broad", "completed", ref)); err == nil || !strings.Contains(err.Error(), "exact source-file scopes") {
		t.Fatalf("conservative scope failure lacks guidance: %v", err)
	}
	record = mustSubmitWorker(t, record, workerResult(t, record, "broad", "cancelled"))
	input := workerAcceptance(t, record, "broad", "unused")
	input.CheckIDs = []string{}
	record = mustAcceptWorker(t, record, input)
	record = mustAssignWorker(t, record, workerBrief("precise", "/root/web", "implementation", []string{"web/app.js"}, "focused"))
	record = mustSubmitWorker(t, record, workerResult(t, record, "precise", "completed", ref))
	record = mustAcceptWorker(t, record, workerAcceptance(t, record, "precise", "accepted", ref))
	if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
		t.Fatalf("source-file scope recovery failed: %+v %v", verified, err)
	}
}
