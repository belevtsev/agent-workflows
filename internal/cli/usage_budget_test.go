package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/taskbudget"
)

func TestUsageBudgetCLIRoundTripWithoutInstrumentationOrExecutions(t *testing.T) {
	r := kitsutest.NewRepo(t)
	t.Chdir(r.Dir)
	var out, diagnostics bytes.Buffer
	cfg := Config{Out: &out, Err: &diagnostics, NativeExecute: func(context.Context, *advisers.NativeCall) advisers.Execution {
		t.Fatal("budget metadata executed a model")
		return advisers.Execution{}
	}}
	run := func(args ...string) usageBudgetView {
		t.Helper()
		out.Reset()
		diagnostics.Reset()
		if err := Execute(t.Context(), append([]string{"usage", "budget"}, args...), cfg); err != nil {
			t.Fatal(args, err, diagnostics.String())
		}
		var view usageBudgetView
		if err := json.Unmarshal(out.Bytes(), &view); err != nil {
			t.Fatal(err, out.String())
		}
		return view
	}
	view := run("set", "demo", "--unit", "tokens", "--min", "0", "--max", "100", "--basis", "manual", "--json")
	if view.Configuration.Entries[taskbudget.Tokens].Estimate.Max != 100 {
		t.Fatal(view)
	}
	run("set", "demo", "--unit", "seconds", "--limit", "30", "--source", "human declaration", "--json")
	view = run("set", "demo", "--unit", "tokens", "--limit", "150", "--source", "human reference", "--json")
	if len(view.Configuration.Entries) != 2 || view.Configuration.Entries[taskbudget.Tokens].Estimate.Max != 100 {
		t.Fatal("set lost independent metadata", view)
	}
	budget := r.Path("openspec/changes/.usage/demo/usage-budget.json")
	before, err := os.ReadFile(budget)
	if err != nil {
		t.Fatal(err)
	}
	view = run("show", "demo", "--json")
	for _, unit := range []string{taskbudget.Tokens, taskbudget.Seconds} {
		if actual := view.Comparison[unit].Actual; actual.Known || actual.Complete {
			t.Fatal("invented actual measurement", unit, actual)
		}
	}
	after, err := os.ReadFile(budget)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("show changed metadata", err)
	}
	for _, args := range [][]string{{"task", "list", "--json"}, {"task", "show", "demo", "--json"}, {"task", "check", "demo"}} {
		out.Reset()
		diagnostics.Reset()
		if err := Execute(t.Context(), args, cfg); err != nil {
			t.Fatal("optional budget history was not a structural task", args, err, diagnostics.String())
		}
		if !strings.Contains(out.String(), "demo") {
			t.Fatal("task command omitted budget-only task", args, out.String())
		}
	}
	for _, rel := range []string{".git/kitsu", ".git/kitsu-stages"} {
		if _, err := os.Stat(r.Path(rel)); !os.IsNotExist(err) {
			t.Fatal("budget created private execution state", rel, err)
		}
	}
	view = run("reset", "demo", "--unit", "tokens", "--json")
	if len(view.Configuration.Entries) != 1 || view.Configuration.Entries[taskbudget.Seconds].Limit.N != 30 {
		t.Fatal("reset lost another unit", view)
	}
	sibling := filepath.Join(filepath.Dir(budget), "unrelated.json")
	if err := os.WriteFile(sibling, []byte("retain"), 0o644); err != nil {
		t.Fatal(err)
	}
	view = run("reset", "demo", "--unit", "seconds", "--json")
	if view.Configuration != nil {
		t.Fatal("last reset retained configuration", view)
	}
	if _, err := os.Stat(budget); !os.IsNotExist(err) {
		t.Fatal("last reset retained budget", err)
	}
	if data, err := os.ReadFile(sibling); err != nil || string(data) != "retain" {
		t.Fatal("reset changed sibling", err)
	}
	r.Write("openspec/changes/full-demo/note.txt", "structural full task only")
	view = run("show", "full-demo", "--json")
	if view.Configuration != nil || view.Comparison[taskbudget.Tokens].Actual.Known {
		t.Fatal("invented optional configuration or usage", view)
	}
}

