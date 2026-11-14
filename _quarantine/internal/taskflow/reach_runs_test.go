package taskflow

import (
	json "encoding/json/v2"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Review reach_runs tests (contract C-reach-runs revision 1). Requirement IDs
// (RR1-RR4) are named in each test's comment; RR5 is the workerResult helper
// in workers_test.go.

const reachRunsReview = "review"
const reachRunsEvidence = "evidence"

func reachRunsRan(path, mode string) ReachRun {
	return ReachRun{Path: path, Mode: mode, Status: "ran", Command: "./" + path + " --check", Observed: "exit 0, printed the expected summary"}
}

func reachRunsNotRun(path, mode string) ReachRun {
	return ReachRun{Path: path, Mode: mode, Status: "not_run", Reason: "No Windows host is available to the reviewer."}
}

// reachRunsLive is a complete answer for the fixture: one entry per
// non-deleted reach row.
func reachRunsLive() []ReachRun {
	return []ReachRun{reachRunsRan("install.sh", "sh install.sh"), reachRunsRan("tool.sh", "bash tool.sh")}
}

// reachRunsFixture returns a small task with a review assignment whose stored
// bundle has three reach rows: install.sh and tool.sh (changed) and old.sh
// (deleted). An evidence (non-review) assignment exists too. The focused
// check has run after both assignments.
func reachRunsFixture(t *testing.T) Record {
	t.Helper()
	repo := bundleTestRepo(t, map[string]string{
		"source.txt": "first line\nsecond line\n",
		"install.sh": "#!/bin/sh\necho one\n",
		"tool.sh":    "#!/bin/sh\necho one\n",
		"old.sh":     "#!/bin/sh\necho old\n",
	}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "install.sh", "#!/bin/sh\necho two\n")
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho two\n")
	bundleTestRemove(t, repo, "old.sh")
	record = mustAssignWorker(t, record, workerBrief(reachRunsEvidence, "/root/reader", "evidence", []string{"."}, "focused"))
	bundle := mustBuildBundle(t, record, reachRunsReview)
	paths := bundleTestReachPaths(bundle.Reach)
	slices.Sort(paths)
	if !reflect.DeepEqual(paths, []string{"install.sh", "old.sh", "tool.sh"}) {
		t.Fatalf("fixture precondition: reach rows %+v", bundle.Reach)
	}
	for _, row := range bundle.Reach {
		if row.Deleted != (row.Path == "old.sh") {
			t.Fatalf("fixture precondition: row %q deleted=%v", row.Path, row.Deleted)
		}
	}
	bundleTestFill(&bundle, "n/a: test fixture")
	ref, _ := bundleTestWriteBundle(t, bundle)
	record = mustAssignWorker(t, record, bundleTestReviewBrief(reachRunsReview, ref))
	if assigned := findAssignment(record, reachRunsReview); assigned == nil || assigned.BundleFile == "" {
		t.Fatalf("fixture precondition: review assignment has no stored bundle: %+v", assigned)
	}
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func reachRunsResult(t *testing.T, record Record, id, status string, runs []ReachRun) WorkerResult {
	t.Helper()
	result := workerResult(t, record, id, status, bundleTestRef(t, record, "source.txt"))
	result.ReachRuns = runs
	return result
}

// reachRunsValidate calls the load-time validator for the named assignment.
func reachRunsValidate(t *testing.T, record Record, id string, result WorkerResult) error {
	t.Helper()
	assigned := findAssignment(record, id)
	if assigned == nil {
		t.Fatalf("assignment %q missing", id)
	}
	return validateResult(record, *assigned, result)
}

// reachRunsRefused asserts that SubmitWorkerResult refuses the input, that
// the error mentions every wanted fragment, and that no task file changed.
func reachRunsRefused(t *testing.T, record Record, input WorkerResult, want ...string) {
	t.Helper()
	before := productionTaskBytes(t, record.Directory)
	_, err := SubmitWorkerResult(t.Context(), record.Directory, input)
	if err == nil {
		t.Fatalf("result with reach_runs %+v accepted", input.ReachRuns)
	}
	for _, fragment := range want {
		if !strings.Contains(err.Error(), fragment) {
			t.Errorf("refusal %q does not mention %q", err, fragment)
		}
	}
	if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("refused result changed task files")
	}
}

// reachRunsWith returns the complete live answer with the tool.sh entry
// replaced by entry.
func reachRunsWith(entry ReachRun) []ReachRun {
	return []ReachRun{reachRunsRan("install.sh", "sh install.sh"), entry}
}

type reachRunsShapeCase struct {
	name string
	runs []ReachRun
}

// reachRunsBadShapes lists entry sets that break RR1 shape rules. Each set
// otherwise covers every live row of the fixture.
func reachRunsBadShapes() []reachRunsShapeCase {
	ran := reachRunsRan("tool.sh", "bash tool.sh")
	notRun := reachRunsNotRun("tool.sh", "Windows Git Bash")
	edit := func(base ReachRun, change func(*ReachRun)) []ReachRun {
		change(&base)
		return reachRunsWith(base)
	}
	extra := func(entry ReachRun) []ReachRun {
		return append(reachRunsLive(), entry)
	}
	cases := []reachRunsShapeCase{
		{"empty status", edit(ran, func(r *ReachRun) { r.Status = "" })},
		{"capitalized status", edit(ran, func(r *ReachRun) { r.Status = "Ran" })},
		{"unknown status", edit(ran, func(r *ReachRun) { r.Status = "skipped" })},
		{"padded status", edit(ran, func(r *ReachRun) { r.Status = " ran" })},
		{"blank path", extra(reachRunsRan("", "bash tool.sh"))},
		{"whitespace path", extra(reachRunsRan(" \t\n", "bash tool.sh"))},
		{"blank mode", edit(ran, func(r *ReachRun) { r.Mode = "" })},
		{"whitespace mode", edit(ran, func(r *ReachRun) { r.Mode = "  \t" })},
		{"ran empty command", edit(ran, func(r *ReachRun) { r.Command = "" })},
		{"ran whitespace command", edit(ran, func(r *ReachRun) { r.Command = " \n" })},
		{"ran empty observed", edit(ran, func(r *ReachRun) { r.Observed = "" })},
		{"ran whitespace observed", edit(ran, func(r *ReachRun) { r.Observed = "\t" })},
		{"ran with reason", edit(ran, func(r *ReachRun) { r.Reason = "Also not run." })},
		{"not_run empty reason", edit(notRun, func(r *ReachRun) { r.Reason = "" })},
		{"not_run whitespace reason", edit(notRun, func(r *ReachRun) { r.Reason = "   " })},
		{"not_run with command", edit(notRun, func(r *ReachRun) { r.Command = "bash tool.sh" })},
		{"not_run with observed", edit(notRun, func(r *ReachRun) { r.Observed = "exit 0" })},
		{"path over 4096 bytes", extra(reachRunsRan(strings.Repeat("p", 4097), "bash"))},
		{"path over 4096 bytes in multibyte runes", extra(reachRunsRan(strings.Repeat("é", 2049), "bash"))},
		{"mode over 16384 bytes", edit(ran, func(r *ReachRun) { r.Mode = strings.Repeat("m", 16385) })},
		{"mode over 16384 bytes in multibyte runes", edit(ran, func(r *ReachRun) { r.Mode = strings.Repeat("é", 8193) })},
		{"command over 16384 bytes", edit(ran, func(r *ReachRun) { r.Command = strings.Repeat("c", 16385) })},
		{"observed over 16384 bytes", edit(ran, func(r *ReachRun) { r.Observed = strings.Repeat("o", 16385) })},
		{"reason over 16384 bytes", edit(notRun, func(r *ReachRun) { r.Reason = strings.Repeat("r", 16385) })},
	}
	tooMany := reachRunsLive()
	for index := len(tooMany); index < 1025; index++ {
		tooMany = append(tooMany, reachRunsRan("tool.sh", fmt.Sprintf("mode %d", index)))
	}
	return append(cases, reachRunsShapeCase{"1025 entries", tooMany})
}

// RR1, RR3: the load-time validator returns shape errors for a review result
// that carries malformed reach_runs entries, and accepts the same result with
// a well-formed answer.
func TestReachRunsValidatorRefusesMalformedEntries(t *testing.T) {
	record := reachRunsFixture(t)
	if err := reachRunsValidate(t, record, reachRunsReview, reachRunsResult(t, record, reachRunsReview, "completed", reachRunsLive())); err != nil {
		t.Fatalf("well-formed reach_runs refused by validateResult: %v", err)
	}
	for _, test := range reachRunsBadShapes() {
		t.Run(test.name, func(t *testing.T) {
			result := reachRunsResult(t, record, reachRunsReview, "completed", test.runs)
			if err := reachRunsValidate(t, record, reachRunsReview, result); err == nil {
				t.Fatal("malformed reach_runs accepted by validateResult")
			}
		})
	}
	t.Run("blocked review", func(t *testing.T) {
		result := reachRunsResult(t, record, reachRunsReview, "blocked", []ReachRun{reachRunsRan("tool.sh", "")})
		if err := reachRunsValidate(t, record, reachRunsReview, result); err == nil {
			t.Fatal("malformed reach_runs on a blocked review accepted by validateResult")
		}
	})
}

// RR1: the shape limits are inclusive: exactly 1024 entries and fields of
// exactly the byte limit are valid.
func TestReachRunsValidatorAcceptsLimits(t *testing.T) {
	record := reachRunsFixture(t)
	cases := []reachRunsShapeCase{
		{"path of 4096 bytes", append(reachRunsLive(), reachRunsRan(strings.Repeat("p", 4096), "bash"))},
		{"mode of 16384 bytes", reachRunsWith(ReachRun{Path: "tool.sh", Mode: strings.Repeat("m", 16384), Status: "ran", Command: "bash tool.sh", Observed: "exit 0"})},
		{"command and observed of 16384 bytes", reachRunsWith(ReachRun{Path: "tool.sh", Mode: "bash", Status: "ran", Command: strings.Repeat("c", 16384), Observed: strings.Repeat("o", 16384)})},
		{"reason of 16384 bytes", reachRunsWith(ReachRun{Path: "tool.sh", Mode: "Windows", Status: "not_run", Reason: strings.Repeat("r", 16384)})},
		{"several modes per path", append(reachRunsLive(), reachRunsNotRun("tool.sh", "Windows Git Bash"), reachRunsRan("tool.sh", "zsh tool.sh"))},
	}
	exact := reachRunsLive()
	for index := len(exact); index < 1024; index++ {
		exact = append(exact, reachRunsRan("tool.sh", fmt.Sprintf("mode %d", index)))
	}
	cases = append(cases, reachRunsShapeCase{"1024 entries", exact})
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			result := reachRunsResult(t, record, reachRunsReview, "completed", test.runs)
			if err := reachRunsValidate(t, record, reachRunsReview, result); err != nil {
				t.Fatalf("reach_runs at the limit refused: %v", err)
			}
		})
	}
}

