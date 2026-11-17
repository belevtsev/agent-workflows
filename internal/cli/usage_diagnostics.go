package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/insights"
	"github.com/belevtsev/kitsu/v3/internal/store"
	"github.com/belevtsev/kitsu/v3/internal/taskbudget"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func runUsageDiagnostics(ctx context.Context, cfg Config, task string, asJSON bool) error {
	if task != "" && !taskName.MatchString(task) {
		return usagef("invalid task name %q", task)
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		return err
	}
	input, err := collectUsageDiagnosticInput(ctx, s, task)
	if err != nil {
		return failf("usage diagnostics: %v", err)
	}
	report := insights.BuildDiagnostics(input)
	extra, err := buildUsageInsightExtras(s, input, report)
	if err != nil {
		return failf("usage diagnostics: %v", err)
	}
	if asJSON {
		return printJSON(cfg, struct {
			insights.Diagnostics
			usageInsightExtras
		}{report, extra})
	}
	fmt.Fprintln(cfg.Out, "Offline workflow diagnostics (captured local evidence)")
	fmt.Fprintf(cfg.Out, "Tokens: %s; uncached input + output: %s\n", diagnosticAmount(report.CapturedTokens), diagnosticAmount(report.UncachedTokens))
	for _, t := range report.Tasks {
		fmt.Fprintf(cfg.Out, "%s: %s tokens, attribution complete: %t\n", decl.Plain(t.Task), diagnosticAmount(t.CapturedTokens), t.AttributionComplete)
		for _, share := range t.Shares {
			fmt.Fprintf(cfg.Out, "  %s: %d (%.1f%% of captured)\n", share.Category, share.Known, share.Percent)
		}
	}
	if report.ManagedIntervals.Closed == 0 {
		fmt.Fprintln(cfg.Out, "Managed prepared-to-receipt span union: unknown; no closed intervals")
	} else {
		fmt.Fprintf(cfg.Out, "Managed prepared-to-receipt span union: %.3fs observed lower bound; closed %d, unfinished %d, abandoned %d, unknown %d\n", report.ManagedIntervals.ObservedSeconds, report.ManagedIntervals.Closed, report.ManagedIntervals.Unfinished, report.ManagedIntervals.Abandoned, report.ManagedIntervals.Unknown)
	}
	fmt.Fprintf(cfg.Out, "Checks: %d actual stored runs, %d explicitly associated, %d unassociated; %d references, %d repeated; reuse execution unknown\n", report.Checks.Runs, report.Checks.AssociatedRuns, report.Checks.UnassociatedRuns, report.Checks.References, report.Checks.RepeatedReferences)
	for _, c := range report.Candidates {
		fmt.Fprintf(cfg.Out, "Investigation candidate: %s %s round %d, %d captured tokens (%s/%s/%s; %s)\n", decl.Plain(c.Task), decl.Plain(c.Stage), c.Round, c.Known, decl.Plain(c.Host), decl.Plain(c.Session), decl.Plain(c.Agent), decl.Plain(c.Record))
	}
	for _, unknown := range report.Unknowns {
		fmt.Fprintln(cfg.Out, unknown)
	}
	printUsageInsightExtras(cfg, extra)
	return nil
}

func diagnosticAmount(counter insights.TokenCounter) string {
	if counter.Status == "unknown" {
		return "unknown"
	}
	if counter.Status == "lower_bound" {
		return fmt.Sprintf("at least %d captured", counter.Known)
	}
	return fmt.Sprintf("%d captured", counter.Known)
}

type usageInsightExtras struct {
	Notes       *insights.Notes      `json:"notes,omitempty"`
	Recurrences insights.Recurrences `json:"recurrences"`
	Budget      *usageBudgetView     `json:"budget,omitempty"`
	Advisers    *adviserTokenUsage   `json:"advisers,omitempty"`
}

func buildUsageInsightExtras(s *session, input insights.DiagnosticInput, report insights.Diagnostics) (usageInsightExtras, error) {
	extra := usageInsightExtras{}
	notes := insights.Notes{}
	if input.Task != "" && s.repo.Root != "" {
		var err error
		notes, err = insights.ReadNotes(s.repo.Root, input.Task, 20)
		if err != nil {
			return extra, err
		}
		extra.Notes = &notes
	}
	extra.Recurrences = insights.RepeatedFailures(diagnosticRunRecords(input), notes, 20)
	items, err := loadAdviserTokenActivity(s.repo.Root)
	if err != nil {
		return extra, err
	}
	activity := attachAdviserTokenActivity(&tokens.Report{}, items)
	extra.Advisers = activity.all
	if input.Task != "" {
		extra.Advisers = activity.byTask[input.Task]
	}
	if input.Task != "" && s.repo.Root != "" {
		record, err := taskbudget.Load(s.repo.Root, input.Task)
		if err != nil {
			return extra, err
		}
		if record != nil {
			actuals := usageActualFromDiagnostics(report, input.Task, extra.Advisers)
			extra.Budget = &usageBudgetView{Task: input.Task, Configuration: record, Comparison: map[string]taskbudget.Comparison{}, Notice: usageBudgetNotice}
			for _, unit := range []string{taskbudget.Tokens, taskbudget.Seconds} {
				extra.Budget.Comparison[unit] = taskbudget.Compare(record.Entries[unit], actuals[unit])
			}
		}
	}
	return extra, nil
}