func TestUsageBudgetFlagsRequireIndependentExplicitDeclarations(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args []string
		good bool
	}{
		{"show", []string{"show", "demo", "--json"}, true},
		{"estimate zero", []string{"set", "demo", "--unit", "tokens", "--min", "0", "--max", "0", "--basis", "manual"}, true},
		{"limit only", []string{"set", "demo", "--unit", "seconds", "--limit", "0", "--source", "human approval reference"}, true},
		{"reset unit", []string{"reset", "demo", "--unit", "tokens"}, true},
		{"missing min", []string{"set", "demo", "--unit", "tokens", "--max", "10", "--basis", "manual"}, false},
		{"missing max", []string{"set", "demo", "--unit", "tokens", "--min", "0", "--basis", "manual"}, false},
		{"missing basis", []string{"set", "demo", "--unit", "tokens", "--min", "0", "--max", "10"}, false},
		{"missing source", []string{"set", "demo", "--unit", "tokens", "--limit", "10"}, false},
		{"source only", []string{"set", "demo", "--unit", "tokens", "--source", "manual"}, false},
		{"empty source", []string{"set", "demo", "--unit", "tokens", "--limit", "10", "--source", " "}, false},
		{"negative", []string{"set", "demo", "--unit", "tokens", "--limit", "-1", "--source", "manual"}, false},
		{"overflow", []string{"set", "demo", "--unit", "tokens", "--limit", "9223372036854775808", "--source", "manual"}, false},
		{"unsupported unit", []string{"set", "demo", "--unit", "calls", "--limit", "10", "--source", "manual"}, false},
		{"reset all", []string{"reset", "demo"}, false},
		{"no update", []string{"set", "demo", "--unit", "tokens"}, false},
		{"assumption only", []string{"set", "demo", "--unit", "tokens", "--assumption", "manual"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseUsageBudget(tt.args)
			if (err == nil) != tt.good {
				t.Fatal("unexpected validation", err)
			}
		})
	}
}

func TestUsageBudgetPlainDisplayPreservesPartialUsageAndSources(t *testing.T) {
	t.Parallel()
	entry := taskbudget.Entry{
		Estimate: &taskbudget.Estimate{Min: 100, Max: 200, Basis: "manual estimate", Assumptions: []string{"no retries"}},
		Limit:    &taskbudget.Limit{N: 300, Source: "user-declared approval reference"},
	}
	view := usageBudgetView{
		Task:          "demo",
		Configuration: &taskbudget.Record{Entries: map[string]taskbudget.Entry{taskbudget.Tokens: entry}},
		Comparison: map[string]taskbudget.Comparison{
			taskbudget.Tokens:  taskbudget.Compare(entry, taskbudget.Actual{N: 50, Known: true, Basis: "known counters only"}),
			taskbudget.Seconds: taskbudget.Compare(taskbudget.Entry{}, taskbudget.Actual{}),
		},
	}
	var out bytes.Buffer
	printUsageBudget(&out, view)
	for _, want := range []string{"at least 50 (incomplete)", "known counters only", "comparison: incomplete", "seconds: actual unknown", "no retries", entry.Limit.Source} {
		if !strings.Contains(out.String(), want) {
			t.Fatal("display omitted", want, out.String())
		}
	}
	if strings.Contains(out.String(), "comparison: within") {
		t.Fatal("partial usage claimed within budget", out.String())
	}
}

func TestUsageBudgetMetadataDoesNotClaimExecutionAuthority(t *testing.T) {
	t.Parallel()
	options, err := parseUsageBudget([]string{"set", "demo", "--unit", "tokens", "--limit", "10", "--source", "https://example.invalid/human-record"})
	if err != nil {
		t.Fatal(err)
	}
	if options.update.Estimate != nil || options.update.Limit.Source != "https://example.invalid/human-record" {
		t.Fatal(options)
	}
	for _, phrase := range []string{"user-authored metadata", "not proof of authorization", "do not authorize paid execution", "hard runtime caps"} {
		if !strings.Contains(usageBudgetNotice, phrase) {
			t.Fatal("missing explanation", phrase)
		}
	}
}

func TestUsageBudgetPlainDisplayQuotesUserAuthoredTerminalControls(t *testing.T) {
	t.Parallel()
	text := "human record\nforged line\x1b[31m"
	entry := taskbudget.Entry{
		Estimate: &taskbudget.Estimate{Basis: text, Assumptions: []string{text}},
		Limit:    &taskbudget.Limit{Source: text},
	}
	view := usageBudgetView{
		Task:          "demo",
		Configuration: &taskbudget.Record{Entries: map[string]taskbudget.Entry{taskbudget.Tokens: entry}},
		Comparison: map[string]taskbudget.Comparison{
			taskbudget.Tokens: taskbudget.Compare(entry, taskbudget.Actual{Basis: text}),
		},
	}
	var out bytes.Buffer
	printUsageBudget(&out, view)
	if strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\nforged line") {
		t.Fatal("user-authored metadata injected terminal commands or lines", out.String())
	}
	if strings.Count(out.String(), `human record\nforged line\x1b[31m`) != 4 {
		t.Fatal("quoted display dropped metadata", out.String())
	}
	encoded, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var decoded usageBudgetView
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Configuration.Entries[taskbudget.Tokens].Limit.Source != text {
		t.Fatal("display escaping changed user-authored JSON data")
	}
}