// RR1: SubmitWorkerResult refuses malformed entries without writing.
func TestReachRunsSubmissionRefusesMalformedEntries(t *testing.T) {
	t.Parallel()
	record := reachRunsFixture(t)
	for _, test := range reachRunsBadShapes() {
		t.Run(test.name, func(t *testing.T) {
			reachRunsRefused(t, record, reachRunsResult(t, record, reachRunsReview, "completed", test.runs))
		})
	}
	// The same fixture accepts the well-formed answer afterwards.
	mustSubmitWorker(t, record, reachRunsResult(t, record, reachRunsReview, "completed", reachRunsLive()))
}

// RR1: a non-review result carrying entries is refused at submission and by
// the load-time validator; the same result without entries is accepted.
func TestReachRunsRefusedForNonReviewRole(t *testing.T) {
	record := reachRunsFixture(t)
	runs := []ReachRun{reachRunsRan("tool.sh", "bash tool.sh")}
	with := reachRunsResult(t, record, reachRunsEvidence, "completed", runs)
	without := reachRunsResult(t, record, reachRunsEvidence, "completed", nil)
	if err := reachRunsValidate(t, record, reachRunsEvidence, without); err != nil {
		t.Fatalf("fixture precondition: evidence result without reach_runs invalid: %v", err)
	}
	if err := reachRunsValidate(t, record, reachRunsEvidence, with); err == nil {
		t.Fatal("validateResult accepted reach_runs on an evidence result")
	}
	reachRunsRefused(t, record, with)
	cancelled := reachRunsResult(t, record, reachRunsEvidence, "cancelled", runs)
	reachRunsRefused(t, record, cancelled)
	mustSubmitWorker(t, record, without)
}

