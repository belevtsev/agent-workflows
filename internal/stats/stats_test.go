package stats

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/belevtsev/gatecub/v3/internal/store"
)

var now = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)

type run struct {
	id, key string
	cmd     []string
	outcome string
	ago     time.Duration // how long before now the run ended
}

var (
	unitCmd = []string{"sh", "unit.sh"}
	lintCmd = []string{"sh", "lint.sh"}
)

// fill records the runs in order through the store, so each gets the next
// sequence number of its directory.
func fill(t *testing.T, st *store.Store, runs ...run) {
	t.Helper()
	for _, r := range runs {
		started := now.Add(-r.ago - time.Minute)
		runID, err := store.NewRunID(started)
		if err != nil {
			t.Fatal(err)
		}
		cmd := r.cmd
		if cmd == nil {
			cmd = unitCmd
		}
		active, err := st.Begin(t.Context(), store.Record{RunID: runID, ID: r.id, Run: cmd, Tree: r.key, ContentKey: r.key, StartedAt: started})
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		end := now.Add(-r.ago)
		if _, err := active.Finish(t.Context(), func(rec *store.Record) {
			rec.Outcome, rec.EndedAt = r.outcome, &end
		}); err != nil {
			t.Fatalf("Finish: %v", err)
		}
	}
}

func decide(t *testing.T, st *store.Store, decisions ...store.Decision) {
	t.Helper()
	for _, d := range decisions {
		if d.Time.IsZero() {
			d.Time = now.Add(-time.Hour)
		}
		if err := st.Record(t.Context(), d); err != nil {
			t.Fatalf("Record: %v", err)
		}
	}
}

func collect(t *testing.T, st *store.Store, since time.Time) *Report {
	t.Helper()
	report, err := Collect(st, since, now)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	return report
}

func TestCollectEmptyStore(t *testing.T) {
	st := store.Open(t.TempDir())
	report := collect(t, st, now.Add(-30*24*time.Hour))
	if len(report.Runs) != 0 || len(report.PassAfterFailure) != 0 || len(report.Decisions) != 0 || report.Unreadable != 0 {
		t.Errorf("report of an empty store = %+v", report)
	}
	if report.Runs == nil || report.PassAfterFailure == nil || report.Decisions == nil {
		t.Error("an empty report has nil maps or slices, which JSON prints as null")
	}
	if !report.Until.Equal(now) || !report.Since.Equal(now.Add(-30*24*time.Hour)) {
		t.Errorf("period = %v .. %v", report.Since, report.Until)
	}
}

func TestRunsPerCheckByOutcome(t *testing.T) {
	st := store.Open(t.TempDir())
	fill(t, st,
		run{id: "unit", key: "k1", outcome: store.Passed, ago: 5 * time.Hour},
		run{id: "unit", key: "k2", outcome: store.Failed, ago: 4 * time.Hour},
		run{id: "unit", key: "k3", outcome: store.Passed, ago: 3 * time.Hour},
		run{id: "unit", key: "k4", outcome: store.SourceChanged, ago: 2 * time.Hour},
		run{id: "lint", key: "k1", cmd: lintCmd, outcome: store.TimedOut, ago: time.Hour},
		run{id: "lint", key: "k2", cmd: lintCmd, outcome: store.Interrupted, ago: time.Hour},
	)
	report := collect(t, st, now.Add(-24*time.Hour))
	want := map[string]map[string]int{
		"unit": {store.Passed: 2, store.Failed: 1, store.SourceChanged: 1},
		"lint": {store.TimedOut: 1, store.Interrupted: 1},
	}
	if !reflect.DeepEqual(report.Runs, want) {
		t.Errorf("Runs = %v, want %v", report.Runs, want)
	}
}

