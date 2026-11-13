package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Contract: cw-task-autofill-summary revision 1, requirements R1, R2, R6, R8 and R9 through the CLI.

const summaryCLITestPlan = `{"title":"Summarize source-bound task state","kind":"small","requirements":[` +
	`{"id":"zeta","description":"Inspect the source declaration","check_ids":["version"]},` +
	`{"id":"alpha","description":"Confirm the available Git version","check_ids":["again"]}],` +
	`"checks":[{"id":"version","argv":["git","--version"],"timeout_seconds":30},` +
	`{"id":"again","argv":["git","--version"],"timeout_seconds":30}]}`

type summaryCLIHarness struct {
	t      *testing.T
	config Config
	out    *bytes.Buffer
	data   []byte
	task   string
}

func newSummaryCLIHarness(t *testing.T) *summaryCLIHarness {
	t.Helper()
	repo, home := t.TempDir(), t.TempDir()
	data := []byte("first line\nsecond line\n")
	if err := os.WriteFile(filepath.Join(repo, "source.txt"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, out := testConfig(t)
	config.HomeDir = func() (string, error) { return home, nil }
	config.WorkingDir = func() (string, error) { return repo, nil }
	config.Getenv = func(string) string { return "" }
	return &summaryCLIHarness{t: t, config: config, out: out, data: data}
}

func (h *summaryCLIHarness) execute(input []byte, args ...string) []byte {
	h.t.Helper()
	h.config.In = bytes.NewReader(input)
	h.out.Reset()
	if err := Execute(h.t.Context(), args, h.config); err != nil {
		h.t.Fatalf("cw %s: %v", strings.Join(args, " "), err)
	}
	return bytes.Clone(h.out.Bytes())
}

func (h *summaryCLIHarness) start(summary bool) []byte {
	h.t.Helper()
	return h.startPlan(summaryCLITestPlan, summary)
}

func (h *summaryCLIHarness) startPlan(plan string, summary bool) []byte {
	h.t.Helper()
	args := []string{"task", "start", "--plan", "-", "--policy", shippedPolicyPath}
	// --summary is the default and changes nothing; --full prints the record.
	if summary {
		args = append(args, "--summary")
	} else {
		args = append(args, "--full")
	}
	return h.execute([]byte(plan), args...)
}

func (h *summaryCLIHarness) taskInput(command string, input any, summary bool) []byte {
	h.t.Helper()
	encoded, err := json.Marshal(input)
	if err != nil {
		h.t.Fatal(err)
	}
	args := []string{"task", command, "--task", h.task, "--input", "-"}
	// --summary is the default and changes nothing; --full prints the record.
	if summary {
		args = append(args, "--summary")
	} else {
		args = append(args, "--full")
	}
	return h.execute(encoded, args...)
}

// directoryOf is the directory of the task with ID id, which a summary names
// instead of the directory: cw task show resolves the ID in the task store.
func (h *summaryCLIHarness) directoryOf(id string) string {
	h.t.Helper()
	var fields map[string]string
	if err := json.Unmarshal(h.execute(nil, "task", "show", "--task", id, "--field", "directory"), &fields); err != nil || fields["directory"] == "" {
		h.t.Fatalf("task show --task %s --field directory: %v %v", id, fields, err)
	}
	return fields["directory"]
}

func (h *summaryCLIHarness) load() taskflow.Record {
	h.t.Helper()
	record, err := taskflow.Load(h.task)
	if err != nil {
		h.t.Fatal(err)
	}
	return record
}

func (h *summaryCLIHarness) fileSHA256() string {
	hash := sha256.Sum256(h.data)
	return hex.EncodeToString(hash[:])
}

func (h *summaryCLIHarness) runChecks() {
	h.t.Helper()
	for _, id := range []string{"version", "again"} {
		if _, err := (taskflow.Env{}).RunCheck(h.t.Context(), h.task, id); err != nil {
			h.t.Fatal(err)
		}
	}
}

func summaryCLIBrief() taskflow.WorkerBrief {
	return taskflow.WorkerBrief{
		ID: "inspect", AgentID: "/root/project_explorer", Role: "evidence",
		Goal: "Inspect the selected source declaration", OwnedPaths: []string{"source.txt"},
		Constraints: []string{"Read source without changing it"}, CheckIDs: []string{},
		StopCondition: "Return the source evidence and uncertainties",
	}
}

// summaryCLIUnhashedRef omits the sha256 member entirely.
func summaryCLIUnhashedRef() map[string]any {
	return map[string]any{"path": "source.txt", "start_line": 1, "end_line": 1}
}

// summaryCLIResult omits task_id, assignment_sha256, agent_id and snapshot members.
func summaryCLIResult() map[string]any {
	return map[string]any{
		"assignment_id": "inspect", "status": "completed", "summary": "Inspected the selected source declaration",
		"evidence": []any{summaryCLIUnhashedRef()}, "changed_files": []any{}, "check_ids": []any{},
		"findings": []any{}, "uncertainties": []any{},
	}
}

// summaryCLIAcceptance omits task_id, result_sha256 and coordinator_id members.
func summaryCLIAcceptance() map[string]any {
	return map[string]any{
		"assignment_id": "inspect", "status": "accepted",
		"reason":   "Checked the returned reference against the selected source",
		"evidence": []any{summaryCLIUnhashedRef()}, "check_ids": []any{}, "findings": []any{},
	}
}

func summaryCLIAssessment(sha string, ids []string, decision bool) taskflow.AssessmentInput {
	ref := taskflow.SourceRef{Path: "source.txt", StartLine: 1, EndLine: 2, SHA256: sha}
	checks := map[string]string{"zeta": "version", "alpha": "again"}
	input := taskflow.AssessmentInput{Requirements: []taskflow.Assessment{}}
	for _, id := range ids {
		input.Requirements = append(input.Requirements, taskflow.Assessment{
			ID: id, Status: "satisfied", Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{checks[id]},
		})
	}
	if decision {
		input.Decision = &taskflow.Decision{
			Status: "resolved", Reason: "Actual source and current planned checks establish the result.",
			Evidence: []taskflow.SourceRef{ref}, CheckIDs: []string{"version", "again"},
		}
	}
	return input
}

// parseSummaryLineForTest requires exactly one line holding one JSON object with exactly the given members.
func parseSummaryLineForTest(t *testing.T, command string, output []byte, members ...string) map[string]json.RawMessage {
	t.Helper()
	line := strings.TrimSuffix(string(output), "\n")
	if strings.TrimSpace(line) == "" || strings.Contains(line, "\n") {
		t.Fatalf("%s --summary output is not exactly one line: %q", command, output)
	}
	decoder := json.NewDecoder(strings.NewReader(line))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		t.Fatalf("%s --summary output is not one JSON object (%v): %q", command, err, line)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s --summary output holds more than one JSON value: %q", command, line)
	}
	got := slices.Sorted(maps.Keys(object))
	want := slices.Clone(members)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("%s --summary members = %v, want %v", command, got, want)
	}
	return object
}