// RR2: a completed bundled review must answer every non-deleted reach row by
// its exact path, and may name only paths of the bundle's reach rows.
func TestReachRunsSubmissionRequiresEveryLiveRow(t *testing.T) {
	t.Parallel()
	record := reachRunsFixture(t)
	for _, test := range []struct {
		name string
		runs []ReachRun
		want []string // fragments; nil where the set also names a non-row path, so either refusal may come first
	}{
		{"nil reach_runs", nil, nil},
		{"empty reach_runs", []ReachRun{}, nil},
		{"install.sh missing", []ReachRun{reachRunsRan("tool.sh", "bash tool.sh")}, []string{"install.sh"}},
		{"tool.sh missing", []ReachRun{reachRunsRan("install.sh", "sh install.sh")}, []string{"tool.sh"}},
		{"only install.sh answered, by not_run", []ReachRun{reachRunsNotRun("install.sh", "Windows")}, []string{"tool.sh"}},
		{"only the deleted row", []ReachRun{reachRunsNotRun("old.sh", "removed")}, nil},
		{"dot-slash spelling", []ReachRun{reachRunsRan("install.sh", "sh install.sh"), reachRunsRan("./tool.sh", "bash tool.sh")}, nil},
		{"different case", []ReachRun{reachRunsRan("install.sh", "sh install.sh"), reachRunsRan("TOOL.SH", "bash tool.sh")}, nil},
		{"trailing space", []ReachRun{reachRunsRan("install.sh", "sh install.sh"), reachRunsRan("tool.sh ", "bash tool.sh")}, nil},
		{"unknown extra path", append(reachRunsLive(), reachRunsRan("other.sh", "bash other.sh")), nil},
		{"unchanged project file", append(reachRunsLive(), reachRunsRan("source.txt", "cat source.txt")), nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := reachRunsResult(t, record, reachRunsReview, "completed", test.runs)
			reachRunsRefused(t, record, result, test.want...)
		})
	}
	t.Run("no entries names a live row", func(t *testing.T) {
		_, err := SubmitWorkerResult(t.Context(), record.Directory, reachRunsResult(t, record, reachRunsReview, "completed", nil))
		if err == nil || (!strings.Contains(err.Error(), "install.sh") && !strings.Contains(err.Error(), "tool.sh")) {
			t.Fatalf("refusal does not name a live reach row: %v", err)
		}
	})
	// Several entries per path, deleted row omitted: accepted and stored.
	runs := []ReachRun{
		reachRunsRan("install.sh", "sh install.sh"),
		reachRunsRan("tool.sh", "bash tool.sh"),
		reachRunsNotRun("tool.sh", "Windows Git Bash"),
	}
	record = mustSubmitWorker(t, record, reachRunsResult(t, record, reachRunsReview, "completed", runs))
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	stored := findWorkerResult(loaded, reachRunsReview)
	if stored == nil || !reflect.DeepEqual(stored.Result.ReachRuns, runs) {
		t.Fatalf("stored reach_runs %+v, want %+v", stored, runs)
	}
}

