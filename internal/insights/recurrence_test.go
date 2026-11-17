package insights

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/store"
)

func recurrenceRecord(id, content, outcome string) store.Record {
	return store.Record{ID: "unit", RunID: id, ContentKey: content, Run: []string{"go", "test", "./..."}, Outcome: outcome, StartedAt: time.Unix(int64(len(id)), 0)}
}

func TestWorkflowFailureRecurrencesSeparateInputsOutcomesAndDuplicates(t *testing.T) {
	t.Parallel()
	a := recurrenceRecord("run-a", "same", store.Failed)
	b := recurrenceRecord("run-bb", "same", store.Failed)
	changed := recurrenceRecord("run-c", "changed", store.Failed)
	command := recurrenceRecord("run-d", "same", store.Failed)
	command.Run = []string{"go", "test", "./different"}
	records := []store.Record{a, a, b, changed, command, recurrenceRecord("time-a", "same", store.TimedOut), recurrenceRecord("time-bb", "same", store.TimedOut), recurrenceRecord("interrupt-a", "same", store.Interrupted), recurrenceRecord("interrupt-b", "same", store.Interrupted), recurrenceRecord("source-a", "same", store.SourceChanged)}
	report := RepeatedFailures(records, Notes{}, 20)
	if len(report.Items) != 2 || report.Omitted != 0 {
		t.Fatal(report)
	}
	for _, item := range report.Items {
		if item.Runs != 2 || item.ContentKey != "same" || item.Observation != "not_recorded" || len(item.NoteReferences) != 0 {
			t.Fatal(item)
		}
	}
	if !strings.Contains(report.Scope, "no confirmed root cause") {
		t.Fatal("advisory hint gained authority", report)
	}
}

func TestWorkflowFailureRecurrencesRespectHypothesisEvidence(t *testing.T) {
	t.Parallel()
	a, b := recurrenceRecord("run-a", "same", store.Failed), recurrenceRecord("run-bb", "same", store.Failed)
	policy := RecordedDefinitionKey(a)
	text := fmt.Sprintf("## Troubleshooting\n| ID | Check | Content key | Policy key | Hypothesis | Prediction | Run | Observation | Status |\n| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n| h1 | unit | same | %s | duplicate callback | exactly one call | run-bb | two calls observed | falsified |\n", policy)
	dir := noteFixture(t, "tests.md", text)
	notes, err := ReadNotes(dir, "example", 20)
	if err != nil || len(notes.Items) != 0 || notes.Closed != 1 {
		t.Fatal(notes, err)
	}
	report := RepeatedFailures([]store.Record{a, b}, notes, 20)
	if len(report.Items) != 1 || report.Items[0].Observation != "recorded_author_claim" || len(report.Items[0].NoteReferences) != 1 {
		t.Fatal("closed distinguishing observation was lost or promoted", report)
	}
	notes.Observations[0].Fields["run"] = "run-a"
	if got := RepeatedFailures([]store.Record{a, b}, notes, 20); got.Items[0].Observation != "not_recorded" {
		t.Fatal("old observation substituted for latest failure", got)
	}
	notes.Omitted = 1
	if got := RepeatedFailures([]store.Record{a, b}, notes, 20); got.Items[0].Observation != "unknown" {
		t.Fatal("omitted history became absence of evidence", got)
	}
}

func TestWorkflowFailureRecurrencesBoundReferences(t *testing.T) {
	t.Parallel()
	records := []store.Record{}
	for i := range 12 {
		records = append(records, recurrenceRecord(fmt.Sprintf("run-%02d", i), "same", store.Failed))
	}
	for i := range 2 {
		records = append(records, recurrenceRecord(fmt.Sprintf("other-%d", i), "other", store.Failed))
	}
	report := RepeatedFailures(records, Notes{}, 1)
	if len(report.Items) != 1 || report.Omitted != 1 {
		t.Fatal(report)
	}
	for _, item := range RepeatedFailures(records, Notes{}, 20).Items {
		if item.Runs == 12 && (len(item.RunSamples) != 8 || item.OmittedRuns != 4) {
			t.Fatal(item)
		}
	}
}

func TestWorkflowFailureRecurrencesShowLaterPassingEvidence(t *testing.T) {
	t.Parallel()
	a, b := recurrenceRecord("failure-a", "same", store.Failed), recurrenceRecord("failure-b", "same", store.Failed)
	a.Seq, b.Seq = 1, 2
	passed := recurrenceRecord("pass", "same", store.Passed)
	passed.Seq = 3
	// Recorded sequence remains authoritative when wall clocks move backward.
	passed.StartedAt = a.StartedAt.Add(-time.Hour)
	report := RepeatedFailures([]store.Record{a, b, passed}, Notes{}, 20)
	if len(report.Items) != 1 || report.Items[0].LatestRun != "pass" || report.Items[0].LatestOutcome != store.Passed || report.Items[0].Signal != "historical_recurrence_followed_by_pass" {
		t.Fatal("old failures appeared current after a later passing run", report)
	}
}
