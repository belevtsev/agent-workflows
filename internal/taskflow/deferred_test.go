package taskflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Contract: token-levers revision 1, section D (deferred finding disposition).

const deferredTestReason = "Out of scope for this change; tracked for the follow-up hardening task."

// deferredTestInspection starts a small task with one evidence worker
// "inspect" owning source.txt, optionally runs and assesses the planned check,
// and submits a completed result carrying one finding per id.
func deferredTestInspection(t *testing.T, assess bool, findings ...string) (Record, SourceRef) {
	t.Helper()
	record, ref := autofillTestInspection(t, helperPlan(t, "small", "pass"))
	if assess {
		mustAssess(t, record, ref)
	}
	input := workerResult(t, record, "inspect", "completed", ref)
	for _, id := range findings {
		input.Findings = append(input.Findings, WorkerFinding{ID: id, Summary: "Finding " + id + " in the inspected source.", Severity: "minor", Evidence: []SourceRef{ref}})
	}
	mustSubmitWorker(t, record, input)
	return autofillTestLoad(t, record.Directory), ref
}

func deferredTestDisposition(id, status, reason string, refs ...SourceRef) FindingDisposition {
	if refs == nil {
		refs = []SourceRef{}
	}
	return FindingDisposition{FindingID: id, Status: status, Reason: reason, Evidence: refs}
}

// deferredTestAcceptance builds an accepted or unused acceptance of "inspect".
func deferredTestAcceptance(t *testing.T, record Record, status string, ref SourceRef, findings ...FindingDisposition) WorkerAcceptance {
	t.Helper()
	acceptance := workerAcceptance(t, record, "inspect", status, ref)
	if status == "unused" {
		acceptance.Evidence, acceptance.CheckIDs = []SourceRef{}, []string{}
		acceptance.Reason = "The inspection is not used for completion; its findings are dispositioned explicitly."
	}
	acceptance.Findings = append([]FindingDisposition{}, findings...)
	return acceptance
}

func deferredTestLine(assignment, finding, reason string) string {
	return "worker assignment " + assignment + " finding " + finding + " deferred: " + reason
}

func deferredTestCount(lines []string, want string) int {
	count := 0
	for _, line := range lines {
		if line == want {
			count++
		}
	}
	return count
}

// D1, D3: a deferred disposition with a reason and source evidence is accepted
// and stored as deferred, in both accepted and unused acceptances. An unknown
// status is still refused, so acceptance of "deferred" is not vacuous.
func TestDeferredDispositionAcceptedInAcceptedAndUnusedAcceptances(t *testing.T) {
	for _, status := range []string{"accepted", "unused"} {
		t.Run(status, func(t *testing.T) {
			record, ref := deferredTestInspection(t, false, "concern")
			unknown := deferredTestAcceptance(t, record, status, ref, deferredTestDisposition("concern", "postponed", deferredTestReason, ref))
			_, err := AcceptWorkerResult(t.Context(), record.Directory, unknown)
			autofillTestRequireRejected(t, "unknown disposition status", record.Directory, err, "", 1, 0)

			mustAcceptWorker(t, record, deferredTestAcceptance(t, record, status, ref, deferredTestDisposition("concern", "deferred", deferredTestReason, ref)))
			stored := autofillTestLoad(t, record.Directory)
			if len(stored.WorkerAcceptances) != 1 {
				t.Fatalf("deferred %s acceptance not stored: %d acceptances", status, len(stored.WorkerAcceptances))
			}
			got := stored.WorkerAcceptances[0].Acceptance
			if got.Status != status || len(got.Findings) != 1 || got.Findings[0].FindingID != "concern" || got.Findings[0].Status != "deferred" ||
				got.Findings[0].Reason != deferredTestReason || len(got.Findings[0].Evidence) != 1 || got.Findings[0].Evidence[0] != ref {
				t.Fatalf("stored deferred %s acceptance = %+v", status, got)
			}
		})
	}
}

