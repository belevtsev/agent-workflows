package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Contract: contract-compact-output revision 1. Requirement IDs: DEF-UNCHANGED, DOC-SUMMARY,
// STATUS-SUMMARY, VERIFY-SUMMARY, SHOW-FIELD, CONSULT-SUMMARY, RUN-FAILURE.

// ---------------------------------------------------------------------------
// Shared helpers.

type compactOutcome struct {
	err            error
	stdout, stderr string
}

func (o compactOutcome) text() string {
	message := ""
	if o.err != nil {
		message = o.err.Error()
	}
	return message + "\n" + o.stdout + "\n" + o.stderr
}

// compactExecute runs cw through the summary harness and returns the error instead of failing.
func compactExecute(h *summaryCLIHarness, input []byte, args ...string) compactOutcome {
	h.t.Helper()
	stderr := new(bytes.Buffer)
	h.config.In = bytes.NewReader(input)
	h.config.Err = stderr
	h.out.Reset()
	err := Execute(h.t.Context(), args, h.config)
	return compactOutcome{err: err, stdout: h.out.String(), stderr: stderr.String()}
}

// compactLine requires exactly one newline-terminated line holding exactly one JSON object.
func compactLine(t *testing.T, command string, output []byte) map[string]json.RawMessage {
	t.Helper()
	text := string(output)
	if strings.TrimSpace(text) == "" || !strings.HasSuffix(text, "\n") || strings.Count(text, "\n") != 1 {
		t.Fatalf("%s: output is not exactly one newline-terminated line: %q", command, output)
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	var object map[string]json.RawMessage
	if err := decoder.Decode(&object); err != nil || object == nil {
		t.Fatalf("%s: output is not one JSON object (%v): %q", command, err, text)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("%s: output holds more than one JSON value: %q", command, text)
	}
	return object
}

// compactRequireKeys checks that every required member is present and, when allowed is non-nil,
// that no member outside allowed is present.
func compactRequireKeys(t *testing.T, command string, object map[string]json.RawMessage, required, allowed []string) {
	t.Helper()
	for _, key := range required {
		if _, ok := object[key]; !ok {
			t.Fatalf("%s: compact output lacks %q; members %v", command, key, slices.Sorted(maps.Keys(object)))
		}
	}
	if allowed == nil {
		return
	}
	for key := range object {
		if !slices.Contains(allowed, key) && !slices.Contains(required, key) {
			t.Fatalf("%s: compact output has unexpected member %q; members %v", command, key, slices.Sorted(maps.Keys(object)))
		}
	}
}

func compactExactKeys(t *testing.T, command string, object map[string]json.RawMessage, keys ...string) {
	t.Helper()
	got := slices.Sorted(maps.Keys(object))
	want := slices.Sorted(slices.Values(keys))
	if !slices.Equal(got, want) {
		t.Fatalf("%s: compact members = %v, want %v", command, got, want)
	}
}

func compactDecode(t *testing.T, raw json.RawMessage, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
}

func compactAny(t *testing.T, raw []byte) any {
	t.Helper()
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return value
}

// compactAt walks a generic JSON value by member names (string) and array indexes (int).
func compactAt(t *testing.T, value any, path ...any) any {
	t.Helper()
	for _, segment := range path {
		switch s := segment.(type) {
		case string:
			object, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("fixture path %v: %v is not an object", path, value)
			}
			if value, ok = object[s]; !ok {
				t.Fatalf("fixture path %v: no member %q", path, s)
			}
		case int:
			array, ok := value.([]any)
			if !ok || s >= len(array) {
				t.Fatalf("fixture path %v: no index %d", path, s)
			}
			value = array[s]
		}
	}
	return value
}

func compactStrings(t *testing.T, value any) []string {
	t.Helper()
	array, ok := value.([]any)
	if !ok {
		t.Fatalf("%v is not an array", value)
	}
	out := []string{}
	for _, element := range array {
		text, ok := element.(string)
		if !ok {
			t.Fatalf("%v is not a string", element)
		}
		out = append(out, text)
	}
	return out
}

// compactCollect gathers every value stored under key anywhere in a generic JSON value.
func compactCollect(value any, key string, found *[]any) {
	switch x := value.(type) {
	case map[string]any:
		for _, name := range slices.Sorted(maps.Keys(x)) {
			if name == key {
				*found = append(*found, x[name])
			}
			compactCollect(x[name], key, found)
		}
	case []any:
		for _, element := range x {
			compactCollect(element, key, found)
		}
	}
}

func compactCountString(value any, text string) int {
	switch x := value.(type) {
	case string:
		if x == text {
			return 1
		}
	case map[string]any:
		count := 0
		for _, element := range x {
			count += compactCountString(element, text)
		}
		return count
	case []any:
		count := 0
		for _, element := range x {
			count += compactCountString(element, text)
		}
		return count
	}
	return 0
}

// ---------------------------------------------------------------------------
// VERIFY-SUMMARY and DEF-UNCHANGED (verify).

func TestCompactVerifySummaryDropsOnlyFixedLimitations(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory

	full := compactExecute(h, nil, "task", "verify", "--full", "--task", h.task)
	if ExitCode(full.err) != 5 {
		t.Fatalf("verify of a fresh task should be incomplete (exit 5): %v (exit %d): %s", full.err, ExitCode(full.err), full.text())
	}
	var report map[string]any
	if err := json.Unmarshal([]byte(full.stdout), &report); err != nil {
		t.Fatalf("verify without --summary did not print one JSON object: %v: %q", err, full.stdout)
	}
	fullProblems, ok := report["problems"].([]any)
	if !ok || len(fullProblems) == 0 {
		t.Fatalf("full verification of an incomplete task lacks problems: %v", report["problems"])
	}
	fullLimitations := compactStrings(t, report["limitations"])
	fixed := taskflow.FixedVerifyLimitations()
	for _, limitation := range fixed {
		if !slices.Contains(fullLimitations, limitation) {
			t.Fatalf("full limitations %q lack fixed limitation %q", fullLimitations, limitation)
		}
	}
	want := []string{}
	for _, limitation := range fullLimitations {
		if !slices.Contains(fixed, limitation) {
			want = append(want, limitation)
		}
	}

	summary := compactExecute(h, nil, "task", "verify", "--task", h.task, "--summary")
	if ExitCode(summary.err) != ExitCode(full.err) {
		t.Fatalf("verify --summary exit %d, want %d as without --summary: %s", ExitCode(summary.err), ExitCode(full.err), summary.text())
	}
	line := compactLine(t, "task verify --summary", []byte(summary.stdout))
	compactExactKeys(t, "task verify --summary", line, "task_id", "complete", "problems", "limitations", "omitted_fixed_limitations")
	var taskID string
	compactDecode(t, line["task_id"], &taskID)
	if stored := h.load(); taskID != stored.ID || taskID == "" {
		t.Fatalf("verify task_id = %q, want %q", taskID, stored.ID)
	}
	var complete bool
	compactDecode(t, line["complete"], &complete)
	if complete {
		t.Fatal("verify --summary reports an incomplete task as complete")
	}
	// Oracle: the full problems, with the two "requirement X is not
	// satisfied" lines joined into one at the place of the first.
	var problems []any
	compactDecode(t, line["problems"], &problems)
	wantFull := []any{"requirement zeta is not satisfied", "requirement alpha is not satisfied", "Codex task decision is unresolved"}
	wantProblems := []any{"requirements zeta, alpha are not satisfied", "Codex task decision is unresolved"}
	if !reflect.DeepEqual(fullProblems, wantFull) || !reflect.DeepEqual(problems, wantProblems) {
		t.Fatalf("verify problems = %v, summary problems = %v; want %v and %v", fullProblems, problems, wantFull, wantProblems)
	}
	var limitations []string
	compactDecode(t, line["limitations"], &limitations)
	if !slices.Equal(limitations, want) {
		t.Fatalf("verify --summary limitations = %q, want %q (full minus fixed)", limitations, want)
	}
	var omitted int
	compactDecode(t, line["omitted_fixed_limitations"], &omitted)
	if omitted != len(fullLimitations)-len(want) || omitted != 4 {
		t.Fatalf("omitted_fixed_limitations = %d, want %d removed (4 for a normal task)", omitted, len(fullLimitations)-len(want))
	}
}