func summaryDecodeForTest(t *testing.T, object map[string]json.RawMessage, member string, target any) {
	t.Helper()
	if err := json.Unmarshal(object[member], target); err != nil {
		t.Fatalf("summary member %q = %s: %v", member, object[member], err)
	}
}

func summaryStringForTest(t *testing.T, object map[string]json.RawMessage, member string) string {
	t.Helper()
	var value string
	summaryDecodeForTest(t, object, member, &value)
	return value
}

func summaryCompactForTest(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		t.Fatalf("summary member %s: %v", raw, err)
	}
	return compact.String()
}

// R8, R9 (start, assign, handoff, accept, assess) with R1, R2 and R6 autofill through absent JSON members.
func TestTaskSummaryFlagReportsStoredWorkerFlow(t *testing.T) {
	h := newSummaryCLIHarness(t)

	start := parseSummaryLineForTest(t, "start", h.start(true), "task_id", "advisers", "requirements", "checks")
	h.task = h.directoryOf(summaryStringForTest(t, start, "task_id"))
	stored := h.load()
	if stored.Directory != h.task || h.task == "" {
		t.Fatalf("start task = %q, want record directory %q", h.task, stored.Directory)
	}
	if got := summaryStringForTest(t, start, "task_id"); got != stored.ID || got == "" {
		t.Fatalf("start task_id = %q, want %q", got, stored.ID)
	}
	if got := summaryCompactForTest(t, start["advisers"]); got != "{}" || len(stored.Advisers) != 0 {
		t.Fatalf("start advisers = %s with stored %v, want {}", got, stored.Advisers)
	}
	var requirements, checks []string
	summaryDecodeForTest(t, start, "requirements", &requirements)
	summaryDecodeForTest(t, start, "checks", &checks)
	var planRequirements, planChecks []string
	for _, requirement := range stored.Plan.Requirements {
		planRequirements = append(planRequirements, requirement.ID)
	}
	for _, check := range stored.Plan.Checks {
		planChecks = append(planChecks, check.ID)
	}
	if !slices.Equal(requirements, []string{"zeta", "alpha"}) || !slices.Equal(requirements, planRequirements) {
		t.Fatalf("start requirements = %v, want plan order %v", requirements, planRequirements)
	}
	if !slices.Equal(checks, []string{"version", "again"}) || !slices.Equal(checks, planChecks) {
		t.Fatalf("start checks = %v, want plan order %v", checks, planChecks)
	}

	assign := parseSummaryLineForTest(t, "assign", h.taskInput("assign", summaryCLIBrief(), true),
		"task_id", "assignment_id", "assignment_sha256", "fingerprint")
	stored = h.load()
	if len(stored.Assignments) != 1 {
		t.Fatal("assignment was not stored")
	}
	assignment := stored.Assignments[0]
	if summaryStringForTest(t, assign, "task_id") != h.load().ID || summaryStringForTest(t, assign, "assignment_id") != "inspect" {
		t.Fatalf("assign summary identifies the wrong assignment: %v", assign)
	}
	if got := summaryStringForTest(t, assign, "assignment_sha256"); got != assignment.SHA256 || got == "" {
		t.Fatalf("assign assignment_sha256 = %q, want %q", got, assignment.SHA256)
	}
	if got := summaryStringForTest(t, assign, "fingerprint"); got != assignment.Snapshot.Fingerprint || got == "" {
		t.Fatalf("assign fingerprint = %q, want %q", got, assignment.Snapshot.Fingerprint)
	}

	handoff := parseSummaryLineForTest(t, "handoff", h.taskInput("handoff", summaryCLIResult(), true),
		"task_id", "assignment_id", "result_sha256", "status", "check_runs")
	stored = h.load()
	if len(stored.WorkerResults) != 1 {
		t.Fatal("handoff was not stored")
	}
	bound := stored.WorkerResults[0]
	if summaryStringForTest(t, handoff, "task_id") != h.load().ID || summaryStringForTest(t, handoff, "assignment_id") != "inspect" {
		t.Fatalf("handoff summary identifies the wrong assignment: %v", handoff)
	}
	if got := summaryStringForTest(t, handoff, "result_sha256"); got != bound.SHA256 || got == "" {
		t.Fatalf("handoff result_sha256 = %q, want %q", got, bound.SHA256)
	}
	if got := summaryStringForTest(t, handoff, "status"); got != bound.Result.Status || got != "completed" {
		t.Fatalf("handoff status = %q, want %q", got, bound.Result.Status)
	}
	if got := summaryCompactForTest(t, handoff["check_runs"]); got != "[]" || len(bound.CheckRuns) != 0 {
		t.Fatalf("handoff check_runs = %s with stored %v, want []", got, bound.CheckRuns)
	}
	current, err := projectctx.Capture(t.Context(), stored.Baseline.SelectedPath)
	if err != nil {
		t.Fatal(err)
	}
	result := bound.Result
	if result.TaskID != stored.ID || result.AssignmentSHA256 != assignment.SHA256 || result.AgentID != "/root/project_explorer" || result.Snapshot != current {
		t.Fatalf("handoff binding was not filled from the task: %+v", result)
	}
	if len(result.Evidence) != 1 || result.Evidence[0].SHA256 != h.fileSHA256() {
		t.Fatalf("handoff evidence hash was not filled: %+v", result.Evidence)
	}

	accept := parseSummaryLineForTest(t, "accept", h.taskInput("accept", summaryCLIAcceptance(), true),
		"task_id", "assignment_id", "status", "findings")
	stored = h.load()
	if len(stored.WorkerAcceptances) != 1 {
		t.Fatal("acceptance was not stored")
	}
	acceptance := stored.WorkerAcceptances[0].Acceptance
	if summaryStringForTest(t, accept, "task_id") != h.load().ID || summaryStringForTest(t, accept, "assignment_id") != "inspect" {
		t.Fatalf("accept summary identifies the wrong assignment: %v", accept)
	}
	if got := summaryStringForTest(t, accept, "status"); got != acceptance.Status || got != "accepted" {
		t.Fatalf("accept status = %q, want %q", got, acceptance.Status)
	}
	if got := summaryCompactForTest(t, accept["findings"]); got != "{}" || len(acceptance.Findings) != 0 {
		t.Fatalf("accept findings = %s with stored %v, want {}", got, acceptance.Findings)
	}
	if acceptance.TaskID != stored.ID || acceptance.CoordinatorID != "/root" || acceptance.ResultSHA256 != bound.SHA256 {
		t.Fatalf("acceptance binding was not filled from the task: %+v", acceptance)
	}
	if len(acceptance.Evidence) != 1 || acceptance.Evidence[0].SHA256 != h.fileSHA256() {
		t.Fatalf("acceptance evidence hash was not filled: %+v", acceptance.Evidence)
	}

	h.runChecks()
	partial := parseSummaryLineForTest(t, "assess", h.taskInput("assess", summaryCLIAssessment(h.fileSHA256(), []string{"zeta"}, false), true),
		"task_id", "requirements")
	stored = h.load()
	var statuses map[string]string
	summaryDecodeForTest(t, partial, "requirements", &statuses)
	if summaryStringForTest(t, partial, "task_id") != h.load().ID || len(statuses) != 1 ||
		statuses["zeta"] != "satisfied" || statuses["zeta"] != stored.Assessments["zeta"].Assessment.Status {
		t.Fatalf("assess requirements = %v, want only zeta with its stored status", statuses)
	}

	full := parseSummaryLineForTest(t, "assess", h.taskInput("assess", summaryCLIAssessment(h.fileSHA256(), []string{"zeta", "alpha"}, true), true),
		"task_id", "requirements", "decision")
	stored = h.load()
	statuses = nil
	summaryDecodeForTest(t, full, "requirements", &statuses)
	if len(statuses) != 2 {
		t.Fatalf("assess requirements = %v, want zeta and alpha", statuses)
	}
	for _, id := range []string{"zeta", "alpha"} {
		if statuses[id] != "satisfied" || statuses[id] != stored.Assessments[id].Assessment.Status {
			t.Fatalf("assess requirement %s = %q, want stored %q", id, statuses[id], stored.Assessments[id].Assessment.Status)
		}
	}
	if stored.Decision == nil {
		t.Fatal("decision was not stored")
	}
	if got := summaryStringForTest(t, full, "decision"); got != stored.Decision.Decision.Status || got != "resolved" {
		t.Fatalf("assess decision = %q, want %q", got, stored.Decision.Decision.Status)
	}
}