// D2: deferred needs a non-blank reason and at least one valid source
// reference; otherwise accept fails and records nothing.
func TestDeferredDispositionNeedsReasonAndValidSourceEvidence(t *testing.T) {
	for _, status := range []string{"accepted", "unused"} {
		t.Run(status, func(t *testing.T) {
			record, ref := deferredTestInspection(t, false, "concern")
			wrongHash := ref
			wrongHash.SHA256 = autofillTestWrongHash
			missingFile := SourceRef{Path: "missing.txt", StartLine: 1, EndLine: 1, SHA256: ref.SHA256}
			pastEnd := ref
			pastEnd.StartLine, pastEnd.EndLine = 1, 99
			for _, test := range []struct {
				name     string
				reason   string
				evidence []SourceRef
			}{
				{"empty_reason", "", []SourceRef{ref}},
				{"blank_reason", " \t\n ", []SourceRef{ref}},
				{"no_evidence", deferredTestReason, []SourceRef{}},
				{"wrong_hash", deferredTestReason, []SourceRef{wrongHash}},
				{"missing_file", deferredTestReason, []SourceRef{missingFile}},
				{"lines_past_end", deferredTestReason, []SourceRef{pastEnd}},
			} {
				input := deferredTestAcceptance(t, record, status, ref, deferredTestDisposition("concern", "deferred", test.reason, test.evidence...))
				_, err := AcceptWorkerResult(t.Context(), record.Directory, input)
				autofillTestRequireRejected(t, "deferred "+test.name, record.Directory, err, "", 1, 0)
			}
			mustAcceptWorker(t, record, deferredTestAcceptance(t, record, status, ref, deferredTestDisposition("concern", "deferred", deferredTestReason, ref)))
		})
	}
}

// D2 bounds: a deferred reason is bounded exactly as a rejected reason is.
func TestDeferredReasonBoundsMatchRejectedReason(t *testing.T) {
	candidates := map[string]string{
		"oversized":      strings.Repeat("x", 100_000),
		"nul_byte":       "reason with \x00 byte",
		"control":        "reason with \x1b escape",
		"padded":         "  " + deferredTestReason + "  ",
		"ordinary_short": "Deferred.",
	}
	refusedByRejected := 0
	for name, reason := range candidates {
		t.Run(name, func(t *testing.T) {
			outcome := map[string]bool{}
			for _, status := range []string{"rejected", "deferred"} {
				record, ref := deferredTestInspection(t, false, "concern")
				input := deferredTestAcceptance(t, record, "accepted", ref, deferredTestDisposition("concern", status, reason, ref))
				_, err := AcceptWorkerResult(t.Context(), record.Directory, input)
				outcome[status] = err == nil
				if err != nil {
					autofillTestRequireRejected(t, status+" "+name, record.Directory, err, "", 1, 0)
				}
			}
			if outcome["rejected"] != outcome["deferred"] {
				t.Fatalf("reason %s: rejected accepted=%t but deferred accepted=%t", name, outcome["rejected"], outcome["deferred"])
			}
			if !outcome["rejected"] {
				refusedByRejected++
			}
		})
	}
	if refusedByRejected == 0 {
		t.Fatal("fixture invariant: no candidate reason exceeded the rejected-reason bounds, so parity was not exercised")
	}
}

