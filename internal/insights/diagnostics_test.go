package insights

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func diagnosticStart(agent, task, stage string, counts tokens.Counts) tokens.Start {
	return tokens.Start{Version: 1, Host: tokens.Codex, Session: "session-1", Agent: agent, Task: task, Stage: stage, Round: 1, Known: true, Record: agent + ".jsonl", Models: map[string]tokens.Counts{"model": counts}}
}

func TestDiagnosticsReconcilesCapturedAccountingAndCacheSubsets(t *testing.T) {
	t.Parallel()
	starts := []tokens.Start{
		diagnosticStart("a", "fix-login", "code", tokens.Counts{Input: 10, CacheWrite: 20, CacheRead: 30, Output: 40}),
		diagnosticStart("b", "fix-login", "review", tokens.Counts{Input: 4, Output: 6}),
		diagnosticStart("c", "fix-login", "commit", tokens.Counts{Input: 2, Output: 3}),
		diagnosticStart(tokens.SessionAgent, "", "", tokens.Counts{Input: 7, CacheRead: 8}),
	}
	starts[0].Round = 2
	baseline := tokens.Build(starts, nil)
	want, _, _, _ := baseline.Find("fix-login")
	got := BuildDiagnostics(DiagnosticInput{Task: "fix-login", Starts: starts})
	if got.CapturedTokens.Known != want.Total || got.CapturedTokens.Complete != want.Complete || got.UncachedTokens.Known != 72 {
		t.Fatalf("captured accounting differs: %+v, baseline %+v", got, want)
	}
	if got.Tasks[0].Shares[2].Known != want.Flow.Rework || got.Tasks[0].Shares[3].Known != want.Flow.Git {
		t.Fatal("workflow categorization differs from tokens.Build")
	}
	// The host identity is part of notice identity. Equal agent/session IDs on
	// distinct hosts must neither deduplicate nor share coordination.
	other := starts[0]
	other.Host, other.Task = tokens.Claude, "other-task"
	otherSession := starts[3]
	otherSession.Host = tokens.Claude
	got = BuildDiagnostics(DiagnosticInput{Starts: append(append(starts, starts[0]), other, otherSession)})
	if got.DuplicateNotices != 1 || got.CapturedTokens.Known != want.Total+115 || got.SharedSessions != 0 {
		t.Fatalf("host/session identity mixed: %+v", got)
	}
}

func TestDiagnosticsUnknownAndSharedUsageNeverAssigned(t *testing.T) {
	t.Parallel()
	a := diagnosticStart("a", "one-task", "code", tokens.Counts{Input: 10})
	b := diagnosticStart("b", "two-task", "review", tokens.Counts{Input: 20})
	session := diagnosticStart(tokens.SessionAgent, "", "", tokens.Counts{Input: 100})
	unlabelled := diagnosticStart("unknown-agent", "", "", tokens.Counts{Input: 50})
	got := BuildDiagnostics(DiagnosticInput{Task: "one-task", Starts: []tokens.Start{a, b, session, unlabelled}})
	if got.CapturedTokens.Known != 10 || got.Tasks[0].AttributionComplete || got.Tasks[0].SharedSessions != 1 || got.UnlabelledStarts != 1 {
		t.Fatalf("invented attribution: %+v", got)
	}
	got = BuildDiagnostics(DiagnosticInput{Starts: []tokens.Start{a}})
	if got.CapturedTokens.Complete || got.Tasks[0].CapturedTokens.Status != "lower_bound" {
		t.Fatalf("missing terminal session usage became complete: %+v", got)
	}
	a.Known, a.Models = false, nil
	got = BuildDiagnostics(DiagnosticInput{Starts: []tokens.Start{a}})
	if got.CapturedTokens.Status != "unknown" || got.Tasks[0].CapturedTokens.Status != "unknown" {
		t.Fatalf("unknown usage became known zero: %+v", got)
	}
}

func TestDiagnosticsHostIsolationCannotCollideWithAnOriginalSessionID(t *testing.T) {
	t.Parallel()
	starts := []tokens.Start{}
	for i, identity := range []struct{ host, session, task string }{
		{tokens.Codex, "same-session", "one-task"},
		{tokens.Claude, "same-session", "two-task"},
		{tokens.Codex, "claude:same-session", "three-task"},
	} {
		agent := diagnosticStart("agent", identity.task, "code", tokens.Counts{Input: int64(i + 1)})
		agent.Host, agent.Session = identity.host, identity.session
		session := diagnosticStart(tokens.SessionAgent, "", "", tokens.Counts{Input: 10})
		session.Host, session.Session = identity.host, identity.session
		starts = append(starts, agent, session)
	}
	got := BuildDiagnostics(DiagnosticInput{Starts: starts})
	if got.DuplicateNotices != 0 || got.SharedSessions != 0 || got.CapturedTokens.Known != 36 || !got.CapturedTokens.Complete {
		t.Fatalf("host identities collided: %+v", got)
	}
	for _, task := range got.Tasks {
		if !task.AttributionComplete || task.CapturedTokens.Known < 11 || task.CapturedTokens.Known > 13 {
			t.Fatalf("coordination assigned across hosts: %+v", task)
		}
	}
}

