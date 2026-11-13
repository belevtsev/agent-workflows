package taskflow

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var updateGolden = flag.Bool("update", false, "rewrite the stage brief golden files")

// briefRecord is an in-memory lite task at /tasks/b: a first tests round t1,
// two test reviews r1 and r2, and a fix round t2 of r1's finding F1. r1's
// latest acceptance resolves F2 and accepts F3, so the open findings of the
// tests stage are r2/F1, then r1's F1 and F3; r1 carries a report.
func briefRecord() Record {
	profile, _ := BuiltInProfile("lite")
	created := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	hex := func(c string) string { return strings.Repeat(c, 32) }
	staged := func(id, agent, role, stage string) WorkerBrief {
		return WorkerBrief{ID: id, AgentID: agent, Role: role, Stage: stage}
	}
	t2 := staged("t2", "/root/te2", "implementation", StageTests)
	t2.Fixes = []FixRef{{Finding: "r1/F1"}}
	result := func(id string, findings ...WorkerFinding) BoundWorkerResult {
		return BoundWorkerResult{Result: WorkerResult{AssignmentID: id, Status: "completed", Findings: findings}}
	}
	r1 := result("r1",
		WorkerFinding{ID: "F1", Severity: "major", Summary: "TestParse does not cover the empty input."},
		WorkerFinding{ID: "F2", Severity: "minor", Summary: "A helper name repeats its package."},
		WorkerFinding{ID: "F3", Severity: "blocker", Summary: "The golden file is written by the test it checks."})
	r1.Result.Report = &ReportRef{Path: "/work/r1.md", SHA256: strings.Repeat("a", 64)}
	return Record{
		ID: hex("b"), Directory: "/tasks/b", CreatedAt: created, Profile: &profile,
		Plan: Plan{Title: "Parse empty input", Kind: "small", Profile: "lite",
			Requirements: []Requirement{{ID: "empty", Description: "Parse returns ErrEmpty for empty input."}, {ID: "limits", Description: "Parse refuses input over 1 MiB."}},
			Checks:       []Check{{ID: "unit"}, {ID: "vet"}}},
		Assignments: []WorkerAssignment{
			{Brief: staged("t1", "/root/te1", "implementation", StageTests), ContractFile: "contract-t1-" + hex("1") + ".json", InputFiles: []string{"input-t1-" + hex("2")}},
			{Brief: staged("r1", "/root/tr1", "review", StageTestReview), BundleFile: "bundle-r1-" + hex("3") + ".json"},
			{Brief: t2, ContractFile: "contract-t2-" + hex("4") + ".json", InputFiles: []string{"input-t2-" + hex("5")}},
			{Brief: staged("r2", "/root/tr2", "review", StageTestReview), BundleFile: "bundle-r2-" + hex("6") + ".json"},
		},
		WorkerResults: []BoundWorkerResult{result("t1"), r1, result("t2"), result("r2", WorkerFinding{ID: "F1", Severity: "minor", Summary: "The fix-round test has no oracle comment."})},
		WorkerAcceptances: []BoundWorkerAcceptance{{Acceptance: WorkerAcceptance{AssignmentID: "r1", Status: "accepted", Findings: []FindingDisposition{
			{FindingID: "F2", Status: "resolved"}, {FindingID: "F3", Status: "accepted"}}}}},
	}
}

// checkGolden compares got with testdata/brief/name, or rewrites it under
// -update.
func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", "brief", name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s differs from the golden file:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

// Oracle: the exact bytes of testdata/brief/tests.txt and tests.json, each
// at most MaxBriefBytes. They name the tests stage's latest assignment t2 and
// its stored files, 0 of 2 author rounds left (t1 is the first round, t2 the
// second), test-engineer.md, both checks and requirements, and the open
// findings newest first (r2/F1, then r1/F1 and r1/F3 with r1's report;
// r1/F2 is resolved).
func TestStageBriefMatchesGolden(t *testing.T) {
	brief, err := StageBrief(briefRecord(), StageTests)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeBrief(brief)
	if err != nil {
		t.Fatal(err)
	}
	text := []byte(brief.Text())
	checkGolden(t, "tests.txt", text)
	checkGolden(t, "tests.json", encoded)
	if len(text) > MaxBriefBytes || len(encoded) > MaxBriefBytes {
		t.Fatalf("brief is %d text bytes and %d JSON bytes; want at most %d each", len(text), len(encoded), MaxBriefBytes)
	}
}