// D4: an accepted review whose findings are all deferred, rejected or resolved
// satisfies the independent-review gate; an accepted disposition still counts
// as unresolved.
func TestDeferredFindingsSatisfyIndependentReviewGate(t *testing.T) {
	for name, statuses := range map[string][]string{
		"all_deferred": {"deferred", "deferred", "deferred"},
		"mixed":        {"deferred", "rejected", "resolved"},
		"one_accepted": {"deferred", "accepted", "resolved"},
	} {
		t.Run(name, func(t *testing.T) {
			record, ref := preparedChangedTask(t)
			record = mustAssignWorker(t, record, workerBrief("review", "/root/independent-reviewer", "review", []string{"."}, "focused"))
			result := workerResult(t, record, "review", "completed", ref)
			ids := []string{"first", "second", "third"}
			for _, id := range ids {
				result.Findings = append(result.Findings, WorkerFinding{ID: id, Summary: "Review concern " + id + ".", Severity: "major", Evidence: []SourceRef{ref}})
			}
			record = mustSubmitWorker(t, record, result)
			acceptance := workerAcceptance(t, record, "review", "accepted", ref)
			for index, id := range ids {
				evidence := []SourceRef{ref}
				if statuses[index] == "accepted" {
					evidence = []SourceRef{}
				}
				acceptance.Findings = append(acceptance.Findings, deferredTestDisposition(id, statuses[index], deferredTestReason+" ("+id+")", evidence...))
			}
			record = mustAcceptWorker(t, record, acceptance)
			if slices.Contains(statuses, "accepted") {
				if _, err := PrepareContext(t.Context(), record.Directory, "final"); err == nil || !strings.Contains(err.Error(), "unresolved") {
					t.Fatalf("accepted (unresolved) finding passed the review gate: %v", err)
				}
				if verified, _ := Verify(t.Context(), record.Directory); verified.Complete {
					t.Fatal("accepted (unresolved) finding completed the task")
				}
				return
			}
			if problems := autofillTestReviewGateProblems(t, record.Directory); len(problems) != 0 {
				t.Fatalf("review with only deferred/rejected/resolved findings fails the review gate: %v", problems)
			}
			final, err := PrepareContext(t.Context(), record.Directory, "final")
			if err != nil {
				t.Fatalf("review with only deferred/rejected/resolved findings blocked the final context: %v", err)
			}
			attachCheckpoint(t, record, "final", final, nil)
			verified, err := Verify(t.Context(), record.Directory)
			if err != nil || !verified.Complete || len(verified.Problems) != 0 {
				t.Fatalf("deferred review findings blocked completion: %+v %v", verified, err)
			}
			for index, id := range ids {
				want := 0
				if statuses[index] == "deferred" {
					want = 1
				}
				if got := deferredTestCount(verified.Limitations, deferredTestLine("review", id, deferredTestReason+" ("+id+")")); got != want {
					t.Fatalf("finding %s (%s): %d limitation entries, want %d: %q", id, statuses[index], got, want, verified.Limitations)
				}
			}
		})
	}
}

// D5: verify lists every deferred disposition of each assignment's latest
// acceptance as one exact limitation; deferred findings are not problems, and
// earlier acceptances' deferrals are not listed.
func TestVerifyListsLatestDeferredDispositionsAsLimitations(t *testing.T) {
	record, ref := deferredTestInspection(t, true, "first", "second", "third")
	reasonFirst, reasonSecond := "First deferral reason.", "Second deferral reason."
	mustAcceptWorker(t, record, deferredTestAcceptance(t, record, "accepted", ref,
		deferredTestDisposition("first", "deferred", reasonFirst, ref),
		deferredTestDisposition("second", "deferred", reasonSecond, ref),
		deferredTestDisposition("third", "rejected", "Source contradicts the third finding.", ref)))
	verified, err := Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete || len(verified.Problems) != 0 {
		t.Fatalf("deferred findings reported as problems: %+v %v", verified, err)
	}
	for _, want := range []string{deferredTestLine("inspect", "first", reasonFirst), deferredTestLine("inspect", "second", reasonSecond)} {
		if got := deferredTestCount(verified.Limitations, want); got != 1 {
			t.Fatalf("limitation %q appears %d times: %q", want, got, verified.Limitations)
		}
	}
	if limitationNaming(verified.Limitations, "finding third") {
		t.Fatalf("rejected finding listed as a limitation: %q", verified.Limitations)
	}

	reasonLater := "Later deferral reason after rejudgment."
	record = autofillTestLoad(t, record.Directory)
	mustAcceptWorker(t, record, deferredTestAcceptance(t, record, "accepted", ref,
		deferredTestDisposition("first", "resolved", "The first finding was fixed and checked.", ref),
		deferredTestDisposition("second", "deferred", reasonLater, ref),
		deferredTestDisposition("third", "rejected", "Source contradicts the third finding.", ref)))
	verified, err = Verify(t.Context(), record.Directory)
	if err != nil || !verified.Complete || len(verified.Problems) != 0 {
		t.Fatalf("rejudged deferred findings: %+v %v", verified, err)
	}
	if got := deferredTestCount(verified.Limitations, deferredTestLine("inspect", "second", reasonLater)); got != 1 {
		t.Fatalf("latest deferral appears %d times: %q", got, verified.Limitations)
	}
	for _, stale := range []string{reasonFirst, reasonSecond} {
		if limitationNaming(verified.Limitations, stale) {
			t.Fatalf("superseded acceptance's deferral still listed (%q): %q", stale, verified.Limitations)
		}
	}
	if limitationNaming(verified.Limitations, "finding first") {
		t.Fatalf("resolved finding listed as deferred: %q", verified.Limitations)
	}
}