// The verify summary lists rejected blocker and major findings ahead of the
// problems, in their own member, and keeps the other limitations.
func TestCompactVerifySummaryListsRejectedFindingsFirst(t *testing.T) {
	rejected := "rejected finding f1 (blocker) of worker assignment review1: Source contradicts it."
	other := "worker assignment review1 finding f2 deferred: Later."
	findings := []taskflow.Finding{}
	for _, fixed := range taskflow.FixedVerifyLimitations() {
		findings = append(findings, taskflow.Finding{Code: taskflow.CodeFixedLimitation, Message: fixed})
	}
	findings = append(findings, taskflow.Finding{Code: taskflow.CodeDeferredFinding, Message: other}, taskflow.Finding{Code: taskflow.CodeRejectedFinding, Message: rejected})
	value := taskflow.Verification{TaskID: "task", Problems: []string{"a problem"}, LimitationFindings: findings}
	data, err := jsonv2.Marshal(summarizeVerification(value))
	if err != nil {
		t.Fatal(err)
	}
	var line map[string]any
	if err := json.Unmarshal(data, &line); err != nil {
		t.Fatal(err)
	}
	if got := compactStrings(t, line["rejected_findings"]); !slices.Equal(got, []string{rejected}) {
		t.Fatalf("rejected_findings = %q, want only %q", got, rejected)
	}
	if got := compactStrings(t, line["limitations"]); !slices.Equal(got, []string{other}) {
		t.Fatalf("limitations = %q, want only %q", got, other)
	}
	if strings.Index(string(data), `"rejected_findings"`) > strings.Index(string(data), `"problems"`) {
		t.Fatalf("rejected_findings is not printed before problems: %s", data)
	}
}

// The summaries select the fixed entries by code or by the producer's exported
// value, never by a copy of their text, so rewording one changes no summary.
// Oracle: a fixed limitation and a rejected finding reworded to text no
// summary has seen are still dropped and moved; the projectctx caveat, read
// from projectctx itself, is still dropped and counted.
func TestSummariesHideFixedEntriesWhateverTheirWording(t *testing.T) {
	reworded := "a reworded fixed limitation"
	rejected := "a reworded rejection of f1"
	other := "a task-specific limitation"
	value := taskflow.Verification{TaskID: "task", Problems: []string{}, LimitationFindings: []taskflow.Finding{
		{Code: taskflow.CodeFixedLimitation, Message: reworded},
		{Code: taskflow.CodeRejectedFinding, Message: rejected},
		{Code: taskflow.CodeTrace, Message: other},
	}}
	verify := summarizeVerification(value)
	if verify.OmittedFixedLimitations != 1 || !slices.Equal(verify.Limitations, []string{other}) || !slices.Equal(verify.RejectedFindings, []string{rejected}) {
		t.Fatalf("verify summary = %+v, want the reworded fixed limitation omitted, the rejection moved and only %q kept", verify, other)
	}

	report := doctor.DoctorReport{Checks: map[string]doctor.Check{}}
	report.Profile.Warnings = []string{projectctx.DirtyComparisonWarning}
	doctorLine := summarizeDoctor(report, false)
	if doctorLine.OmittedFixed != 1 || len(doctorLine.Warnings) != 0 || doctorLine.Status != "ok" {
		t.Fatalf("doctor summary = %+v, want the projectctx caveat omitted and counted", doctorLine)
	}
}

// ---------------------------------------------------------------------------
// SHOW-FIELD and DEF-UNCHANGED (show).

// compactFindArrayKey returns the top-level member whose array holds an element matching match.
func compactFindArrayKey(t *testing.T, shown map[string]any, what string, match func(map[string]any) bool) string {
	t.Helper()
	for _, key := range slices.Sorted(maps.Keys(shown)) {
		array, ok := shown[key].([]any)
		if !ok {
			continue
		}
		for _, element := range array {
			if object, ok := element.(map[string]any); ok && match(object) {
				return key
			}
		}
	}
	t.Fatalf("task show output has no top-level array holding %s", what)
	return ""
}

func compactMember(object map[string]any, path ...string) any {
	var value any = object
	for _, name := range path {
		next, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = next[name]
	}
	return value
}

func compactShowHarness(t *testing.T) (*summaryCLIHarness, map[string]any, taskflow.CheckRun, taskflow.CheckRun) {
	t.Helper()
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	h.taskInput("assign", summaryCLIBrief(), false)
	h.taskInput("handoff", summaryCLIResult(), false)
	for _, id := range []string{"version", "version", "again"} {
		if _, err := (taskflow.Env{}).RunCheck(t.Context(), h.task, id); err != nil {
			t.Fatal(err)
		}
	}
	stored := h.load()
	var first taskflow.CheckRun
	for _, run := range stored.Checks {
		if run.CheckID == "version" {
			first = run
			break
		}
	}
	last := lastCheckRunForTest(t, stored, "version")
	if first.ID == "" || first.ID == last.ID {
		t.Fatalf("fixture needs two distinct version runs: first %q last %q", first.ID, last.ID)
	}

	full := compactExecute(h, nil, "task", "show", "--full", "--task", h.task)
	if full.err != nil {
		t.Fatalf("task show: %v: %s", full.err, full.text())
	}
	// DEF-UNCHANGED: show without --field still prints the whole record.
	record := wholeRecordForTest(t, "show", []byte(full.stdout))
	if len(record.Checks) != len(stored.Checks) || len(record.Assignments) != 1 || len(record.WorkerResults) != 1 {
		t.Fatalf("task show without --field did not print the whole record: %d checks, %d assignments, %d results",
			len(record.Checks), len(record.Assignments), len(record.WorkerResults))
	}
	var shown map[string]any
	if err := json.Unmarshal([]byte(full.stdout), &shown); err != nil {
		t.Fatal(err)
	}
	return h, shown, first, last
}

func TestCompactShowFieldSelectsDottedPaths(t *testing.T) {
	h, shown, first, last := compactShowHarness(t)

	assignments := compactFindArrayKey(t, shown, "the assignment brief", func(element map[string]any) bool {
		return compactMember(element, "brief", "id") == "inspect"
	})
	results := compactFindArrayKey(t, shown, "the worker result", func(element map[string]any) bool {
		return compactMember(element, "result", "assignment_id") == "inspect"
	})
	checkRuns, ok := shown["checks"].([]any)
	if !ok {
		t.Fatalf("task show has no checks array: %v", shown["checks"])
	}
	var lastRun, firstRun any
	for _, element := range checkRuns {
		object, _ := element.(map[string]any)
		switch object["id"] {
		case last.ID:
			lastRun = element
		case first.ID:
			firstRun = element
		}
	}
	if lastRun == nil || firstRun == nil {
		t.Fatalf("task show checks lack runs %s and %s", first.ID, last.ID)
	}

	want := map[string]any{
		"id":                                compactAt(t, shown, "id"),
		"plan.title":                        compactAt(t, shown, "plan", "title"),
		"plan.requirements.1.description":   compactAt(t, shown, "plan", "requirements", 1, "description"),
		"plan.requirements.alpha.check_ids": compactAt(t, shown, "plan", "requirements", 1, "check_ids"),
		"plan.requirements.zeta":            compactAt(t, shown, "plan", "requirements", 0),
		"plan.checks.again.argv":            compactAt(t, shown, "plan", "checks", 1, "argv"),
		"checks.version":                    lastRun,
		"checks." + first.ID:                firstRun,
		assignments + ".inspect.brief.goal": compactAt(t, shown, assignments, 0, "brief", "goal"),
		results + ".inspect.result.summary": compactAt(t, shown, results, 0, "result", "summary"),
	}
	// Literal anchors so the oracle does not depend only on the default output.
	literal := map[string]any{
		"plan.title":                        "Summarize source-bound task state",
		"plan.requirements.1.description":   "Confirm the available Git version",
		"plan.requirements.alpha.check_ids": []any{"again"},
		"plan.checks.again.argv":            []any{"git", "--version"},
		assignments + ".inspect.brief.goal": "Inspect the selected source declaration",
		results + ".inspect.result.summary": "Inspected the selected source declaration",
	}
	for path, value := range literal {
		if !reflect.DeepEqual(want[path], value) {
			t.Fatalf("fixture: default output at %s = %v, want %v", path, want[path], value)
		}
	}
	if compactMember(lastRun.(map[string]any), "id") != last.ID {
		t.Fatal("fixture: last version run not found")
	}

	args := []string{"task", "show", "--task", h.task}
	for _, path := range slices.Sorted(maps.Keys(want)) {
		args = append(args, "--field", path)
	}
	outcome := compactExecute(h, nil, args...)
	if outcome.err != nil {
		t.Fatalf("task show --field: %v: %s", outcome.err, outcome.text())
	}
	line := compactLine(t, "task show --field", []byte(outcome.stdout))
	compactExactKeys(t, "task show --field", line, slices.Collect(maps.Keys(want))...)
	for path, value := range want {
		if got := compactAny(t, line[path]); !reflect.DeepEqual(got, value) {
			t.Fatalf("--field %s = %v, want %v", path, got, value)
		}
	}

	single := compactExecute(h, nil, "task", "show", "--task", h.task, "--field", "checks.version.id")
	if single.err != nil {
		t.Fatalf("task show --field checks.version.id: %v: %s", single.err, single.text())
	}
	one := compactLine(t, "task show --field", []byte(single.stdout))
	compactExactKeys(t, "task show --field", one, "checks.version.id")
	if got := compactAny(t, one["checks.version.id"]); got != last.ID {
		t.Fatalf("checks.version.id = %v, want the latest run %s (last match wins)", got, last.ID)
	}
}

