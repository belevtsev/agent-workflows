package cli

import (
	"context"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/insights"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/taskbudget"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func TestUsageInsightsWithoutWorktreeRetainsCapturedEvidence(t *testing.T) {
	t.Parallel()
	s := diagnosticSession(t)
	diagnosticWrite(t, filepath.Join(tokensDir(s.repo), "session", "worker.json"), tokens.Start{Version: 1, Host: tokens.Codex, Session: "session", Agent: "worker", Task: "example", Known: true, Models: map[string]tokens.Counts{"model": {Input: 30, Output: 5}}})
	s.repo.Root = ""
	before := diagnosticTree(t, s.repo.GitDir)
	input, err := collectUsageDiagnosticInput(context.Background(), s, "example")
	if err != nil {
		t.Fatal(err)
	}
	report := insights.BuildDiagnostics(input)
	extra, err := buildUsageInsightExtras(s, input, report)
	if err != nil {
		t.Fatal(err)
	}
	if report.CapturedTokens.Known != 35 || extra.Notes != nil || extra.Budget != nil {
		t.Fatal("worktree-only metadata prevented captured evidence inspection", report, extra)
	}
	if after := diagnosticTree(t, s.repo.GitDir); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostics without a working tree created records")
	}
}

func TestUsageInsightsPreserveOrdinaryOutputAndCreateNoWork(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "fail")
	for range 2 {
		if result := r.Kitsu("check", "run"); result.Code != ExitNotVerified {
			t.Fatal(result.Output())
		}
	}
	ordinary := r.Kitsu("usage", "show")
	if ordinary.Code != 0 {
		t.Fatal(ordinary.Output())
	}
	before := diagnosticTree(t, r.Path(".git"))
	for range 2 {
		result := r.Kitsu("usage", "show", "--diagnostics", "--json")
		if result.Code != 0 {
			t.Fatal(result.Output())
		}
		var report struct {
			insights.Diagnostics
			Recurrences insights.Recurrences `json:"recurrences"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &report); err != nil {
			t.Fatal(err)
		}
		if report.CapturedTokens.Status != "unknown" || report.Checks.Runs != 2 || len(report.Recurrences.Items) != 1 || report.Recurrences.Items[0].Runs != 2 {
			t.Fatal("original recorded failure or missing usage was misreported", report)
		}
	}
	if after := diagnosticTree(t, r.Path(".git")); !reflect.DeepEqual(before, after) {
		t.Fatal("diagnostics created private evidence, locks or work")
	}
	if after := r.Kitsu("usage", "show"); after.Code != ordinary.Code || after.Stdout != ordinary.Stdout || after.Stderr != ordinary.Stderr {
		t.Fatal("ordinary usage changed after explicit diagnostics", after.Output())
	}
	plain := r.Kitsu("usage", "show", "--diagnostics")
	if plain.Code != 0 || !strings.Contains(plain.Stdout, "Tokens: unknown;") || strings.Contains(plain.Stdout, "Tokens: 0") {
		t.Fatal("unknown usage became measured zero", plain.Output())
	}
}

func TestUsageInsightsOptionalNotesAndCompactRecovery(t *testing.T) {
	t.Parallel()
	r := kitsutest.NewRepo(t)
	file := "openspec/changes/note-task/proposal.md"
	original := "# Proposal\n\n## Consequential claims\n| Claim | Kind | Source | Consequence if wrong | Revisit when | Status |\n| --- | --- | --- | --- | --- | --- |\n| The parser retains λ | inferred | source.txt:1 | Do NOT omit failures | A counterexample appears | open |\n"
	r.Write(file, original)
	r.Write("source.txt", "original evidence λ\n")
	before := r.Kitsu("task", "show", "note-task", "--json")
	if before.Code != 0 {
		t.Fatal(before.Output())
	}
	result := r.Kitsu("usage", "show", "--task", "note-task", "--diagnostics", "--json")
	var report struct {
		Notes insights.Notes `json:"notes"`
	}
	if result.Code != 0 || json.Unmarshal([]byte(result.Stdout), &report) != nil || len(report.Notes.Items) != 1 {
		t.Fatal(result.Output())
	}
	if report.Notes.Items[0].Fields["kind"] != "inferred" || report.Notes.Items[0].Fields["consequence if wrong"] != "Do NOT omit failures" {
		t.Fatal("uncertainty or consequence lost", report)
	}
	if after := r.Kitsu("task", "show", "note-task", "--json"); after.Code != before.Code || after.Stdout != before.Stdout {
		t.Fatal("optional author notes changed task authority", after.Output())
	}
	page := compactResult(t, r.Kitsu("context", "read", file, "--max-bytes", "2048"), 0)
	var recovered strings.Builder
	item := page.Items[0]
	for steps := 0; ; steps++ {
		if steps > 20 {
			t.Fatal("unbounded note source expansion")
		}
		for _, line := range item.Lines {
			recovered.WriteString(line.Text)
		}
		if item.Next == "" {
			break
		}
		next := compactResult(t, r.Kitsu("context", "show", page.ID, "--item", item.ID, "--cursor", item.Next, "--max-bytes", "2048"), 0)
		item = next.Items[0]
	}
	if recovered.String() != original {
		t.Fatal("compact recovery changed optional original notes")
	}
	if r.Read(file) != original {
		t.Fatal("diagnostics rewrote optional source")
	}
}

func TestUsageInsightsBudgetActualsKeepScopeAndCompleteness(t *testing.T) {
	t.Parallel()
	diagnostic := insights.Diagnostics{Tasks: []insights.TaskDiagnostic{{Task: "example", CapturedTokens: insights.TokenCounter{Known: 100, Complete: true, Status: "known"}, AttributionComplete: true, ManagedIntervals: insights.IntervalSummary{Closed: 2, ObservedSeconds: 12.9, Complete: true}}}}
	for _, tc := range []struct {
		name     string
		adviser  *adviserTokenUsage
		want     int64
		complete bool
	}{
		{name: "host only", want: 100, complete: true},
		{name: "measured advisers", adviser: &adviserTokenUsage{Calls: 1, MeasuredCalls: 1, Complete: true, InputTokens: 20, OutputTokens: 5}, want: 125, complete: true},
		{name: "unmeasured retry", adviser: &adviserTokenUsage{Calls: 2, MeasuredCalls: 1, InputTokens: 20, OutputTokens: 5}, want: 125},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actual := usageActualFromDiagnostics(diagnostic, "example", tc.adviser)
			if !actual[taskbudget.Tokens].Known || actual[taskbudget.Tokens].N != tc.want || actual[taskbudget.Tokens].Complete != tc.complete {
				t.Fatal(actual)
			}
			if actual[taskbudget.Seconds].N != 12 || !actual[taskbudget.Seconds].Known || actual[taskbudget.Seconds].Complete {
				t.Fatal("observed interval union became complete task runtime", actual)
			}
		})
	}
	unknown := usageActualFromDiagnostics(insights.Diagnostics{}, "missing", nil)
	if unknown[taskbudget.Tokens].Known || unknown[taskbudget.Seconds].Known {
		t.Fatal("absent evidence became zero actuals", unknown)
	}
	diagnostic.Tasks[0].AttributionComplete = false
	if actual := usageActualFromDiagnostics(diagnostic, "example", nil); actual[taskbudget.Tokens].Complete {
		t.Fatal("shared attribution was presented complete", actual)
	}
}

func TestUsageInsightsNestedHelpAndCompletion(t *testing.T) {
	t.Parallel()
	r := kitsutest.NewRepo(t)
	for _, args := range [][]string{{"usage", "budget"}, {"usage", "budget", "--help"}, {"help", "usage", "budget"}, {"usage", "help", "budget"}, {"usage", "budget", "help", "set"}, {"usage", "help", "budget", "set"}} {
		result := r.Kitsu(args...)
		if result.Code != 0 || !strings.Contains(result.Stdout, "usage: kitsu usage budget") || result.Stderr != "" {
			t.Fatal("nested help was not read-only available", args, result.Output())
		}
	}
	result := r.Kitsu("__complete", "--shell", "fish", "--", "usage", "budget", "")
	if result.Code != 0 || !strings.Contains(result.Stdout, "set") || !strings.Contains(result.Stdout, "show") || !strings.Contains(result.Stdout, "reset") {
		t.Fatal("nested budget completion missing", result.Output())
	}
}

func TestUsageInsightsOverflowPreservesMeasuredLowerBounds(t *testing.T) {
	t.Parallel()
	diagnostic := insights.Diagnostics{Tasks: []insights.TaskDiagnostic{{Task: "example", CapturedTokens: insights.TokenCounter{Known: 8, Complete: true, Status: "known"}, AttributionComplete: true}}}
	for _, advisers := range []*adviserTokenUsage{
		{InputTokens: math.MaxInt64, OutputTokens: 1, MeasuredCalls: 1, Calls: 1, Complete: true},
		{InputTokens: 8, MeasuredCalls: 2, Calls: 2, Overflow: true},
	} {
		actual := usageActualFromDiagnostics(diagnostic, "example", advisers)[taskbudget.Tokens]
		comparison := taskbudget.Compare(taskbudget.Entry{Limit: &taskbudget.Limit{N: 5, Source: "explicit test limit"}}, actual)
		if !actual.Known || actual.Complete || actual.N != math.MaxInt64 || comparison.LimitStatus != "exceeded" {
			t.Fatal("established exceedance or measured lower bound lost on overflow", actual, comparison)
		}
	}
}