// D5 with D3: a deferral inside an unused acceptance is listed too.
func TestVerifyListsDeferredDispositionOfUnusedAcceptance(t *testing.T) {
	record, ref := deferredTestInspection(t, true, "concern")
	mustAcceptWorker(t, record, deferredTestAcceptance(t, record, "unused", ref, deferredTestDisposition("concern", "deferred", deferredTestReason, ref)))
	verified, _ := Verify(t.Context(), record.Directory)
	if got := deferredTestCount(verified.Limitations, deferredTestLine("inspect", "concern", deferredTestReason)); got != 1 {
		t.Fatalf("unused acceptance deferral appears %d times: %q", got, verified.Limitations)
	}
	for _, problem := range verified.Problems {
		if strings.Contains(problem, "concern") {
			t.Fatalf("deferred finding reported as a problem: %q", verified.Problems)
		}
	}
}

// D6: deferred evidence goes stale at verify exactly like resolved evidence.
// The worker owns other.txt only; the disposition cites either source.txt
// (which then changes) or other.txt (which does not). The control shows that
// the source.txt change by itself does not block completion.
func TestDeferredEvidenceFreshnessMatchesResolved(t *testing.T) {
	for _, test := range []struct {
		name, status string
		citeChanged  bool
	}{
		{"deferred_cited_changed", "deferred", true},
		{"resolved_cited_changed", "resolved", true},
		{"deferred_cited_unchanged", "deferred", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			sourceData, otherData := []byte("first line\nsecond line\n"), []byte("other line\n")
			for name, data := range map[string][]byte{"source.txt": sourceData, "other.txt": otherData} {
				if err := os.WriteFile(filepath.Join(repo, name), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			record, err := Start(t.Context(), repo, filepath.Join(t.TempDir(), "tasks"), helperPlan(t, "small", "pass"), fixtureBinding())
			if err != nil {
				t.Fatal(err)
			}
			sourceRef := SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: digest(sourceData)}
			otherRef := SourceRef{Path: "other.txt", StartLine: 1, EndLine: 1, SHA256: digest(otherData)}
			record = mustAssignWorker(t, record, workerBrief("inspect", "/root/project_explorer", "evidence", []string{"other.txt"}))
			mustAssess(t, record, otherRef)
			result := workerResult(t, record, "inspect", "completed", otherRef)
			result.Findings = []WorkerFinding{{ID: "concern", Summary: "Finding about the inspected source.", Severity: "major", Evidence: []SourceRef{otherRef}}}
			record = mustSubmitWorker(t, record, result)
			cited := otherRef
			if test.citeChanged {
				cited = sourceRef
			}
			acceptance := workerAcceptance(t, record, "inspect", "accepted", otherRef)
			acceptance.Findings = []FindingDisposition{deferredTestDisposition("concern", test.status, deferredTestReason, cited)}
			mustAcceptWorker(t, record, acceptance)
			if verified, err := Verify(t.Context(), record.Directory); err != nil || !verified.Complete {
				t.Fatalf("fixture invariant: task incomplete before the cited bytes changed: %+v %v", verified, err)
			}
			if err := os.WriteFile(filepath.Join(repo, "source.txt"), []byte("changed first line\nsecond line\n"), 0600); err != nil {
				t.Fatal(err)
			}
			mustAssess(t, record, otherRef)
			verified, err := Verify(t.Context(), record.Directory)
			if !test.citeChanged {
				if err != nil || !verified.Complete || len(verified.Problems) != 0 {
					t.Fatalf("uncited source change blocked completion: %+v %v", verified, err)
				}
				return
			}
			if verified.Complete || len(verified.Problems) == 0 {
				t.Fatalf("%s evidence whose cited bytes changed was not a problem: %+v %v", test.status, verified, err)
			}
		})
	}
}