func TestCompactShowFieldRejectsUnknownSegments(t *testing.T) {
	h, _, _, _ := compactShowHarness(t)
	for _, test := range []struct {
		name    string
		fields  []string
		mention []string
	}{
		{"unknown_member", []string{"plan.nosuchmember"}, []string{"nosuchmember", "title", "requirements"}},
		{"unknown_element_id", []string{"plan.requirements.gammaid"}, []string{"gammaid", "zeta", "alpha"}},
		{"index_out_of_range", []string{"plan.requirements.9"}, []string{"9"}},
		{"unknown_after_valid_field", []string{"id", "plan.nosuchmember"}, []string{"nosuchmember"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := []string{"task", "show", "--task", h.task}
			for _, field := range test.fields {
				args = append(args, "--field", field)
			}
			outcome := compactExecute(h, nil, args...)
			if ExitCode(outcome.err) != 1 {
				t.Fatalf("unknown segment is not exit 1: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
			}
			if outcome.stdout != "" {
				t.Fatalf("unknown segment printed stdout %q", outcome.stdout)
			}
			message := ""
			if outcome.err != nil {
				message = outcome.err.Error()
			}
			message += "\n" + outcome.stderr
			for _, text := range test.mention {
				if !strings.Contains(message, text) {
					t.Fatalf("error text does not mention %q: %s", text, message)
				}
			}
		})
	}
	for _, field := range []string{"", "plan..title", "plan.", ".plan"} {
		t.Run(fmt.Sprintf("usage_%q", field), func(t *testing.T) {
			outcome := compactExecute(h, nil, "task", "show", "--task", h.task, "--field", field)
			if ExitCode(outcome.err) != 2 {
				t.Fatalf("--field %q is not a usage error: %v (exit %d): %s", field, outcome.err, ExitCode(outcome.err), outcome.text())
			}
		})
	}
}

// ---------------------------------------------------------------------------
// RUN-FAILURE and DEF-UNCHANGED (run).

const compactGoMod = "module example.invalid/compactfixture\n\ngo 1.21\n"

func compactCheckHarness(t *testing.T, files map[string]string, argv []string) *summaryCLIHarness {
	t.Helper()
	h := newSummaryCLIHarness(t)
	repo, err := h.config.WorkingDir()
	if err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := json.Marshal(map[string]any{
		"title": "Run a failing source check", "kind": "small",
		"requirements": []any{map[string]any{"id": "behavior", "description": "Run the planned source check", "check_ids": []string{"probe"}}},
		"checks":       []any{map[string]any{"id": "probe", "argv": argv, "timeout_seconds": 60}},
	})
	if err != nil {
		t.Fatal(err)
	}
	h.task = wholeRecordForTest(t, "start", h.startPlan(string(plan), false)).Directory
	return h
}

// compactFailedRun runs the probe check with --summary, requires exit 5 and one line, and checks the
// common members against the stored run.
func compactFailedRun(t *testing.T, h *summaryCLIHarness) map[string]json.RawMessage {
	t.Helper()
	line := compactFailedRunLine(t, h)
	if _, ok := line["log_truncated"]; ok {
		t.Fatalf("log_truncated printed for a log below taskflow.MaxLogBytes: %s", line["log_truncated"])
	}
	return line
}

// compactFailedRunLine is compactFailedRun without the log_truncated expectation.
func compactFailedRunLine(t *testing.T, h *summaryCLIHarness) map[string]json.RawMessage {
	t.Helper()
	outcome := compactExecute(h, nil, "task", "run", "--task", h.task, "--check", "probe", "--summary")
	if ExitCode(outcome.err) != 5 {
		t.Fatalf("failed check run --summary is not exit 5: %v (exit %d): %s", outcome.err, ExitCode(outcome.err), outcome.text())
	}
	line := compactLine(t, "task run --summary", []byte(outcome.stdout))
	compactRequireKeys(t, "task run --summary", line, []string{"check_id", "run_id", "status", "exit_code", "log"}, nil)
	stored := lastCheckRunForTest(t, h.load(), "probe")
	if stored.ExitCode == nil || *stored.ExitCode == 0 {
		t.Fatalf("fixture check did not fail: %+v", stored)
	}
	var runID, status string
	compactDecode(t, line["run_id"], &runID)
	compactDecode(t, line["status"], &status)
	if runID != stored.ID || status != stored.Status {
		t.Fatalf("run summary run_id %q status %q, want stored %q %q", runID, status, stored.ID, stored.Status)
	}
	return line
}

func compactOptionalStrings(t *testing.T, line map[string]json.RawMessage, member string) []string {
	t.Helper()
	raw, ok := line[member]
	if !ok {
		return nil
	}
	var values []string
	compactDecode(t, raw, &values)
	return values
}

func compactCheckErrorLineBounds(t *testing.T, lines []string) {
	t.Helper()
	if len(lines) > 20 {
		t.Fatalf("error_lines has %d entries, want at most 20", len(lines))
	}
	total := 0
	for _, line := range lines {
		if len([]rune(line)) > 240 {
			t.Fatalf("error line of %d characters exceeds 240: %q", len([]rune(line)), line)
		}
		total += len(line)
	}
	if total > 4096 {
		t.Fatalf("error_lines total %d bytes exceeds 4096", total)
	}
}

func compactAnyContains(lines []string, text string) bool {
	return slices.ContainsFunc(lines, func(line string) bool { return strings.Contains(line, text) })
}

func TestCompactRunSummaryReportsFailingGoTests(t *testing.T) {
	h := compactCheckHarness(t, map[string]string{
		"go.mod": compactGoMod,
		"fixture_test.go": "package compactfixture\n\nimport \"testing\"\n\n" +
			"func TestAlphaFails(t *testing.T) { t.Fatal(\"alpha broke\") }\n\n" +
			"func TestBetaFails(t *testing.T) { t.Error(\"beta broke\") }\n\n" +
			"func TestGammaPasses(t *testing.T) {}\n",
	}, []string{"go", "test", "-count=2", "./..."})

	line := compactFailedRun(t, h)
	failing := compactOptionalStrings(t, line, "failing_tests")
	// -count=2 prints every failure twice; the names are de-duplicated.
	if got := slices.Sorted(slices.Values(failing)); !slices.Equal(got, []string{"TestAlphaFails", "TestBetaFails"}) {
		t.Fatalf("failing_tests = %q, want TestAlphaFails and TestBetaFails once each", failing)
	}
	errorLines := compactOptionalStrings(t, line, "error_lines")
	compactCheckErrorLineBounds(t, errorLines)
	for _, text := range []string{"fixture_test.go:5: alpha broke", "fixture_test.go:7: beta broke", "FAIL"} {
		if !compactAnyContains(errorLines, text) {
			t.Fatalf("error_lines %q lack %q", errorLines, text)
		}
	}
	if _, ok := line["log_tail"]; ok {
		t.Fatalf("log_tail printed although failing tests and error lines were found: %s", line["log_tail"])
	}

	// DEF-UNCHANGED: without --summary the run prints the CheckRun record and keeps exit 5.
	plain := compactExecute(h, nil, "task", "run", "--full", "--task", h.task, "--check", "probe")
	if ExitCode(plain.err) != 5 {
		t.Fatalf("failed check run without --summary is not exit 5: %v (exit %d)", plain.err, ExitCode(plain.err))
	}
	var members map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plain.stdout), &members); err != nil {
		t.Fatalf("run without --summary did not print one JSON object: %v: %q", err, plain.stdout)
	}
	for _, member := range []string{"id", "check_id", "argv"} {
		if _, ok := members[member]; !ok {
			t.Fatalf("run without --summary lacks CheckRun member %q", member)
		}
	}
	for _, member := range []string{"failing_tests", "error_lines", "log_tail"} {
		if _, ok := members[member]; ok {
			t.Fatalf("run without --summary printed compact member %q", member)
		}
	}
}