func printUsageInsightExtras(cfg Config, extra usageInsightExtras) {
	if extra.Notes != nil {
		fmt.Fprintf(cfg.Out, "Optional author notes: %d displayed; %d closed, %d omitted; sources remain authoritative\n", len(extra.Notes.Items), extra.Notes.Closed, extra.Notes.Omitted)
		for _, note := range extra.Notes.Items {
			fmt.Fprintf(cfg.Out, "  %s %s:%d\n", note.Kind, decl.Plain(note.Path), note.Line)
			for _, key := range []string{"claim", "choice", "hypothesis", "status"} {
				if value := note.Fields[key]; value != "" {
					fmt.Fprintf(cfg.Out, "    %s: %s\n", key, decl.Plain(value))
				}
			}
			if len(note.Missing) > 0 || len(note.OmittedFields) > 0 {
				fmt.Fprintf(cfg.Out, "    incomplete projection: missing %v; omitted %v; inspect original\n", note.Missing, note.OmittedFields)
			}
		}
		if len(extra.Notes.Items) > 0 {
			fmt.Fprintln(cfg.Out, "  Sources, consequences, alternatives and predictions: inspect the original references or --json; these notes carry no execution authority.")
		}
		for _, problem := range extra.Notes.Problems {
			fmt.Fprintln(cfg.Out, decl.Plain(problem))
		}
	}
	for _, recurrence := range extra.Recurrences.Items {
		fmt.Fprintf(cfg.Out, "Recorded repeated %s: %s on content %s (%d runs); last failure %s; latest recorded %s %s; observation %s; signal %s\n", recurrence.Outcome, decl.Plain(recurrence.Check), decl.Plain(recurrence.ContentKey), recurrence.Runs, decl.Plain(recurrence.LastRun), decl.Plain(recurrence.LatestOutcome), decl.Plain(recurrence.LatestRun), recurrence.Observation, recurrence.Signal)
	}
	if extra.Recurrences.Omitted > 0 {
		fmt.Fprintf(cfg.Out, "Additional repeated failure groups omitted: %d\n", extra.Recurrences.Omitted)
	}
	if extra.Budget != nil {
		for _, unit := range []string{taskbudget.Tokens, taskbudget.Seconds} {
			c := extra.Budget.Comparison[unit]
			fmt.Fprintf(cfg.Out, "Budget %s: estimate %s; declared limit %s; actual known=%t complete=%t (%d)\n", unit, c.EstimateStatus, c.LimitStatus, c.Actual.Known, c.Actual.Complete, c.Actual.N)
		}
		fmt.Fprintln(cfg.Out, usageBudgetNotice)
	}
	printAdviserTokenUsage(cfg.Out, extra.Advisers)
}

func collectUsageDiagnostics(ctx context.Context, s *session, task string) (insights.Diagnostics, error) {
	input, err := collectUsageDiagnosticInput(ctx, s, task)
	if err != nil {
		return insights.Diagnostics{}, err
	}
	return insights.BuildDiagnostics(input), nil
}

func collectUsageDiagnosticInput(ctx context.Context, s *session, task string) (insights.DiagnosticInput, error) {
	if err := ctx.Err(); err != nil {
		return insights.DiagnosticInput{}, err
	}
	input := insights.DiagnosticInput{Task: task}
	if err := collectDiagnosticStore(s, &input); err != nil {
		return input, err
	}
	if err := collectDiagnosticStages(s, &input); err != nil {
		return input, err
	}
	if s.repo.Root != "" {
		tasks, err := verification.Tasks(s.repo.Root)
		if err != nil {
			return input, err
		}
		for _, name := range tasks {
			if task != "" && name != task {
				continue
			}
			history, err := verification.Load(s.repo.Root, name)
			if errors.Is(err, verification.ErrNoHistory) {
				continue
			}
			if err != nil {
				return input, err
			}
			for _, result := range history.Attempt.Results {
				input.References = append(input.References, insights.CheckReference{Task: name, CheckID: result.ID, RunID: result.RunID, Source: "verification:" + name + "/" + history.ID})
			}
		}
	}
	return input, ctx.Err()
}

// diagnosticRunRecords preserves task provenance from original receipts and
// verification references. Overlapping timestamps alone never select a run.
func diagnosticRunRecords(input insights.DiagnosticInput) []store.Record {
	refs := map[[2]string]bool{}
	all := append([]insights.CheckReference{}, input.References...)
	for _, stage := range input.Stages {
		if !stage.Damaged {
			all = append(all, stage.Checks...)
		}
	}
	for _, ref := range all {
		if ref.Task == input.Task {
			refs[[2]string{ref.CheckID, ref.RunID}] = true
		}
	}
	result := []store.Record{}
	for _, check := range input.Checks {
		if input.Task == "" || refs[[2]string{check.Record.ID, check.Record.RunID}] {
			result = append(result, check.Record)
		}
	}
	return result
}