// RR2: an entry for a deleted row is allowed alongside the required ones.
func TestReachRunsSubmissionAllowsDeletedRowEntry(t *testing.T) {
	record := reachRunsFixture(t)
	runs := append(reachRunsLive(), reachRunsNotRun("old.sh", "deleted file, nothing to run"))
	record = mustSubmitWorker(t, record, reachRunsResult(t, record, reachRunsReview, "completed", runs))
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if stored := findWorkerResult(loaded, reachRunsReview); stored == nil || !reflect.DeepEqual(stored.Result.ReachRuns, runs) {
		t.Fatalf("stored reach_runs %+v, want %+v", stored, runs)
	}
}

// RR2: blocked and cancelled reviews need no entries; RR1 shape still applies
// to any entries they carry.
func TestReachRunsNotRequiredForIncompleteReview(t *testing.T) {
	t.Parallel()
	for _, status := range []string{"blocked", "cancelled"} {
		t.Run(status, func(t *testing.T) {
			record := reachRunsFixture(t)
			reachRunsRefused(t, record, reachRunsResult(t, record, reachRunsReview, status, []ReachRun{reachRunsRan("tool.sh", "bash tool.sh"), {Path: "install.sh", Mode: "sh", Status: "ran"}}))
			record = mustSubmitWorker(t, record, reachRunsResult(t, record, reachRunsReview, status, nil))
			if _, err := Load(record.Directory); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// RR2: a bundle with zero reach rows needs no entries, and any entry names a
// path that is not a reach row.
func TestReachRunsZeroRowBundle(t *testing.T) {
	record, _ := fixtureTask(t, helperPlan(t, "small", "pass"))
	bundle := mustBuildBundle(t, record, reachRunsReview)
	if len(bundle.Reach) != 0 {
		t.Fatalf("fixture precondition: unchanged source produced rows %+v", bundle.Reach)
	}
	ref, _ := bundleTestWriteBundle(t, bundle)
	record = mustAssignWorker(t, record, bundleTestReviewBrief(reachRunsReview, ref))
	if _, err := RunCheck(t.Context(), record.Directory, "focused"); err != nil {
		t.Fatal(err)
	}
	record, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	reachRunsRefused(t, record, reachRunsResult(t, record, reachRunsReview, "completed", []ReachRun{reachRunsRan("source.txt", "cat source.txt")}))
	record = mustSubmitWorker(t, record, reachRunsResult(t, record, reachRunsReview, "completed", nil))
	if _, err := Load(record.Directory); err != nil {
		t.Fatal(err)
	}
}

// RR3: validateResult does not require reach_runs, so a completed bundled
// review recorded before the field existed still validates.
func TestReachRunsNotRequiredByLoadValidator(t *testing.T) {
	record := reachRunsFixture(t)
	for name, runs := range map[string][]ReachRun{"nil": nil, "empty": {}, "partial": {reachRunsRan("tool.sh", "bash tool.sh")}} {
		t.Run(name, func(t *testing.T) {
			result := reachRunsResult(t, record, reachRunsReview, "completed", runs)
			if err := reachRunsValidate(t, record, reachRunsReview, result); err != nil {
				t.Fatalf("validateResult required reach_runs coverage: %v", err)
			}
		})
	}
}

// RR4: a result without entries encodes no reach_runs member, and decoding
// such a result re-encodes to the same bytes; entries encode with the
// contract's member names and omit empty optional members.
func TestReachRunsEncoding(t *testing.T) {
	base := WorkerResult{TaskID: "task", AssignmentID: "review", AssignmentSHA256: strings.Repeat("a", 64), AgentID: "/root/reviewer", Status: "completed", Summary: "Reviewed.", Evidence: []SourceRef{}, ChangedFiles: []WorkerChangedFile{}, CheckIDs: []string{}, Findings: []WorkerFinding{}, Uncertainties: []string{}}
	for name, runs := range map[string][]ReachRun{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			result := base
			result.ReachRuns = runs
			data, err := json.Marshal(result)
			if err != nil {
				t.Fatal(err)
			}
			var members map[string]any
			if err := json.Unmarshal(data, &members); err != nil {
				t.Fatal(err)
			}
			if _, ok := members["reach_runs"]; ok || strings.Contains(string(data), "reach_runs") {
				t.Fatalf("result without entries encodes reach_runs: %s", data)
			}
			var decoded WorkerResult
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != string(data) {
				t.Fatalf("round trip changed bytes:\n%s\n%s", data, again)
			}
		})
	}
	t.Run("entries", func(t *testing.T) {
		result := base
		result.ReachRuns = []ReachRun{reachRunsRan("tool.sh", "bash tool.sh"), reachRunsNotRun("tool.sh", "Windows")}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var members struct {
			ReachRuns []map[string]string `json:"reach_runs"`
		}
		if err := json.Unmarshal(data, &members, json.RejectUnknownMembers(false)); err != nil {
			t.Fatal(err)
		}
		want := []map[string]string{
			{"path": "tool.sh", "mode": "bash tool.sh", "status": "ran", "command": "./tool.sh --check", "observed": "exit 0, printed the expected summary"},
			{"path": "tool.sh", "mode": "Windows", "status": "not_run", "reason": "No Windows host is available to the reviewer."},
		}
		if !reflect.DeepEqual(members.ReachRuns, want) {
			t.Fatalf("encoded reach_runs %+v, want %+v", members.ReachRuns, want)
		}
	})
}