func TestCompactRunSummaryReportsCompileErrors(t *testing.T) {
	h := compactCheckHarness(t, map[string]string{
		"go.mod":    compactGoMod,
		"broken.go": "package compactfixture\n\nfunc broken() int {\n\treturn \"not an int\"\n}\n",
	}, []string{"go", "test", "./..."})

	line := compactFailedRun(t, h)
	if failing := compactOptionalStrings(t, line, "failing_tests"); len(failing) != 0 {
		t.Fatalf("failing_tests = %q for a build failure without test failures", failing)
	}
	errorLines := compactOptionalStrings(t, line, "error_lines")
	compactCheckErrorLineBounds(t, errorLines)
	if !compactAnyContains(errorLines, "broken.go:4:") {
		t.Fatalf("error_lines %q lack the compile error at broken.go:4", errorLines)
	}
	if _, ok := line["log_tail"]; ok {
		t.Fatalf("log_tail printed although error lines were found: %s", line["log_tail"])
	}
}

func TestCompactRunSummaryBoundsExcerpts(t *testing.T) {
	var source strings.Builder
	source.WriteString("package compactfixture\n\nimport (\n\t\"strings\"\n\t\"testing\"\n)\n\n")
	names := []string{}
	for index := range 30 {
		name := fmt.Sprintf("TestBound%02d", index)
		names = append(names, name)
		fmt.Fprintf(&source, "func %s(t *testing.T) { t.Error(strings.Repeat(\"y\", 300)) }\n\n", name)
	}
	h := compactCheckHarness(t, map[string]string{"go.mod": compactGoMod, "bound_test.go": source.String()},
		[]string{"go", "test", "-count=1", "./..."})

	line := compactFailedRun(t, h)
	failing := compactOptionalStrings(t, line, "failing_tests")
	if len(failing) != 20 {
		t.Fatalf("failing_tests has %d names from 30 failing tests, want 20: %q", len(failing), failing)
	}
	seen := map[string]bool{}
	for _, name := range failing {
		if seen[name] || !slices.Contains(names, name) {
			t.Fatalf("failing_tests holds a duplicate or unknown name %q: %q", name, failing)
		}
		seen[name] = true
	}
	errorLines := compactOptionalStrings(t, line, "error_lines")
	if len(errorLines) == 0 {
		t.Fatal("error_lines is empty although every test printed an error line")
	}
	compactCheckErrorLineBounds(t, errorLines)
}

// The total cap: 30 error-shaped lines of about 250 characters each, so 20 of them truncated to 240
// characters would still exceed 4096 bytes.
func TestCompactRunSummaryCapsErrorLinesTotal(t *testing.T) {
	var lines strings.Builder
	for index := range 30 {
		prefix := fmt.Sprintf("long%02d.go:%d:2: error: ", index, index+3)
		fmt.Fprintf(&lines, "%s%s\n", prefix, strings.Repeat("z", 250-len(prefix)))
	}
	h := compactCheckHarness(t, map[string]string{"errors.txt": lines.String()},
		[]string{"cat", "errors.txt", "compact-missing-input"})

	line := compactFailedRun(t, h)
	errorLines := compactOptionalStrings(t, line, "error_lines")
	if len(errorLines) == 0 {
		t.Fatal("error_lines is empty although the log holds 30 error lines")
	}
	compactCheckErrorLineBounds(t, errorLines)
}

func TestCompactRunSummaryFallsBackToLogTail(t *testing.T) {
	var rows strings.Builder
	for index := 1; index <= 15; index++ {
		fmt.Fprintf(&rows, "row-%02d\n\n", index)
	}
	h := compactCheckHarness(t, map[string]string{"rows.txt": rows.String()},
		[]string{"cat", "rows.txt", "compact-missing-input"})

	line := compactFailedRun(t, h)
	if failing := compactOptionalStrings(t, line, "failing_tests"); len(failing) != 0 {
		t.Fatalf("failing_tests = %q for a log without Go test failures", failing)
	}
	if errorLines := compactOptionalStrings(t, line, "error_lines"); len(errorLines) != 0 {
		t.Fatalf("error_lines = %q for a log without error lines", errorLines)
	}
	tail := compactOptionalStrings(t, line, "log_tail")
	if len(tail) != 10 {
		t.Fatalf("log_tail has %d lines, want the last 10 non-empty lines: %q", len(tail), tail)
	}
	for _, entry := range tail {
		if strings.TrimSpace(entry) == "" {
			t.Fatalf("log_tail holds an empty line: %q", tail)
		}
	}
	if !compactAnyContains(tail, "row-15") {
		t.Fatalf("log_tail %q lacks the last row", tail)
	}
	for index := 1; index <= 5; index++ {
		if row := fmt.Sprintf("row-%02d", index); compactAnyContains(tail, row) {
			t.Fatalf("log_tail %q holds %s, which is not among the last 10 non-empty lines", tail, row)
		}
	}
}

// Revision 2: the stored log keeps the first taskflow.MaxLogBytes bytes; a failed check whose output
// exceeds it reports log_truncated: true.
func TestCompactRunSummaryReportsTruncatedLog(t *testing.T) {
	limit := int(taskflow.MaxLogBytes)
	var rows strings.Builder
	for index := 0; rows.Len() <= limit+65536; index++ {
		fmt.Fprintf(&rows, "row-%07d\n", index)
	}
	h := compactCheckHarness(t, map[string]string{"large.txt": rows.String()},
		[]string{"cat", "large.txt", "compact-missing-input"})

	line := compactFailedRunLine(t, h)
	raw, ok := line["log_truncated"]
	if !ok {
		t.Fatalf("log_truncated absent although the output (%d bytes) exceeds %d", rows.Len(), limit)
	}
	var truncated bool
	compactDecode(t, raw, &truncated)
	if !truncated {
		t.Fatalf("log_truncated = %s, want true", raw)
	}
	compactCheckErrorLineBounds(t, compactOptionalStrings(t, line, "error_lines"))
	if tail := compactOptionalStrings(t, line, "log_tail"); len(tail) > 10 {
		t.Fatalf("log_tail has %d lines, want at most 10", len(tail))
	}
}

func TestCompactRunSummaryOfPassedRunHasNoExcerpts(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.startPlan(summaryCLIGitPlan, false)).Directory
	line := compactLine(t, "task run --summary", h.runCheck(true))
	for _, member := range []string{"failing_tests", "error_lines", "log_tail", "log_truncated"} {
		if _, ok := line[member]; ok {
			t.Fatalf("passed run --summary printed %q", member)
		}
	}
}

// ---------------------------------------------------------------------------
// DOC-SUMMARY and DEF-UNCHANGED (doctor).

const compactDirtyCaveat = projectctx.DirtyComparisonWarning

type compactDoctorFixture struct {
	repo, home, skills, codex string
}