// Oracle: D15 and the H3 acceptance. With 20 open findings, newest first
// F20..F1 across 20 test reviews, of 380-byte summaries except 60-byte ones
// for the four oldest, both encodings stay at most 4,096 bytes; the listed
// findings are the newest ones in order, with no short older one slipped in
// after a misfit, and omitted.findings counts the rest, so listed plus counted
// is 20 and at least one is counted.
func TestStageBriefWithTwentyFindingsListsTheNewestAndCountsTheRest(t *testing.T) {
	record := briefRecord()
	record.WorkerAcceptances = nil
	record.Assignments, record.WorkerResults = record.Assignments[:1], record.WorkerResults[:1]
	for i := 1; i <= 20; i++ {
		id, size := fmt.Sprintf("r%d", i), 380
		if i <= 4 {
			size = 60
		}
		record.Assignments = append(record.Assignments, WorkerAssignment{Brief: WorkerBrief{ID: id, AgentID: "/root/" + id, Role: "review", Stage: StageTestReview}})
		record.WorkerResults = append(record.WorkerResults, BoundWorkerResult{Result: WorkerResult{AssignmentID: id,
			Findings: []WorkerFinding{{ID: fmt.Sprintf("F%d", i), Severity: "major", Summary: findingText(i, size)}}}})
	}
	brief, err := StageBrief(record, StageTests)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeBrief(brief)
	if err != nil {
		t.Fatal(err)
	}
	if text := brief.Text(); len(text) > MaxBriefBytes || len(encoded) > MaxBriefBytes {
		t.Fatalf("brief is %d text bytes and %d JSON bytes; want at most %d each", len(text), len(encoded), MaxBriefBytes)
	}
	listed := len(brief.Findings)
	t.Logf("%d JSON bytes, %d text bytes; %d findings listed, %d counted", len(encoded), len(brief.Text()), listed, brief.Omitted.Findings)
	if listed == 0 || brief.Omitted.Findings == 0 || listed+brief.Omitted.Findings != 20 {
		t.Fatalf("listed %d, counted %d; want both above 0 and 20 in all", listed, brief.Omitted.Findings)
	}
	for i, finding := range brief.Findings {
		if want := fmt.Sprintf("r%d/F%d", 20-i, 20-i); finding.ID != want {
			t.Fatalf("finding %d is %s; want %s (newest first)", i, finding.ID, want)
		}
	}
	if want := fmt.Sprintf("not listed: %d older findings;", brief.Omitted.Findings); !strings.Contains(brief.Text(), want) {
		t.Fatalf("text lacks %q:\n%s", want, brief.Text())
	}
	// The bound holds for every summary length from 300 to 400 bytes, so for
	// every slack the last listed finding leaves before the count is added.
	for n := 300; n <= 400; n++ {
		for i := range record.WorkerResults[1:] {
			record.WorkerResults[i+1].Result.Findings[0].Summary = findingText(i+1, n)
		}
		brief, err := StageBrief(record, StageTests)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := EncodeBrief(brief)
		if err != nil {
			t.Fatal(err)
		}
		if text := brief.Text(); len(text) > MaxBriefBytes || len(encoded) > MaxBriefBytes {
			t.Fatalf("with %d-byte summaries the brief is %d text bytes and %d JSON bytes; want at most %d each", n, len(text), len(encoded), MaxBriefBytes)
		}
	}
}

