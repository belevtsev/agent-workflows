package cli

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Oracle: problems that differ only in their requirement, assignment or check
// ID join into one line at the place of the first, check runs join only with
// runs of the same status, other problems keep their text and order, and the
// summary lists 20 lines and counts the rest.
func TestVerifySummaryGroupsAndCapsProblems(t *testing.T) {
	problems := []string{
		"requirement r1 is not satisfied",
		"worker assignment a1 is not completed",
		"requirement r2 is not satisfied",
		"latest run of c1 is failed",
		"latest run of c2 is timed_out",
		"latest run of c3 is failed",
		"requirement r3: evidence names no file",
	}
	for i := range 25 {
		problems = append(problems, fmt.Sprintf("other problem %d", i))
	}
	want := []string{
		"requirements r1, r2 are not satisfied",
		"worker assignment a1 is not completed",
		"latest runs of c1, c3 are failed",
		"latest run of c2 is timed_out",
		"requirement r3: evidence names no file",
	}
	for i := range 15 {
		want = append(want, fmt.Sprintf("other problem %d", i))
	}
	summary := summarizeVerification(taskflow.Verification{TaskID: "task", Problems: problems})
	if !slices.Equal(summary.Problems, want) || summary.OmittedProblems != 10 {
		t.Fatalf("summary problems = %q with %d omitted, want %q with 10 omitted", summary.Problems, summary.OmittedProblems, want)
	}
	line := compactJSONLineOf(t, summary)
	if !strings.Contains(line, `"omitted_problems":10`) {
		t.Fatalf("summary line does not count the omitted problems: %s", line)
	}

	// No problem past the cap: nothing is counted, and the member is omitted.
	summary = summarizeVerification(taskflow.Verification{TaskID: "task", Problems: nil})
	if summary.Problems == nil || len(summary.Problems) != 0 || strings.Contains(compactJSONLineOf(t, summary), "omitted_problems") {
		t.Fatalf("summary of no problems = %#v", summary)
	}
}

// boundedShow runs cw task show with args and decodes its one JSON line.
func boundedShow(t *testing.T, h *summaryCLIHarness, args ...string) map[string]any {
	t.Helper()
	outcome := compactExecute(h, nil, append([]string{"task", "show", "--task", h.task}, args...)...)
	if outcome.err != nil {
		t.Fatalf("task show %v: %s", args, outcome.text())
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(outcome.stdout), &value); err != nil || strings.Count(outcome.stdout, "\n") != 1 {
		t.Fatalf("task show %v did not print one JSON line: %v: %q", args, err, outcome.stdout)
	}
	return value
}

// Oracle: without --field or --full, show prints the task's state (each
// requirement's assessment, each check's latest run, the revision) and the
// sorted top-level members of the record that --full prints; more runs of
// the same check change only the revision. --full with --field is a usage
// error.
func TestTaskShowPrintsTheSummaryAndFieldList(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	h.execute(nil, "task", "run", "--force", "--task", h.task, "--check", "version")

	record := boundedShow(t, h, "--full")
	want := map[string]any{
		"task_id":      h.load().ID,
		"title":        "Summarize source-bound task state",
		"kind":         "small",
		"revision":     float64(h.load().Revision),
		"requirements": map[string]any{"zeta": "unassessed", "alpha": "unassessed"},
		"checks":       map[string]any{"version": "passed", "again": "not_run"},
		"fields":       toAny(slices.Sorted(maps.Keys(record))),
	}
	first := boundedShow(t, h)
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("task show = %#v\nwant %#v", first, want)
	}

	for range 4 {
		h.execute(nil, "task", "run", "--force", "--task", h.task, "--check", "version")
	}
	later := boundedShow(t, h)
	if later["revision"] == first["revision"] {
		t.Fatal("setup: the runs stored no revision")
	}
	later["revision"] = first["revision"]
	if !reflect.DeepEqual(later, first) {
		t.Fatalf("task show after more runs of one check = %#v, want %#v apart from the revision", later, first)
	}

	both := compactExecute(h, nil, "task", "show", "--task", h.task, "--full", "--field", "id")
	if ExitCode(both.err) != 2 || both.stdout != "" {
		t.Fatalf("task show --full --field: exit %d stdout %q, want a usage error", ExitCode(both.err), both.stdout)
	}
}

func toAny(values []string) []any {
	result := make([]any, len(values))
	for i, value := range values {
		result[i] = value
	}
	return result
}

// boundedTimeline runs cw task timeline with args and decodes its JSON.
func boundedTimeline(t *testing.T, h *summaryCLIHarness, args ...string) taskflow.Timeline {
	t.Helper()
	outcome := compactExecute(h, nil, append([]string{"task", "timeline", "--task", h.task}, args...)...)
	if outcome.err != nil {
		t.Fatalf("task timeline %v: %s", args, outcome.text())
	}
	var timeline taskflow.Timeline
	if err := json.Unmarshal([]byte(outcome.stdout), &timeline); err != nil {
		t.Fatalf("task timeline %v: %v: %q", args, err, outcome.stdout)
	}
	return timeline
}

// Oracle: the timeline lists the last 20 entries by default and the last N
// with --last N, which are the tail of what --last 0 lists, and counts the
// earlier ones as omitted_entries, in JSON and in the --summary task line; a
// negative N is a usage error.
func TestTaskTimelineListsTheLastEntries(t *testing.T) {
	h := newSummaryCLIHarness(t)
	h.task = wholeRecordForTest(t, "start", h.start(false)).Directory
	for range 22 {
		h.execute(nil, "task", "run", "--force", "--task", h.task, "--check", "version")
	}

	every := boundedTimeline(t, h, "--last", "0")
	if len(every.Entries) != 22 || every.OmittedEntries != 0 {
		t.Fatalf("--last 0 lists %d entries with %d omitted, want 22 and 0", len(every.Entries), every.OmittedEntries)
	}
	for _, test := range []struct {
		args []string
		keep int
	}{{nil, 20}, {[]string{"--last", "2"}, 2}, {[]string{"--last", "30"}, 22}} {
		got := boundedTimeline(t, h, test.args...)
		if !reflect.DeepEqual(got.Entries, every.Entries[22-test.keep:]) || got.OmittedEntries != 22-test.keep {
			t.Errorf("timeline %v lists %d entries with %d omitted, want the last %d with %d omitted", test.args, len(got.Entries), got.OmittedEntries, test.keep, 22-test.keep)
		}
	}

	summary := compactExecute(h, nil, "task", "timeline", "--task", h.task, "--summary", "--last", "2")
	lines := strings.Split(strings.TrimSuffix(summary.stdout, "\n"), "\n")
	if summary.err != nil || len(lines) != 3 || !strings.Contains(lines[0], `"omitted_entries":20`) {
		t.Fatalf("timeline --summary --last 2: %v: %q, want a task line counting 20 omitted and two entries", summary.err, summary.stdout)
	}

	negative := compactExecute(h, nil, "task", "timeline", "--task", h.task, "--last", "-1")
	if ExitCode(negative.err) != 2 || negative.stdout != "" {
		t.Fatalf("timeline --last -1: exit %d stdout %q, want a usage error", ExitCode(negative.err), negative.stdout)
	}
}