func newCompactDoctorFixture(t *testing.T) compactDoctorFixture {
	t.Helper()
	f := compactDoctorFixture{repo: t.TempDir(), home: t.TempDir(), skills: t.TempDir(), codex: t.TempDir()}
	doctorWrite(t, f.repo, "go.mod", "module example.invalid/doctorfixture\n\ngo 1.21\n")
	doctorWrite(t, f.repo, "doctor.go", "package doctorfixture\n")
	doctorWrite(t, f.repo, "AGENTS.md", "# Agent instructions\n\nRun go test ./... before completion.\n")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"-c", "user.name=compact", "-c", "user.email=compact@example.invalid", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "fixture"},
	} {
		command := exec.Command("git", args...)
		command.Dir = f.repo
		command.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, output)
		}
	}
	for _, name := range []string{"task-orchestration", "production-plan", "code-review"} {
		doctorWrite(t, f.skills, name+"/SKILL.md", "fixture")
	}
	doctorWrite(t, f.codex, "config.toml", "[mcp_servers.gopls]\nenabled = true\n")
	return f
}

func (f compactDoctorFixture) run(t *testing.T, env map[string]string, extra ...string) compactOutcome {
	t.Helper()
	root, out := doctorFixture(t, Config{
		WorkingDir: func() (string, error) { return "", errors.New("--repo was ignored") },
		HomeDir:    func() (string, error) { return f.home, nil },
		Getenv:     fixtureEnvironment(env),
	})
	stderr := new(bytes.Buffer)
	root.SetErr(stderr)
	args := append([]string{"doctor", "--repo", f.repo, "--codex-home", f.codex, "--skills-home", f.skills}, extra...)
	root.SetArgs(args)
	err := root.ExecuteContext(t.Context())
	return compactOutcome{err: err, stdout: out.String(), stderr: stderr.String()}
}

var compactHex64 = regexp.MustCompile(`[0-9a-f]{64}`)

var compactCredentialNames = []string{"OPENAI_API_KEY", "SUBQ_API_KEY", "TYPESAFE_API_KEY"}

func TestCompactDoctorSummary(t *testing.T) {
	f := newCompactDoctorFixture(t)
	full := f.run(t, nil)
	summary := f.run(t, nil, "--summary")
	if ExitCode(summary.err) != ExitCode(full.err) {
		t.Fatalf("doctor --summary exit %d, want %d as without --summary: %s", ExitCode(summary.err), ExitCode(full.err), summary.text())
	}

	// DEF-UNCHANGED: the default report is still the full DoctorReport with every check.
	var report doctor.DoctorReport
	if err := jsonv2.Unmarshal([]byte(full.stdout), &report); err != nil {
		t.Fatalf("doctor without --summary is not a DoctorReport: %v", err)
	}
	if report.Checks["workflow_skills"].Status != "configured" || report.Checks["mcp_gopls"].Status != "configured" {
		t.Fatalf("fixture checks are not configured in the default report: %+v", report.Checks)
	}
	generic := compactAny(t, []byte(full.stdout))
	if top, _ := generic.(map[string]any); top["omitted_fixed_warnings"] != nil || top["problems"] != nil {
		t.Fatal("doctor without --summary printed compact members")
	}
	caveats := compactCountString(generic, compactDirtyCaveat)
	if caveats == 0 {
		t.Fatalf("fixture: the default report of a Git repository lacks the fixed dirty caveat: %s", full.stdout)
	}

	line := compactLine(t, "doctor --summary", []byte(summary.stdout))
	compactRequireKeys(t, "doctor --summary", line, []string{"status", "snapshot", "kinds", "contracts", "problems"}, nil)
	if _, ok := line["checks"]; ok {
		t.Fatal("doctor --summary printed every check")
	}
	if strings.Contains(summary.stdout, compactDirtyCaveat) {
		t.Fatal("doctor --summary listed the fixed dirty caveat")
	}
	if _, ok := line["live"]; ok {
		t.Fatal("doctor --summary printed live without --live")
	}

	// snapshot essentials equal the default report's snapshot.
	encoded, err := jsonv2.Marshal(report.Profile.Snapshot)
	if err != nil {
		t.Fatal(err)
	}
	snapshotWant, _ := compactAny(t, encoded).(map[string]any)
	var snapshot map[string]any
	compactDecode(t, line["snapshot"], &snapshot)
	if got := slices.Sorted(maps.Keys(snapshot)); !slices.Equal(got, []string{"branch", "dirty", "file_count", "fingerprint", "head"}) {
		t.Fatalf("snapshot members = %v", got)
	}
	for key, value := range snapshot {
		want, ok := snapshotWant[key]
		if !ok || !reflect.DeepEqual(value, want) {
			t.Fatalf("snapshot %s = %v, want default report value %v", key, value, want)
		}
	}
	if snapshot["fingerprint"] == "" || snapshot["head"] == "" {
		t.Fatalf("snapshot of a committed repository lacks fingerprint or head: %v", snapshot)
	}

	encoded, err = jsonv2.Marshal(report.Profile.Kinds)
	if err != nil {
		t.Fatal(err)
	}
	var kindsWant, kinds []string
	compactDecode(t, encoded, &kindsWant)
	compactDecode(t, line["kinds"], &kinds)
	if len(kinds) == 0 || !slices.Equal(kinds, kindsWant) {
		t.Fatalf("kinds = %q, want %q", kinds, kindsWant)
	}

	// contracts are the default report's contract paths, as plain strings.
	var found []any
	compactCollect(generic, "contracts", &found)
	if len(found) == 0 {
		t.Fatal("default report has no contracts member")
	}
	contractsWant := []string{}
	for _, element := range found[0].([]any) {
		switch x := element.(type) {
		case string:
			contractsWant = append(contractsWant, x)
		case map[string]any:
			path, _ := x["path"].(string)
			contractsWant = append(contractsWant, path)
		}
	}
	var contracts []string
	compactDecode(t, line["contracts"], &contracts)
	if len(contracts) == 0 || !slices.Equal(slices.Sorted(slices.Values(contracts)), slices.Sorted(slices.Values(contractsWant))) {
		t.Fatalf("contracts = %q, want the default report paths %q (AGENTS.md fixture)", contracts, contractsWant)
	}
	for _, contract := range contracts {
		if compactHex64.MatchString(contract) {
			t.Fatalf("contract %q carries a hash", contract)
		}
	}

	// problems hold exactly the required checks that are missing, degraded or
	// stale; optional_missing counts the optional ones, which are not listed.
	problemsWant := map[string]string{}
	optionalWant := 0
	for id, check := range report.Checks {
		if !slices.Contains([]string{"missing", "degraded", "stale"}, check.Status) {
			continue
		}
		if doctor.CheckClass(id) == doctor.ClassOptional {
			optionalWant++
		} else {
			problemsWant[id] = check.Status
		}
	}
	if optionalWant == 0 {
		t.Fatalf("fixture: no optional check is missing, degraded or stale: %+v", report.Checks)
	}
	var optionalMissing int
	compactDecode(t, line["optional_missing"], &optionalMissing)
	if optionalMissing != optionalWant {
		t.Fatalf("optional_missing = %d, want %d", optionalMissing, optionalWant)
	}
	if _, ok := line["optional"]; ok {
		t.Fatalf("doctor --summary listed the optional checks without --all: %s", line["optional"])
	}
	var problems map[string]map[string]any
	compactDecode(t, line["problems"], &problems)
	if problems == nil {
		t.Fatalf("problems = %s, want an object", line["problems"])
	}
	if got, want := slices.Sorted(maps.Keys(problems)), slices.Sorted(maps.Keys(problemsWant)); !slices.Equal(got, want) {
		t.Fatalf("problem checks = %v, want %v", got, want)
	}
	for id, entry := range problems {
		if keys := slices.Sorted(maps.Keys(entry)); !slices.Equal(keys, []string{"detail", "status"}) {
			t.Fatalf("problem %s members = %v, want detail and status", id, keys)
		}
		if entry["status"] != problemsWant[id] {
			t.Fatalf("problem %s status = %v, want %s", id, entry["status"], problemsWant[id])
		}
		if _, ok := entry["detail"].(string); !ok {
			t.Fatalf("problem %s detail = %v, want a string", id, entry["detail"])
		}
		encoded, err := jsonv2.Marshal(report.Checks[id])
		if err != nil {
			t.Fatal(err)
		}
		if detail, ok := compactAny(t, encoded).(map[string]any)["detail"]; ok && detail != entry["detail"] {
			t.Fatalf("problem %s detail = %v, want the default report's %v", id, entry["detail"], detail)
		}
	}

	// warnings: only non-fixed ones, absent when none; the fixed caveat is counted.
	var warningArrays []any
	compactCollect(generic, "warnings", &warningArrays)
	otherWarnings := []string{}
	for _, array := range warningArrays {
		elements, _ := array.([]any)
		for _, element := range elements {
			if text, ok := element.(string); ok && text != compactDirtyCaveat {
				otherWarnings = append(otherWarnings, text)
			}
		}
	}
	if raw, ok := line["warnings"]; ok {
		var warnings []string
		compactDecode(t, raw, &warnings)
		if len(warnings) == 0 {
			t.Fatal("doctor --summary printed an empty warnings list")
		}
		for _, warning := range warnings {
			if !slices.Contains(otherWarnings, warning) {
				t.Fatalf("summary warning %q is not a non-fixed default warning %q", warning, otherWarnings)
			}
		}
	} else if len(otherWarnings) != 0 {
		t.Fatalf("doctor --summary omitted non-fixed warnings %q", otherWarnings)
	}
	// Revision 2: the committed repository's profile.warnings holds the caveat once; the summary omits and
	// counts it, and prints no warnings member when it is the only warning.
	if profileWarnings := compactStrings(t, compactAt(t, generic, "profile", "warnings")); !slices.Contains(profileWarnings, compactDirtyCaveat) {
		t.Fatalf("fixture: default profile.warnings %q lack the fixed caveat", profileWarnings)
	}
	var omitted int
	compactDecode(t, line["omitted_fixed_warnings"], &omitted)
	if omitted != 1 {
		t.Fatalf("omitted_fixed_warnings = %d, want 1 for the omitted caveat (%d occurrences in the default report)", omitted, caveats)
	}
	if len(otherWarnings) == 0 {
		if _, ok := line["warnings"]; ok {
			t.Fatalf("doctor --summary printed warnings although the caveat is the only one: %s", line["warnings"])
		}
		var status string
		compactDecode(t, line["status"], &status)
		if (status == "ok") != (len(problems) == 0) || (status != "ok" && status != "attention") {
			t.Fatalf("status = %q with %d problems; want ok exactly when problems is empty (the caveat alone never causes attention)", status, len(problems))
		}
	}

	wantStatus := "ok"
	if _, hasWarnings := line["warnings"]; len(problemsWant) > 0 || hasWarnings {
		wantStatus = "attention"
	}
	var status string
	compactDecode(t, line["status"], &status)
	if status != wantStatus {
		t.Fatalf("status = %q, want %q", status, wantStatus)
	}

	// missing_credentials appears while adviser credentials are absent.
	var missing []string
	compactDecode(t, line["missing_credentials"], &missing)
	if len(missing) == 0 || !slices.IsSorted(missing) {
		t.Fatalf("missing_credentials = %q, want a sorted non-empty list", missing)
	}
	for _, name := range missing {
		if !slices.Contains(compactCredentialNames, name) {
			t.Fatalf("missing_credentials holds unknown key %q", name)
		}
	}
}