// Oracle: the bound holds by construction, not by the fixture's luck. A
// 4,096-byte title, 256 requirements of 16 KiB, 256 checks with 64-byte IDs
// and a stored contract with 32 inputs, the largest the record allows, give both
// encodings of at most 4,096 bytes; the title is omitted rather than cut,
// every listed requirement keeps its whole description, and the files and
// checks leave room for the three open findings.
func TestStageBriefStaysWithinTheBoundAtTheRecordsLimits(t *testing.T) {
	record := briefRecord()
	record.Plan.Title = strings.Repeat("t", 4096)
	record.Plan.Requirements, record.Plan.Checks = nil, nil
	for i := range 256 {
		record.Plan.Requirements = append(record.Plan.Requirements, Requirement{ID: fmt.Sprintf("r%d", i), Description: strings.Repeat("d", 16384)})
		record.Plan.Checks = append(record.Plan.Checks, Check{ID: fmt.Sprintf("c%063d", i)})
	}
	record.Plan.Requirements[3].Description = "short"
	last := &record.Assignments[2]
	last.InputFiles = nil
	for i := range MaxBriefInputs {
		last.InputFiles = append(last.InputFiles, fmt.Sprintf("input-%s-%032d", strings.Repeat("x", 64), i))
	}
	brief, err := StageBrief(record, StageTests)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := EncodeBrief(brief)
	if err != nil {
		t.Fatal(err)
	}
	if text := brief.Text(); len(text) > MaxBriefBytes || len(encoded) > MaxBriefBytes {
		t.Fatalf("brief is %d text bytes and %d JSON bytes; want at most %d each", len(text), len(encoded), MaxBriefBytes)
	}
	if brief.Title != "" || !brief.Omitted.Title {
		t.Fatalf("title %d bytes, omitted %v; want it omitted whole", len(brief.Title), brief.Omitted.Title)
	}
	if len(brief.Requirements) != 1 || brief.Requirements[0].Description != "short" || brief.Omitted.Requirements != 255 {
		t.Fatalf("requirements %+v, omitted %d; want only r3 whole and 255 counted", brief.Requirements, brief.Omitted.Requirements)
	}
	if len(brief.Findings) != 3 || brief.Omitted.Findings != 0 {
		t.Fatalf("findings %d listed, %d counted; want all 3 listed", len(brief.Findings), brief.Omitted.Findings)
	}
}

// Oracle: cw task brief --stage names a stage of the task's profile. An
// unknown stage, a stage outside lite and any stage of a task without a
// profile are refused with ErrUnknownStage and the reason.
func TestStageBriefRefusesAStageOutsideTheProfile(t *testing.T) {
	legacy := briefRecord()
	legacy.Profile = nil
	for _, c := range []struct {
		record Record
		stage  string
		want   string
	}{
		{briefRecord(), "deploy", `unknown stage "deploy": use one of spec, spec-review`},
		{briefRecord(), StageSpec, "unknown stage spec: it is not in profile lite; use one of tests, implementation, test-review, code-review"},
		{legacy, StageTests, "unknown stage tests: the task was started before profiles and has no stages"},
	} {
		_, err := StageBrief(c.record, c.stage)
		if !errors.Is(err, ErrUnknownStage) || !strings.Contains(err.Error(), c.want) {
			t.Errorf("StageBrief(%s) = %v; want %q", c.stage, err, c.want)
		}
	}
}

// Oracle: each stage loads exactly one role contract, and the file exists in
// the skill (skills/task-orchestration/references/contracts).
func TestEveryStageLoadsOneExistingContract(t *testing.T) {
	for _, stage := range stageOrder {
		brief, err := StageBrief(Record{ID: strings.Repeat("b", 32), Profile: &Profile{Name: "strict", Stages: slices.Clone(stageOrder)}}, stage)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join("..", "..", brief.Reference)); err != nil {
			t.Errorf("stage %s loads %s: %v", stage, brief.Reference, err)
		}
	}
}

// Oracle: D15, each stage starts a fresh worker. A brief whose agent_id an
// assignment of another stage used is refused as invalid input with this
// exact text and no revision; the same agent with repeats is accepted.
func TestAssignRefusesAnAgentOfAnotherStageUnlessItRepeats(t *testing.T) {
	record, _ := stageTask(t, false)
	tests := stageBrief("t1", "implementation", StageTests, "source_test.txt")
	tests.AgentID = "/root/worker"
	record = mustAssignWorker(t, record, tests)
	record = mustAssignWorker(t, record, stageBrief("i1", "implementation", StageImplementation, "source.txt"))
	reused := stageBrief("i2", "implementation", StageImplementation, "other.txt")
	reused.AgentID = "/root/worker"
	before := revisions(t, record)
	_, err := AssignWorker(t.Context(), record.Directory, reused)
	want := "worker brief agent_id /root/worker already worked on stage tests in assignment t1; each stage starts a fresh worker, so give this implementation brief a new agent_id"
	if !errors.Is(err, ErrInvalidInput) || err.Error() != want {
		t.Fatalf("AssignWorker = %v;\nwant invalid input %q", err, want)
	}
	if after := revisions(t, record); after != before {
		t.Fatalf("revisions = %d, want %d", after, before)
	}
	reused.Repeats = "i1"
	mustAssignWorker(t, record, reused)
}
