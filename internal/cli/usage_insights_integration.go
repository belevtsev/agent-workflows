package cli

import (
	"context"
	"fmt"
	"math"

	"github.com/belevtsev/kitsu/v3/internal/insights"
	"github.com/belevtsev/kitsu/v3/internal/taskbudget"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

func usageActualForTask(ctx context.Context, s *session, task string) (map[string]taskbudget.Actual, error) {
	report, err := collectUsageDiagnostics(ctx, s, task)
	if err != nil {
		return nil, err
	}
	items, err := loadAdviserTokenActivity(s.repo.Root)
	if err != nil {
		return nil, err
	}
	activity := attachAdviserTokenActivity(&tokens.Report{}, items)
	return usageActualFromDiagnostics(report, task, activity.byTask[task]), nil
}

func usageActualFromDiagnostics(report insights.Diagnostics, task string, advisers *adviserTokenUsage) map[string]taskbudget.Actual {
	actuals := map[string]taskbudget.Actual{
		taskbudget.Tokens:  {Basis: "Captured host usage only; missing and shared attribution is not estimated."},
		taskbudget.Seconds: {Basis: "Union of observed managed prepared-to-receipt spans, including coordination and waiting; incomplete task coverage, not worker execution or a hard runtime cap."},
	}
	for _, item := range report.Tasks {
		if item.Task != task {
			continue
		}
		known := item.CapturedTokens.Status == "known" || item.CapturedTokens.Status == "lower_bound"
		actuals[taskbudget.Tokens] = taskbudget.Actual{N: item.CapturedTokens.Known, Known: known, Complete: known && item.CapturedTokens.Complete && item.AttributionComplete, Basis: actuals[taskbudget.Tokens].Basis}
		seconds := item.ManagedIntervals.ObservedSeconds
		if item.ManagedIntervals.Closed > 0 && seconds >= 0 && seconds < float64(math.MaxInt64) {
			actuals[taskbudget.Seconds] = taskbudget.Actual{N: int64(seconds), Known: true, Complete: false, Basis: actuals[taskbudget.Seconds].Basis}
		}
	}
	if advisers != nil {
		actual := actuals[taskbudget.Tokens]
		actual.Basis = "Captured host tokens plus separately recorded measured adviser input/output; unknown calls and shared/unattributed usage remain incomplete."
		if advisers.Overflow || advisers.InputTokens > math.MaxInt64-advisers.OutputTokens || advisers.InputTokens+advisers.OutputTokens > math.MaxInt64-actual.N {
			actual.N, actual.Known, actual.Complete = math.MaxInt64, true, false
			actual.Basis = "Nonnegative measured components exceed the integer range; the shown amount is a representable lower bound, not the exact total."
		} else {
			actual.N += advisers.InputTokens + advisers.OutputTokens
			actual.Complete = actual.Complete && advisers.Complete
			actual.Known = actual.Known || advisers.MeasuredCalls > 0
		}
		actuals[taskbudget.Tokens] = actual
	}
	return actuals
}

func hasUsageBudget(root, task string) (bool, error) {
	record, err := taskbudget.Load(root, task)
	return record != nil, err
}

func appendUsageBudgetTasks(root string, tasks []string) ([]string, error) {
	extra, err := taskbudget.Tasks(root)
	return append(tasks, extra...), err
}

func appendUsageBudgetReferences(root string, views []uxTaskView) ([]uxTaskView, error) {
	for i := range views {
		has, err := hasUsageBudget(root, views[i].Task)
		if err != nil {
			return nil, err
		}
		if has {
			views[i].UsageBudget = []string{"kitsu", "usage", "budget", "show", views[i].Task}
		}
	}
	return views, nil
}

func validateUsageBudget(s *session, task string) (int, error) {
	if err := taskbudget.ValidateRecords(s.repo.Root, task); err != nil {
		fmt.Fprintf(s.cfg.Out, "%s usage budget: %v\n", task, err)
		return 1, nil
	}
	return 0, nil
}