func TestPassAfterFailure(t *testing.T) {
	cases := []struct {
		name string
		runs []run
		want map[string]int
	}{
		{
			name: "pass after a failure on the same key",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Failed, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{"unit": 1},
		},
		{
			name: "pass after a timeout on the same key",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.TimedOut, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{"unit": 1},
		},
		{
			name: "pass after a pass is not counted",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Failed, ago: 4 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{"unit": 1},
		},
		{
			name: "no count across content keys",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Failed, ago: 3 * time.Hour},
				{id: "unit", key: "k2", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{},
		},
		{
			name: "no count across checks",
			runs: []run{
				{id: "lint", key: "k1", outcome: store.Failed, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{},
		},
		{
			name: "a non-decisive run in between does not hide the failure",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Failed, ago: 4 * time.Hour},
				{id: "unit", key: "k1", outcome: store.SourceChanged, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Interrupted, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{"unit": 1},
		},
		{
			name: "order of recording, not the clock",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Failed, ago: time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 5 * time.Hour},
			},
			want: map[string]int{"unit": 1},
		},
		{
			name: "a pass before the failure is not counted",
			runs: []run{
				{id: "unit", key: "k1", outcome: store.Passed, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Failed, ago: 2 * time.Hour},
			},
			want: map[string]int{},
		},
		{
			name: "a different command on the same key is not the same check",
			runs: []run{
				{id: "unit", key: "k1", cmd: []string{"sh", "old.sh"}, outcome: store.Failed, ago: 3 * time.Hour},
				{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
			},
			want: map[string]int{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := store.Open(t.TempDir())
			fill(t, st, tc.runs...)
			report := collect(t, st, now.Add(-24*time.Hour))
			if !reflect.DeepEqual(report.PassAfterFailure, tc.want) {
				t.Errorf("PassAfterFailure = %v, want %v", report.PassAfterFailure, tc.want)
			}
		})
	}
}

// design.md, Records, Statistics: a pass counts when its previous decisive
// run on the same content key, with the same `run` and `paths`, did not pass.
// A run of another command in between is not that previous run.
func TestPassAfterFailureSkipsRunsOfAnotherCommand(t *testing.T) {
	st := store.Open(t.TempDir())
	fill(t, st,
		run{id: "unit", key: "k1", outcome: store.Failed, ago: 4 * time.Hour},
		run{id: "unit", key: "k1", cmd: []string{"sh", "unit.sh", "-v"}, outcome: store.Passed, ago: 3 * time.Hour},
		run{id: "unit", key: "k1", outcome: store.Passed, ago: 2 * time.Hour},
	)
	report := collect(t, st, now.Add(-24*time.Hour))
	if want := map[string]int{"unit": 1}; !reflect.DeepEqual(report.PassAfterFailure, want) {
		t.Errorf("PassAfterFailure = %v, want %v", report.PassAfterFailure, want)
	}
}

func TestPeriodFilter(t *testing.T) {
	st := store.Open(t.TempDir())
	since := now.Add(-7 * 24 * time.Hour)
	fill(t, st,
		run{id: "unit", key: "k1", outcome: store.Failed, ago: 8 * 24 * time.Hour},
		run{id: "unit", key: "k1", outcome: store.Passed, ago: 6 * 24 * time.Hour},
		run{id: "unit", key: "k2", outcome: store.Failed, ago: 9 * 24 * time.Hour},
		run{id: "unit", key: "k2", outcome: store.Passed, ago: 8 * 24 * time.Hour},
		run{id: "lint", key: "k1", cmd: lintCmd, outcome: store.Passed, ago: 10 * 24 * time.Hour},
	)
	decide(t, st,
		store.Decision{Time: now.Add(-8 * 24 * time.Hour), Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed"},
		store.Decision{Time: now.Add(-6 * 24 * time.Hour), Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed"},
	)
	report := collect(t, st, since)
	if want := map[string]map[string]int{"unit": {store.Passed: 1}}; !reflect.DeepEqual(report.Runs, want) {
		t.Errorf("Runs = %v, want %v", report.Runs, want)
	}
	// The pass in the period counts although the failure before it is older.
	if want := map[string]int{"unit": 1}; !reflect.DeepEqual(report.PassAfterFailure, want) {
		t.Errorf("PassAfterFailure = %v, want %v", report.PassAfterFailure, want)
	}
	if len(report.Decisions) != 1 || report.Decisions[0].Count != 1 {
		t.Errorf("Decisions = %+v, want one allowed decision in the period", report.Decisions)
	}
}

func TestDecisionGroups(t *testing.T) {
	st := store.Open(t.TempDir())
	failed := map[string]string{"unit": "failed"}
	decide(t, st,
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed"},
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed"},
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "denied", Checks: failed},
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "denied", Checks: map[string]string{"unit": "failed", "lint": "not_run"}},
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "denied", Checks: map[string]string{"unit": "not_run", "lint": "not_run"}},
		store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "denied", Cause: store.CauseInvalidDeclaration},
		store.Decision{Hook: "pre-commit", Mode: store.Observe, Outcome: "denied", Checks: failed},
		store.Decision{Hook: "pre-push", Mode: store.Enforce, Outcome: "denied", Checks: map[string]string{"unit": "timed_out"}},
		store.Decision{Hook: "pre-merge-commit", Mode: store.Enforce, Outcome: "allowed"},
	)
	report := collect(t, st, now.Add(-24*time.Hour))
	want := []Group{
		{Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed", Count: 2},
		{
			Hook: "pre-commit", Mode: store.Enforce, Outcome: "denied", Count: 4,
			Reasons: map[string]int{"failed": 2, "not_run": 2, store.CauseInvalidDeclaration: 1},
			Checks:  map[string]map[string]int{"failed": {"unit": 2}, "not_run": {"lint": 2, "unit": 1}},
		},
		{
			Hook: "pre-commit", Mode: store.Observe, Outcome: "denied", Count: 1,
			Reasons: map[string]int{"failed": 1}, Checks: map[string]map[string]int{"failed": {"unit": 1}},
		},
		{Hook: "pre-merge-commit", Mode: store.Enforce, Outcome: "allowed", Count: 1},
		{
			Hook: "pre-push", Mode: store.Enforce, Outcome: "denied", Count: 1,
			Reasons: map[string]int{"timed_out": 1}, Checks: map[string]map[string]int{"timed_out": {"unit": 1}},
		},
	}
	if !reflect.DeepEqual(report.Decisions, want) {
		t.Errorf("Decisions =\n%+v\nwant\n%+v", report.Decisions, want)
	}
}

func TestUnreadable(t *testing.T) {
	st := store.Open(t.TempDir())
	fill(t, st, run{id: "unit", key: "k1", outcome: store.Passed, ago: time.Hour})
	decide(t, st, store.Decision{Hook: "pre-commit", Mode: store.Enforce, Outcome: "allowed"})
	bad := filepath.Join(st.Dir, "runs", "unit", "k1", "broken.json")
	if err := os.WriteFile(bad, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	month := filepath.Join(st.Dir, "decisions", now.Add(-time.Hour).Format("2006-01")+".jsonl")
	file, err := os.OpenFile(month, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("garbage\n")
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	report := collect(t, st, now.Add(-24*time.Hour))
	if report.Unreadable != 2 {
		t.Errorf("Unreadable = %d, want 2", report.Unreadable)
	}
	if report.Runs["unit"][store.Passed] != 1 || len(report.Decisions) != 1 {
		t.Errorf("the readable records were not counted: %+v", report)
	}
}