// adjudicatedConsultation writes an adjudicated before-phase consultation of
// the harness task through cw consult and returns its path.
func (h *summaryCLIHarness) adjudicatedConsultation() string {
	t := h.t
	t.Helper()
	fingerprint := h.load().Baseline.Fingerprint

	context := bytes.TrimSpace(h.execute(nil, "task", "context", "--task", h.task, "--phase", "before"))
	if !json.Valid(context) {
		t.Fatalf("task context did not print JSON: %q", context)
	}
	dir := t.TempDir()
	request, record := filepath.Join(dir, "request.json"), filepath.Join(dir, "record.json")
	decision, final := filepath.Join(dir, "decision.json"), filepath.Join(dir, "final.json")
	writeJSON := func(path string, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(request, map[string]any{
		"state": map[string]any{"task_context": json.RawMessage(context)},
		"questions": map[string]any{"q1": map[string]any{
			"type":         "choice",
			"instructions": "Choose whether source.txt supports the planned inspection.",
			"criteria": map[string]string{
				"a":                     "source.txt supports the planned inspection.",
				"insufficient_evidence": "The available evidence does not decide the question.",
			},
		}},
	})
	h.execute(nil, "consult", "--request", request, "--output", record, "--policy", shippedPolicyPath)
	writeJSON(decision, map[string]any{"questions": map[string]any{"q1": map[string]any{
		"status": "resolved", "choice": "a", "reason": "The first line of source.txt is the inspected declaration.",
		"evidence":                  []string{"source:source.txt:1-1@" + fingerprint},
		"verification":              []string{"observation:Read line 1 of source.txt."},
		"rejected_judgments":        map[string]any{},
		"investigations":            map[string]any{},
		"agreement_not_independent": false,
	}}})
	h.execute(nil, "consult", "--adjudicate", record, "--decision", decision, "--output", final)

	return final
}

// R8, R9 (consult).
func TestTaskSummaryFlagReportsAttachedConsultation(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = h.directoryOf(summaryStringForTest(t, parseSummaryLineForTest(t, "start", h.start(true),
		"task_id", "advisers", "requirements", "checks"), "task_id"))
	final := h.adjudicatedConsultation()

	consult := parseSummaryLineForTest(t, "consult",
		h.execute(nil, "task", "consult", "--task", h.task, "--phase", "before", "--input", final, "--summary"),
		"task_id", "phase", "decision_status", "providers")
	stored := h.load()
	attachment, ok := stored.Consultations["before"]
	if !ok {
		t.Fatal("consultation was not stored")
	}
	if summaryStringForTest(t, consult, "task_id") != h.load().ID || summaryStringForTest(t, consult, "phase") != "before" {
		t.Fatalf("consult summary identifies the wrong phase: %v", consult)
	}
	if got := summaryStringForTest(t, consult, "decision_status"); got != attachment.DecisionStatus || got == "" {
		t.Fatalf("consult decision_status = %q, want %q", got, attachment.DecisionStatus)
	}
	if got := summaryCompactForTest(t, consult["providers"]); got != "{}" || len(attachment.ProviderStatuses) != 0 {
		t.Fatalf("consult providers = %s with stored %v, want {}", got, attachment.ProviderStatuses)
	}
}

func wholeRecordForTest(t *testing.T, command string, output []byte) taskflow.Record {
	t.Helper()
	var members map[string]json.RawMessage
	if err := json.Unmarshal(output, &members); err != nil {
		t.Fatalf("%s did not print one JSON object: %v", command, err)
	}
	for _, member := range []string{"id", "directory", "revision", "plan", "baseline"} {
		if _, ok := members[member]; !ok {
			t.Fatalf("%s output lacks record member %q", command, member)
		}
	}
	var record taskflow.Record
	if err := json.Unmarshal(output, &record); err != nil {
		t.Fatal(err)
	}
	stored, err := taskflow.Load(record.Directory)
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != stored.ID || record.Directory != stored.Directory || record.Revision != stored.Revision {
		t.Fatalf("%s printed record %s revision %d, stored %s revision %d", command, record.ID, record.Revision, stored.ID, stored.Revision)
	}
	return record
}

// R8: without --summary the commands still print the whole record.
func TestTaskCommandsWithoutSummaryPrintWholeRecord(t *testing.T) {
	h := newSummaryCLIHarness(t)
	record := wholeRecordForTest(t, "start", h.start(false))
	h.task = record.Directory

	record = wholeRecordForTest(t, "assign", h.taskInput("assign", summaryCLIBrief(), false))
	if len(record.Assignments) != 1 {
		t.Fatal("assign output lacks the assignment")
	}
	record = wholeRecordForTest(t, "handoff", h.taskInput("handoff", summaryCLIResult(), false))
	if len(record.WorkerResults) != 1 {
		t.Fatal("handoff output lacks the result")
	}
	record = wholeRecordForTest(t, "accept", h.taskInput("accept", summaryCLIAcceptance(), false))
	if len(record.WorkerAcceptances) != 1 {
		t.Fatal("accept output lacks the acceptance")
	}
	h.runChecks()
	record = wholeRecordForTest(t, "assess", h.taskInput("assess", summaryCLIAssessment(h.fileSHA256(), []string{"zeta"}, false), false))
	if _, ok := record.Assessments["zeta"]; !ok {
		t.Fatal("assess output lacks the assessment")
	}
}

// Contract revision 2 additions.

const summaryCLIGitPlan = `{"title":"Run a planned source check","kind":"small","requirements":[` +
	`{"id":"behavior","description":"Confirm the available Git version","check_ids":["gitversion"]}],` +
	`"checks":[{"id":"gitversion","argv":["git","--version"],"timeout_seconds":30}]}`

func (h *summaryCLIHarness) runCheck(summary bool) []byte {
	h.t.Helper()
	args := []string{"task", "run", "--task", h.task, "--check", "gitversion"}
	// --summary is the default and changes nothing; --full prints the record.
	if summary {
		args = append(args, "--summary")
	} else {
		args = append(args, "--full", "--force")
	}
	return h.execute(nil, args...)
}

func lastCheckRunForTest(t *testing.T, record taskflow.Record, checkID string) taskflow.CheckRun {
	t.Helper()
	for i := len(record.Checks) - 1; i >= 0; i-- {
		if record.Checks[i].CheckID == checkID {
			return record.Checks[i]
		}
	}
	t.Fatalf("no stored run for %s", checkID)
	return taskflow.CheckRun{}
}

// R8, R9 (run), and the unchanged CheckRun output without --summary.
func TestTaskSummaryFlagReportsCheckRun(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.startPlan(summaryCLIGitPlan, false)).Directory

	run := parseSummaryLineForTest(t, "run", h.runCheck(true), "check_id", "run_id", "status", "exit_code", "log")
	stored := lastCheckRunForTest(t, h.load(), "gitversion")
	if got := summaryStringForTest(t, run, "check_id"); got != "gitversion" {
		t.Fatalf("run check_id = %q, want gitversion", got)
	}
	if got := summaryStringForTest(t, run, "run_id"); got != stored.ID || got == "" {
		t.Fatalf("run run_id = %q, want %q", got, stored.ID)
	}
	if got := summaryStringForTest(t, run, "status"); got != stored.Status || got == "" {
		t.Fatalf("run status = %q, want %q", got, stored.Status)
	}
	var exitCode int
	summaryDecodeForTest(t, run, "exit_code", &exitCode)
	if stored.ExitCode == nil || exitCode != *stored.ExitCode || exitCode != 0 {
		t.Fatalf("run exit_code = %d, want stored %v", exitCode, stored.ExitCode)
	}
	if stored.Failure != "" {
		t.Fatalf("passing run stored failure %q", stored.Failure)
	}
	// Oracle: the log is named relative to the task directory.
	if got := summaryStringForTest(t, run, "log"); got != stored.Log || stored.Log == "" || filepath.IsAbs(got) {
		t.Fatalf("run log = %q, want %q", got, stored.Log)
	}

	output := h.runCheck(false)
	var members map[string]json.RawMessage
	if err := json.Unmarshal(output, &members); err != nil {
		t.Fatalf("run without --summary did not print one JSON object: %v", err)
	}
	for _, member := range []string{"id", "check_id", "argv", "before", "log_sha256", "output_sha256"} {
		if _, ok := members[member]; !ok {
			t.Fatalf("run without --summary lacks CheckRun member %q", member)
		}
	}
	var printed taskflow.CheckRun
	if err := json.Unmarshal(output, &printed); err != nil {
		t.Fatal(err)
	}
	latest := lastCheckRunForTest(t, h.load(), "gitversion")
	if printed.ID != latest.ID || printed.ID == stored.ID || printed.Log != latest.Log {
		t.Fatalf("run without --summary printed %+v, want stored run %s", printed, latest.ID)
	}
}