func TestCompactDoctorSummaryCredentialsAndLive(t *testing.T) {
	f := newCompactDoctorFixture(t)
	withCredentials := f.run(t, compactAllCredentials(), "--summary")
	if ExitCode(withCredentials.err) != ExitCode(f.run(t, compactAllCredentials()).err) {
		t.Fatalf("doctor --summary exit code differs: %s", withCredentials.text())
	}
	line := compactLine(t, "doctor --summary", []byte(withCredentials.stdout))
	if _, ok := line["missing_credentials"]; ok {
		t.Fatalf("missing_credentials printed with every credential set: %s", line["missing_credentials"])
	}
	if strings.Contains(withCredentials.stdout, "fixture-secret") {
		t.Fatal("doctor --summary printed a credential")
	}

	live := f.run(t, nil, "--live", "--timeout", "5s")
	liveSummary := f.run(t, nil, "--live", "--timeout", "5s", "--summary")
	if ExitCode(liveSummary.err) != ExitCode(live.err) {
		t.Fatalf("doctor --live --summary exit %d, want %d: %s", ExitCode(liveSummary.err), ExitCode(live.err), liveSummary.text())
	}
	liveLine := compactLine(t, "doctor --live --summary", []byte(liveSummary.stdout))
	if _, ok := liveLine["live"]; !ok {
		t.Fatalf("doctor --live --summary lacks live: %s", liveSummary.stdout)
	}
}

// A repository whose required checks all pass is ok, however many optional
// tools are absent. Oracle: with git and cw on PATH and the three workflow
// skills present, but no Codex MCP server for jbcontext, no Claude settings
// and no provider credential, --summary prints status ok, an empty problems
// object and optional_missing equal to the number of optional checks the full
// report marks missing, degraded or stale; --all lists exactly those checks
// under optional and changes neither status nor problems.
func TestCompactDoctorSummaryIsOkWithOnlyOptionalChecksMissing(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is needed for the fixture: %v", err)
	}
	tools := t.TempDir()
	doctorWrite(t, tools, "cw", "#!/bin/sh\nexit 0\n")
	if err := os.Chmod(filepath.Join(tools, "cw"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tools+string(os.PathListSeparator)+filepath.Dir(git))
	f := newCompactDoctorFixture(t)

	var report doctor.DoctorReport
	if err := jsonv2.Unmarshal([]byte(f.run(t, nil).stdout), &report); err != nil {
		t.Fatalf("doctor without --summary is not a DoctorReport: %v", err)
	}
	optionalWant := []string{}
	for id, check := range report.Checks {
		if !slices.Contains([]string{"missing", "degraded", "stale"}, check.Status) {
			continue
		}
		if doctor.CheckClass(id) != doctor.ClassOptional {
			t.Fatalf("fixture: required check %s is %s: %s", id, check.Status, check.Detail)
		}
		optionalWant = append(optionalWant, id)
	}
	if len(optionalWant) == 0 {
		t.Fatalf("fixture: no optional check is missing: %+v", report.Checks)
	}

	for _, extra := range [][]string{{"--summary"}, {"--summary", "--all"}} {
		outcome := f.run(t, nil, extra...)
		if outcome.err != nil {
			t.Fatalf("doctor %v: %s", extra, outcome.text())
		}
		line := compactLine(t, fmt.Sprintf("doctor %v", extra), []byte(outcome.stdout))
		var status string
		var problems map[string]any
		var optionalMissing int
		compactDecode(t, line["status"], &status)
		compactDecode(t, line["problems"], &problems)
		compactDecode(t, line["optional_missing"], &optionalMissing)
		if status != "ok" || problems == nil || len(problems) != 0 {
			t.Fatalf("doctor %v status = %q, problems = %s; want ok and {}: %s", extra, status, line["problems"], outcome.stdout)
		}
		if optionalMissing != len(optionalWant) {
			t.Fatalf("doctor %v optional_missing = %d, want %d (%v)", extra, optionalMissing, len(optionalWant), optionalWant)
		}
		var optional map[string]any
		if raw, ok := line["optional"]; ok {
			compactDecode(t, raw, &optional)
		}
		listed := slices.Sorted(maps.Keys(optional))
		want := []string{}
		if len(extra) == 2 {
			want = slices.Sorted(slices.Values(optionalWant))
		}
		if !slices.Equal(listed, want) {
			t.Fatalf("doctor %v optional = %v, want %v", extra, listed, want)
		}
	}
	if outcome := f.run(t, nil, "--all"); ExitCode(outcome.err) != ExitUsage {
		t.Fatalf("doctor --all without --summary exit %d, want %d: %s", ExitCode(outcome.err), ExitUsage, outcome.text())
	}
}

// ---------------------------------------------------------------------------
// CONSULT-SUMMARY and DEF-UNCHANGED (consult).

const compactStateMarker = "compact-request-state-marker"

// compactWithoutTiming drops timing members so two records of the same inputs compare equal.
func compactWithoutTiming(value any) any {
	switch x := value.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, element := range x {
			if key == "duration_seconds" || strings.HasSuffix(key, "_at") || strings.Contains(key, "time") {
				continue
			}
			out[key] = compactWithoutTiming(element)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for index, element := range x {
			out[index] = compactWithoutTiming(element)
		}
		return out
	}
	return value
}

