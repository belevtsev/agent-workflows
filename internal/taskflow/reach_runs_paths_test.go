package taskflow

import (
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Reach-run path tests (contract C-reach-runs r3: RR2a, RR2b, RR2c, RR6,
// RR7). Requirement IDs are named in each test's comment.

// reachPathsR3Fixture returns a task whose "review" assignment carries a
// stored bundle with three reach rows in bundle order: install.sh (changed,
// live), old.sh (deleted) and tool.sh (changed, live). The focused check has
// run, so a completed review result can be submitted.
func reachPathsR3Fixture(t *testing.T) Record {
	t.Helper()
	repo := bundleTestRepo(t, map[string]string{
		"source.txt": "first line\nsecond line\n",
		"install.sh": "#!/bin/sh\necho install v0\n",
		"tool.sh":    "#!/bin/sh\necho tool v0\n",
		"old.sh":     "#!/bin/sh\necho old\n",
	}, false)
	record := bundleTestStart(t, repo, helperPlan(t, "small", "pass"))
	bundleTestWrite(t, repo, "install.sh", "#!/bin/sh\necho install v1\n")
	bundleTestWrite(t, repo, "tool.sh", "#!/bin/sh\necho tool v1\n")
	bundleTestRemove(t, repo, "old.sh")

	bundle := mustBuildBundle(t, record, "review")
	if got := bundleTestReachPaths(bundle.Reach); !reflect.DeepEqual(got, []string{"install.sh", "old.sh", "tool.sh"}) {
		t.Fatalf("fixture precondition: reach rows %q", got)
	}
	for _, row := range bundle.Reach {
		if row.Deleted != (row.Path == "old.sh") {
			t.Fatalf("fixture precondition: row %s deleted = %v", row.Path, row.Deleted)
		}
	}
	bundleTestFill(&bundle, "n/a: test fixture")
	ref, _ := bundleTestWriteBundle(t, bundle)
	brief := workerBrief("review", "/root/reach-paths-reviewer", "review", []string{"."}, "focused")
	brief.Bundle = ref
	record = mustAssignWorker(t, record, brief)
	if assigned := findAssignment(record, "review"); assigned == nil || assigned.BundleFile == "" {
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

// reachPathsR3Ran is one well-formed "ran" entry for the given path.
func reachPathsR3Ran(path string) ReachRun {
	return ReachRun{Path: path, Mode: "direct invocation", Status: "ran", Command: "sh " + path, Observed: "exit status 0"}
}

// reachPathsR3Complete is the complete answer for the fixture: one "ran"
// entry for each live row.
func reachPathsR3Complete() []ReachRun {
	return []ReachRun{reachPathsR3Ran("install.sh"), reachPathsR3Ran("tool.sh")}
}

// reachPathsR3Result builds a completed review result for the fixture with
// exactly the given reach runs.
func reachPathsR3Result(t *testing.T, record Record, runs []ReachRun) WorkerResult {
	t.Helper()
	result := workerResult(t, record, "review", "completed", bundleTestRef(t, record, "source.txt"))
	result.ReachRuns = runs
	return result
}

// reachPathsR3Refused submits the runs, asserts the refusal mentions want
// byte-exactly, that no task file changed and that a following Load sees no
// result for the review (RR2c); it then submits the complete answer and
// asserts it is accepted and recorded (RR2c).
func reachPathsR3Refused(t *testing.T, runs []ReachRun, want string) {
	t.Helper()
	record := reachPathsR3Fixture(t)
	before := productionTaskBytes(t, record.Directory)
	_, err := SubmitWorkerResult(t.Context(), record.Directory, reachPathsR3Result(t, record, runs))
	if err == nil {
		t.Fatalf("completed review with reach runs %+v accepted", runs)
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("refusal %q does not contain %q", err, want)
	}
	if after := productionTaskBytes(t, record.Directory); !reflect.DeepEqual(after, before) {
		t.Fatal("refused result changed task files")
	}
	loaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if stored := findWorkerResult(loaded, "review"); stored != nil {
		t.Fatalf("refused result recorded: %+v", stored)
	}

	updated, err := SubmitWorkerResult(t.Context(), loaded.Directory, reachPathsR3Result(t, loaded, reachPathsR3Complete()))
	if err != nil {
		t.Fatalf("complete answer after refusal refused: %v", err)
	}
	if stored := findWorkerResult(updated, "review"); stored == nil {
		t.Fatal("accepted complete answer not in returned record")
	}
	reloaded, err := Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	stored := findWorkerResult(reloaded, "review")
	if stored == nil {
		t.Fatal("accepted complete answer not recorded")
	}
	if !reflect.DeepEqual(stored.Result.ReachRuns, reachPathsR3Complete()) {
		t.Fatalf("recorded reach runs %+v, want %+v", stored.Result.ReachRuns, reachPathsR3Complete())
	}
}

// RR2a, RR2c: an entry whose path is not the exact path of a reach row is
// refused with that path in the error, even when the misspelling also leaves
// the live row tool.sh unanswered. The "plus" cases answer every live row,
// so only the unknown-path check can refuse them.
func TestReachPathsR3UnknownPathWins(t *testing.T) {
	cases := []struct {
		name string
		runs []ReachRun
		want string
	}{
		{"dot slash prefix", []ReachRun{reachPathsR3Ran("install.sh"), reachPathsR3Ran("./tool.sh")}, "./tool.sh"},
		{"upper case", []ReachRun{reachPathsR3Ran("install.sh"), reachPathsR3Ran("TOOL.SH")}, "TOOL.SH"},
		{"trailing space", []ReachRun{reachPathsR3Ran("install.sh"), reachPathsR3Ran("tool.sh ")}, "tool.sh "},
		{"complete plus other.sh", append(reachPathsR3Complete(), reachPathsR3Ran("other.sh")), "other.sh"},
		{"complete plus trailing space", append(reachPathsR3Complete(), reachPathsR3Ran("tool.sh ")), "tool.sh "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reachPathsR3Refused(t, tc.runs, tc.want)
		})
	}
}

// RR2b, RR2c: when every entry names a reach row but a live row has no
// entry, the error names the first missing live row in bundle order.
func TestReachPathsR3MissingLiveRow(t *testing.T) {
	cases := []struct {
		name string
		runs []ReachRun
		want string
	}{
		{"only old.sh", []ReachRun{reachPathsR3Ran("old.sh")}, "install.sh"},
		{"only install.sh", []ReachRun{reachPathsR3Ran("install.sh")}, "tool.sh"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reachPathsR3Refused(t, tc.runs, tc.want)
		})
	}
}

// RR6: a review assignment without a stored bundle needs no reach runs, even
// when root holds no bundle file.
func TestReachPathsR3NoBundleNoCoverage(t *testing.T) {
	root, err := os.OpenRoot(bundleTestDir(t))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	assigned := WorkerAssignment{Brief: workerBrief("review", "/root/reach-paths-reviewer", "review", []string{"."}, "focused")}
	if assigned.BundleFile != "" || assigned.Brief.Bundle != nil {
		t.Fatalf("fixture precondition: assignment carries a bundle: %+v", assigned)
	}
	result := WorkerResult{AssignmentID: "review", AgentID: assigned.Brief.AgentID, Status: "completed"}
	if err := reachRunCoverage(root, assigned, result); err != nil {
		t.Fatalf("review without bundle refused: %v", err)
	}
}

// reachPathsR3StoredBundle writes the bundle JSON with the given version into
// a fresh 0700 directory as bundle-review-<32 hex>.json (0600) and returns the
// opened root and a review assignment bound to that stored file.
func reachPathsR3StoredBundle(t *testing.T, version int) (*os.Root, WorkerAssignment) {
	t.Helper()
	directory := filepath.Join(bundleTestDir(t), "task")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(Bundle{Version: version, ReviewID: "review", Reach: []ReachRow{{Path: "a.sh"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"path":"a.sh"`) {
		t.Fatalf("fixture precondition: bundle JSON has no a.sh row: %s", data)
	}
	name := "bundle-review-0123456789abcdef0123456789abcdef.json"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	brief := workerBrief("review", "/root/reach-paths-reviewer", "review", []string{"."}, "focused")
	brief.Bundle = &BundleRef{Path: path, SHA256: bundleTestSum(data)}
	return root, WorkerAssignment{Brief: brief, BundleFile: name}
}

// RR7: a version-1 stored bundle predates the reach-run duty and needs no
// reach runs; the same bundle as version 2 requires an entry for a.sh.
func TestReachPathsR3VersionOneBundleExempt(t *testing.T) {
	result := WorkerResult{AssignmentID: "review", AgentID: "/root/reach-paths-reviewer", Status: "completed"}
	root, assigned := reachPathsR3StoredBundle(t, 1)
	if err := reachRunCoverage(root, assigned, result); err != nil {
		t.Fatalf("version-1 bundle required reach runs: %v", err)
	}
	root, assigned = reachPathsR3StoredBundle(t, 2)
	err := reachRunCoverage(root, assigned, result)
	if err == nil {
		t.Fatal("version-2 bundle with an unanswered live row accepted")
	}
	if !strings.Contains(err.Error(), "a.sh") {
		t.Fatalf("version-2 refusal %q does not name a.sh", err)
	}
}