// R8, R9 (non-empty handoff check_runs and accept findings) with R2 on finding and disposition evidence.
func TestTaskSummaryFlagReportsCheckRunsAndFindingDispositions(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.startPlan(summaryCLIGitPlan, false)).Directory

	brief := summaryCLIBrief()
	brief.CheckIDs = []string{"gitversion"}
	h.taskInput("assign", brief, false)
	h.runCheck(false)
	checkRun := lastCheckRunForTest(t, h.load(), "gitversion")

	result := summaryCLIResult()
	result["check_ids"] = []string{"gitversion"}
	result["findings"] = []any{map[string]any{
		"id": "first-line", "summary": "The first line names the inspected source.",
		"evidence": []any{summaryCLIUnhashedRef()},
	}}
	handoff := parseSummaryLineForTest(t, "handoff", h.taskInput("handoff", result, true),
		"task_id", "assignment_id", "result_sha256", "status", "check_runs")
	stored := h.load()
	if len(stored.WorkerResults) != 1 {
		t.Fatal("handoff was not stored")
	}
	bound := stored.WorkerResults[0]
	var checkRuns []string
	summaryDecodeForTest(t, handoff, "check_runs", &checkRuns)
	if !slices.Equal(checkRuns, bound.CheckRuns) || !slices.Equal(checkRuns, []string{checkRun.ID}) {
		t.Fatalf("handoff check_runs = %v, stored %v, want [%s]", checkRuns, bound.CheckRuns, checkRun.ID)
	}
	if got := summaryStringForTest(t, handoff, "result_sha256"); got != bound.SHA256 {
		t.Fatalf("handoff result_sha256 = %q, want %q", got, bound.SHA256)
	}
	findings := bound.Result.Findings
	if len(findings) != 1 || len(findings[0].Evidence) != 1 || findings[0].Evidence[0].SHA256 != h.fileSHA256() {
		t.Fatalf("finding evidence hash was not filled: %+v", findings)
	}

	acceptance := summaryCLIAcceptance()
	acceptance["check_ids"] = []string{"gitversion"}
	acceptance["findings"] = []any{map[string]any{
		"finding_id": "first-line", "status": "resolved",
		"reason":   "The first line was checked against the source.",
		"evidence": []any{summaryCLIUnhashedRef()},
	}}
	accept := parseSummaryLineForTest(t, "accept", h.taskInput("accept", acceptance, true),
		"task_id", "assignment_id", "status", "findings")
	stored = h.load()
	if len(stored.WorkerAcceptances) != 1 {
		t.Fatal("acceptance was not stored")
	}
	dispositions := stored.WorkerAcceptances[0].Acceptance.Findings
	var statuses map[string]string
	summaryDecodeForTest(t, accept, "findings", &statuses)
	if len(statuses) != 1 || len(dispositions) != 1 || statuses["first-line"] != "resolved" ||
		statuses[dispositions[0].FindingID] != dispositions[0].Status {
		t.Fatalf("accept findings = %v, stored %+v", statuses, dispositions)
	}
	if len(dispositions[0].Evidence) != 1 || dispositions[0].Evidence[0].SHA256 != h.fileSHA256() {
		t.Fatalf("disposition evidence hash was not filled: %+v", dispositions[0].Evidence)
	}
}