// compactConsultPair runs the same consult invocation without and with --summary against the same
// output path (moving the first record aside) and returns both outcomes and record bytes.
func compactConsultPair(t *testing.T, output string, args ...string) (plain, summary compactOutcome, plainData, summaryData []byte) {
	t.Helper()
	run := func(extra ...string) compactOutcome {
		outcome := executeConsult(t, fixtureEnvironment(nil), append(append(slices.Clone(args), "--output", output), extra...)...)
		return compactOutcome(outcome)
	}
	plain = run()
	var err error
	if plainData, err = os.ReadFile(output); err != nil {
		t.Fatalf("consult without --summary wrote no record: %v: %s", err, plain.text())
	}
	if err := os.Remove(output); err != nil {
		t.Fatal(err)
	}
	summary = run("--summary")
	if summaryData, err = os.ReadFile(output); err != nil {
		t.Fatalf("consult --summary wrote no record: %v: %s", err, summary.text())
	}
	if ExitCode(summary.err) != ExitCode(plain.err) {
		t.Fatalf("consult --summary exit %d, want %d as without --summary: %s", ExitCode(summary.err), ExitCode(plain.err), summary.text())
	}
	plainRecord, summaryRecord := compactAny(t, plainData), compactAny(t, summaryData)
	if !reflect.DeepEqual(compactWithoutTiming(plainRecord), compactWithoutTiming(summaryRecord)) {
		t.Fatalf("consult --summary wrote a different record:\n%s\nwant\n%s", summaryData, plainData)
	}
	// DEF-UNCHANGED: without --summary consult keeps its human line rather than the JSON view.
	if strings.TrimSpace(plain.stdout+plain.stderr) == "" || strings.Contains(plain.stdout, `"final_decision"`) {
		t.Fatalf("consult without --summary changed its output: %q", plain.stdout)
	}
	return plain, summary, plainData, summaryData
}

var compactConsultAllowed = []string{"record", "status", "request", "providers", "comparison", "final_decision"}

// compactConsultLine checks the members shared by both consult modes against the written record.
func compactConsultLine(t *testing.T, outcome compactOutcome, output string, data []byte) (map[string]json.RawMessage, map[string]any) {
	t.Helper()
	line := compactLine(t, "consult --summary", []byte(outcome.stdout))
	compactRequireKeys(t, "consult --summary", line, []string{"record", "status", "request", "providers", "final_decision"}, compactConsultAllowed)
	for _, text := range []string{compactStateMarker, "Does the cited source support the claim?"} {
		if strings.Contains(outcome.stdout, text) {
			t.Fatalf("consult --summary printed request content %q", text)
		}
	}
	record, _ := compactAny(t, data).(map[string]any)
	var recordPath string
	compactDecode(t, line["record"], &recordPath)
	if !samePath(recordPath, output) {
		t.Fatalf("record = %q, want %q", recordPath, output)
	}
	var status string
	compactDecode(t, line["status"], &status)
	if status != record["status"] {
		t.Fatalf("status = %q, want record status %v", status, record["status"])
	}
	var request map[string]any
	compactDecode(t, line["request"], &request)
	if request["sha256"] != record["request_sha256"] || record["request_sha256"] == nil {
		t.Fatalf("request.sha256 = %v, want record request_sha256 %v", request["sha256"], record["request_sha256"])
	}
	var providers map[string]map[string]any
	compactDecode(t, line["providers"], &providers)
	recordProviders, _ := record["providers"].(map[string]any)
	if got, want := slices.Sorted(maps.Keys(providers)), slices.Sorted(maps.Keys(recordProviders)); !slices.Equal(got, want) {
		t.Fatalf("providers = %v, want record providers %v", got, want)
	}
	for id, entry := range providers {
		recordEntry, _ := recordProviders[id].(map[string]any)
		if entry["status"] != recordEntry["status"] || entry["requested_model"] != recordEntry["requested_model"] || entry["status"] == nil {
			t.Fatalf("provider %s = %v, want record %v", id, entry, recordEntry)
		}
		for key := range entry {
			if !slices.Contains([]string{"status", "requested_model", "resolved_model", "answers", "error"}, key) {
				t.Fatalf("provider %s has member %q", id, key)
			}
		}
	}
	validated, err := consult.ValidateRecord(data)
	if err != nil {
		t.Fatalf("record written with --summary does not validate: %v", err)
	}
	var final map[string]any
	compactDecode(t, line["final_decision"], &final)
	if final["status"] != validated.DecisionStatus {
		t.Fatalf("final_decision.status = %v, want %s", final["status"], validated.DecisionStatus)
	}
	return line, request
}

func TestCompactConsultSummary(t *testing.T) {
	directory := t.TempDir()
	policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
	requestText := fixtureRequest(`"` + compactStateMarker + `"`)
	request := writeFixtureFile(t, directory, "request.json", requestText)
	output := filepath.Join(directory, "record.json")

	_, summary, _, data := compactConsultPair(t, output, "--request", request, "--policy", policy)
	if summary.err != nil {
		t.Fatalf("consult --summary without advisers is not exit 0: %v: %s", summary.err, summary.text())
	}
	_, requestMember := compactConsultLine(t, summary, output, data)
	if file, _ := requestMember["file"].(string); !samePath(file, request) {
		t.Fatalf("request.file = %v, want %s", requestMember["file"], request)
	}
	digest := sha256.Sum256([]byte(requestText))
	if requestMember["sha256"] != hex.EncodeToString(digest[:]) {
		t.Fatalf("request.sha256 = %v, want the request file digest", requestMember["sha256"])
	}

	// The record written with --summary is still accepted by --adjudicate, with and without --summary.
	for _, test := range []struct {
		name   string
		items  map[string]any
		exit   int
		status string
	}{
		{"resolved", map[string]any{"claim": resolvedItem("supported", []string{"source:docs/claim.md:3-5@0123abc"}, []string{"check:fixture-check@run-1"})}, 0, "resolved"},
		{"unresolved", map[string]any{"claim": unresolvedItem()}, 5, "unresolved"},
	} {
		t.Run("adjudicate_"+test.name, func(t *testing.T) {
			scratch := t.TempDir()
			decision := decisionFixture(t, scratch, test.items)
			final := filepath.Join(scratch, "final.json")
			_, adjudicated, _, finalData := compactConsultPair(t, final, "--adjudicate", output, "--decision", decision)
			if ExitCode(adjudicated.err) != test.exit {
				t.Fatalf("adjudicate --summary exit %d, want %d: %s", ExitCode(adjudicated.err), test.exit, adjudicated.text())
			}
			line, requestMember := compactConsultLine(t, adjudicated, final, finalData)
			if string(bytes.TrimSpace(compactMustRaw(t, line, "request", "file"))) != "null" {
				t.Fatalf("adjudicate request.file = %v, want null", requestMember["file"])
			}
			if source, _ := requestMember["source_record"].(string); !samePath(source, output) {
				t.Fatalf("adjudicate request.source_record = %v, want %s", requestMember["source_record"], output)
			}
			var finalDecision map[string]any
			compactDecode(t, line["final_decision"], &finalDecision)
			if finalDecision["status"] != test.status {
				t.Fatalf("final_decision.status = %v, want %s", finalDecision["status"], test.status)
			}
		})
	}
}

// compactMustRaw returns the raw JSON of member.child, failing when either is absent.
func compactMustRaw(t *testing.T, line map[string]json.RawMessage, member, child string) json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	compactDecode(t, line[member], &object)
	raw, ok := object[child]
	if !ok {
		t.Fatalf("%s lacks %q: %s", member, child, line[member])
	}
	return raw
}