func TestDiagnosticsMalformedAndConflictingCountersStayUnknown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		starts []tokens.Start
	}{
		{"negative", []tokens.Start{diagnosticStart("a", "one-task", "code", tokens.Counts{Input: -1})}},
		{"overflow", []tokens.Start{diagnosticStart("a", "one-task", "code", tokens.Counts{Input: math.MaxInt64}), diagnosticStart("b", "one-task", "review", tokens.Counts{Input: 1})}},
		{"conflict", []tokens.Start{diagnosticStart("a", "one-task", "code", tokens.Counts{Input: 1}), diagnosticStart("a", "one-task", "code", tokens.Counts{Input: 2})}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildDiagnostics(DiagnosticInput{Starts: tc.starts})
			if got.CapturedTokens.Complete || got.DamagedRecords == 0 || got.CapturedTokens.Known < 0 {
				t.Fatalf("malformed usage hidden: %+v", got)
			}
		})
	}
}

func TestDiagnosticsManagedIntervalsUseUnionAndPreserveUnknowns(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	a, b, c := start.Add(10*time.Second), start.Add(15*time.Second), start.Add(20*time.Second)
	stages := []ManagedStage{{Task: "one-task", Prepared: start, Ended: &a}, {Task: "one-task", Prepared: start.Add(5 * time.Second), Ended: &b}, {Task: "one-task", Prepared: b, Ended: &c, Status: "abandoned"}, {Task: "one-task", Prepared: c}, {Task: "one-task", Damaged: true}}
	got := BuildDiagnostics(DiagnosticInput{Task: "one-task", Stages: stages})
	if got.ManagedIntervals.ObservedSeconds != 20 || got.ManagedIntervals.Closed != 3 || got.ManagedIntervals.Unfinished != 1 || got.ManagedIntervals.Unknown != 1 || got.ManagedIntervals.Abandoned != 1 || got.ManagedIntervals.Complete {
		t.Fatalf("bad interval projection: %+v", got.ManagedIntervals)
	}
	if !reflect.DeepEqual(got, BuildDiagnostics(DiagnosticInput{Task: "one-task", Stages: stages})) {
		t.Fatal("repeated projection changed its result")
	}
}

func TestDiagnosticsCheckRunsRequireExplicitTaskReferences(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Second)
	input := DiagnosticInput{Task: "one-task", Stages: []ManagedStage{{Task: "one-task", Prepared: start, Ended: &end}}, Checks: []CheckRun{}}
	for _, outcome := range []string{store.Failed, store.TimedOut, store.Interrupted, store.SourceChanged, ""} {
		input.Checks = append(input.Checks, CheckRun{Record: store.Record{ID: "test", RunID: outcome + "run", Outcome: outcome, StartedAt: start, EndedAt: &end}, Source: outcome + ".json"})
	}
	got := BuildDiagnostics(input)
	if got.Checks.Runs != 0 {
		t.Fatalf("overlapping whole-repo checks assigned to task: %+v", got.Checks)
	}
	input.References = []CheckReference{{Task: "one-task", CheckID: "test", RunID: "failedrun", Source: "original.finish.json"}, {Task: "one-task", CheckID: "test", RunID: "failedrun", Source: "later.finish.json"}, {Task: "one-task", CheckID: "test", RunID: "missing"}}
	got = BuildDiagnostics(input)
	if got.Checks.Runs != 1 || got.Checks.References != 3 || got.Checks.RepeatedReferences != 1 || got.Checks.MissingReferences != 1 {
		t.Fatalf("execution/ref/reuse mixed: %+v", got.Checks)
	}
	input.Task = ""
	got = BuildDiagnostics(input)
	for _, outcome := range []string{store.Failed, store.TimedOut, store.Interrupted, store.SourceChanged, "unfinished_unknown"} {
		if got.Checks.Outcomes[outcome] != 1 {
			t.Fatalf("outcomes collapsed: %+v", got.Checks)
		}
	}
	if got.Checks.ObservedSeconds != 1 {
		t.Fatal("overlapping check intervals double counted")
	}
}