func TestCompactConsultSummaryPrintsNothingOnInvalidInput(t *testing.T) {
	directory := t.TempDir()
	policy := writePolicyFixture(t, filepath.Join(directory, "policy"), 3, nil)
	valid := writeFixtureFile(t, directory, "valid.json", fixtureRequest(`"`+compactStateMarker+`"`))

	t.Run("invalid_request", func(t *testing.T) {
		invalid := writeFixtureFile(t, t.TempDir(), "request.json", fixtureRequest(`{"task_context":{"task_id":"fixture"}}`))
		output := filepath.Join(t.TempDir(), "record.json")
		outcome := executeConsult(t, fixtureEnvironment(nil), "--request", invalid, "--output", output, "--policy", policy, "--summary")
		if ExitCode(outcome.err) != 1 || outcome.stdout != "" {
			t.Fatalf("invalid request --summary: exit %d stdout %q, want exit 1 and no stdout", ExitCode(outcome.err), outcome.stdout)
		}
		requireNotWritten(t, output)
	})
	t.Run("existing_output", func(t *testing.T) {
		output := writeFixtureFile(t, t.TempDir(), "record.json", "existing")
		outcome := executeConsult(t, fixtureEnvironment(nil), "--request", valid, "--output", output, "--policy", policy, "--summary")
		if ExitCode(outcome.err) != 1 || outcome.stdout != "" {
			t.Fatalf("existing output --summary: exit %d stdout %q, want exit 1 and no stdout", ExitCode(outcome.err), outcome.stdout)
		}
		if data, err := os.ReadFile(output); err != nil || string(data) != "existing" {
			t.Fatalf("existing output was replaced: %q %v", data, err)
		}
	})
	t.Run("existing_adjudicate_output", func(t *testing.T) {
		record := filepath.Join(t.TempDir(), "record.json")
		if outcome := executeConsult(t, fixtureEnvironment(nil), "--request", valid, "--output", record, "--policy", policy); outcome.err != nil {
			t.Fatalf("fixture record: %v: %s", outcome.err, outcome.text())
		}
		scratch := t.TempDir()
		decision := decisionFixture(t, scratch, map[string]any{"claim": unresolvedItem()})
		output := writeFixtureFile(t, scratch, "final.json", "existing")
		outcome := executeConsult(t, fixtureEnvironment(nil), "--adjudicate", record, "--decision", decision, "--output", output, "--summary")
		if ExitCode(outcome.err) != 1 || outcome.stdout != "" {
			t.Fatalf("existing adjudicate output --summary: exit %d stdout %q, want exit 1 and no stdout", ExitCode(outcome.err), outcome.stdout)
		}
	})
}

func compactAllCredentials() map[string]string {
	return map[string]string{
		"TYPESAFE_API_KEY": "fixture-secret-jev", "SUBQ_API_KEY": "fixture-secret-subq", "OPENAI_API_KEY": "fixture-secret-openai",
	}
}

// ---------------------------------------------------------------------------
// The summary is the default output of the task commands that change state.

// compactJSONLineOf is what writeJSON prints for value: the bytes a task
// command prints for it.
func compactJSONLineOf(t *testing.T, value any) string {
	t.Helper()
	var line bytes.Buffer
	if err := writeJSON(&line, value); err != nil {
		t.Fatal(err)
	}
	return line.String()
}

// compactOutputMode is how a task command is asked for its output.
type compactOutputMode struct {
	name  string
	flags []string
}

// compactTaskFlow runs start, assign, handoff, accept, run, assess, verify and
// consult on a fresh task in one output mode, and calls want after each with
// the command and what it printed.
func compactTaskFlow(t *testing.T, mode compactOutputMode, want func(h *summaryCLIHarness, command, printed string)) {
	t.Helper()
	h := newSummaryCLIHarness(t)
	run := func(command string, input []byte, args ...string) string {
		t.Helper()
		args = append(append([]string{"task", command}, args...), mode.flags...)
		outcome := compactExecute(h, input, args...)
		// A fresh task's verification is incomplete: exit 5 with output.
		if outcome.err != nil && (command != "verify" || ExitCode(outcome.err) != 5) {
			t.Fatalf("cw %s: %s", strings.Join(args, " "), outcome.text())
		}
		return outcome.stdout
	}
	inputRun := func(command string, input any) string {
		t.Helper()
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		return run(command, encoded, "--task", h.task, "--input", "-")
	}

	printed := run("start", []byte(summaryCLITestPlan), "--plan", "-", "--policy", shippedPolicyPath)
	h.task = compactStartedTask(h, printed)
	want(h, "start", printed)
	want(h, "assign", inputRun("assign", summaryCLIBrief()))
	want(h, "handoff", inputRun("handoff", summaryCLIResult()))
	want(h, "accept", inputRun("accept", summaryCLIAcceptance()))
	want(h, "run", run("run", nil, "--task", h.task, "--check", "version"))
	run("run", nil, "--task", h.task, "--check", "again")
	want(h, "assess", inputRun("assess", summaryCLIAssessment(h.fileSHA256(), []string{"zeta"}, false)))
	want(h, "verify", run("verify", nil, "--task", h.task))
	want(h, "consult", run("consult", nil, "--task", h.task, "--phase", "before", "--input", h.adjudicatedConsultation()))
}

// compactStartedTask is the task directory start printed: the record names it
// as "directory", and the summary names the task by "task_id", which show
// resolves to the directory.
func compactStartedTask(h *summaryCLIHarness, printed string) string {
	t := h.t
	t.Helper()
	var members struct {
		TaskID    string `json:"task_id"`
		Directory string `json:"directory"`
	}
	if err := json.Unmarshal([]byte(printed), &members); err != nil {
		t.Fatalf("start did not print one JSON object: %v: %q", err, printed)
	}
	if members.Directory != "" {
		return members.Directory
	}
	if members.TaskID == "" {
		t.Fatalf("start printed neither a task directory nor a task ID: %q", printed)
	}
	return h.directoryOf(members.TaskID)
}

// compactFormerSummary is what command printed with --summary before the
// summary became the default: the summary view of the record it left.
func compactFormerSummary(t *testing.T, h *summaryCLIHarness, command string) any {
	t.Helper()
	record := h.load()
	switch command {
	case "start":
		return startSummary(record)
	case "assign":
		return assignSummary(record, "inspect")
	case "handoff":
		return handoffSummary(record, "inspect")
	case "accept":
		return acceptSummary(record, "inspect")
	case "run":
		return runSummary(h.task, lastCheckRunForTest(t, record, "version"))
	case "assess":
		return assessSummary(record, summaryCLIAssessment(h.fileSHA256(), []string{"zeta"}, false))
	case "consult":
		return consultSummary(record, "before")
	}
	t.Fatalf("no former summary for %s", command)
	return nil
}

// Oracle: without a flag each command prints, byte for byte, what it printed
// with --summary before (the summary view of the record it left; for verify,
// the summary of the verification it produces), and --summary, now a
// no-op, prints the same bytes. add-checks is covered by aclTestSummary.
func TestTaskCommandsPrintTheSummaryByDefault(t *testing.T) {
	for _, mode := range []compactOutputMode{{"default", nil}, {"--summary", []string{"--summary"}}} {
		t.Run(mode.name, func(t *testing.T) {
			compactTaskFlow(t, mode, func(h *summaryCLIHarness, command, printed string) {
				t.Helper()
				var want string
				if command == "verify" {
					// The finding codes the summary selects by are not in the
					// JSON, so verify again in process on the same source.
					verification, err := taskEnv(h.config).Verify(t.Context(), h.task)
					if verification.TaskID == "" {
						t.Fatalf("verify produced no verification: %v", err)
					}
					want = compactJSONLineOf(t, summarizeVerification(verification))
				} else {
					want = compactJSONLineOf(t, compactFormerSummary(t, h, command))
				}
				if printed != want {
					t.Errorf("%s (%s) printed\n%s\nwant the former --summary output\n%s", command, mode.name, printed, want)
				}
			})
		})
	}
}

// Oracle: with --full each command prints, byte for byte, what it printed
// without a flag before: the whole record it left, for run the whole check
// run, and for verify the whole verification it stored.
func TestTaskCommandsPrintTheWholeRecordWithFull(t *testing.T) {
	compactTaskFlow(t, compactOutputMode{"--full", []string{"--full"}}, func(h *summaryCLIHarness, command, printed string) {
		t.Helper()
		record := h.load()
		var want string
		switch command {
		case "run":
			want = compactJSONLineOf(t, lastCheckRunForTest(t, record, "version"))
		case "verify":
			if record.Verification == nil {
				t.Fatalf("verify --full stored no verification; printed %q", printed)
			}
			want = compactJSONLineOf(t, *record.Verification)
		default:
			want = compactJSONLineOf(t, record)
		}
		if printed != want {
			t.Errorf("%s --full printed\n%s\nwant the former default output\n%s", command, printed, want)
		}
	})
}
