package cli

import (
	"cmp"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/tokens"
)

type adviserTokenItem struct {
	ID             string          `json:"id"`
	Task           string          `json:"task"`
	Adviser        string          `json:"adviser"`
	RequestedModel string          `json:"requested_model"`
	Outcome        string          `json:"outcome"`
	ActualModel    string          `json:"actual_model,omitempty"`
	Tokens         *advisers.Usage `json:"tokens,omitempty"`
}

type adviserTokenUsage struct {
	Calls         int                `json:"calls"`
	Usage         string             `json:"usage"`
	Complete      bool               `json:"complete"`
	Items         []adviserTokenItem `json:"items"`
	MeasuredCalls int                `json:"measured_calls"`
	InputTokens   int64              `json:"input_tokens"`
	OutputTokens  int64              `json:"output_tokens"`
	Overflow      bool               `json:"overflow,omitempty"`
}

type adviserTokenActivity struct {
	all    *adviserTokenUsage
	byTask map[string]*adviserTokenUsage
}

// Count every attempt separately from host tokens. Native providers may report
// measured usage; CLI, pending, and otherwise unmeasured attempts remain unknown.
func loadAdviserTokenActivity(root string) ([]adviserTokenItem, error) {
	items := []adviserTokenItem{}
	if root == "" {
		return items, nil
	}
	directory, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = directory.Close() }()
	dirs, err := adviserHistoryDirectories(directory.FS())
	if err != nil {
		return nil, err
	}
	seen := map[[2]string]advisers.Consultation{}
	for _, rel := range dirs {
		consultations, err := advisers.ReadConsultationsAt(root, rel)
		if errors.Is(err, advisers.ErrNoBinding) {
			if _, statErr := directory.Lstat(rel); errors.Is(statErr, fs.ErrNotExist) {
				continue
			}
		}
		if err != nil {
			return nil, fmt.Errorf("adviser token activity in %s: %w", rel, err)
		}
		for _, consultation := range consultations {
			key := [2]string{consultation.Task, consultation.ID}
			previous, exists := seen[key]
			if !exists || consultation.Execution.EndedAt.After(previous.Execution.EndedAt) {
				seen[key] = consultation
			}
		}
	}
	for _, consultation := range seen {
		items = append(items, adviserTokenItem{
			ID: consultation.ID, Task: consultation.Task, Adviser: consultation.Adviser,
			RequestedModel: consultation.Identity.Model, Outcome: consultation.Execution.Outcome,
			ActualModel: consultation.Execution.ActualModel, Tokens: consultation.Execution.Usage,
		})
	}
	slices.SortFunc(items, func(a, b adviserTokenItem) int {
		return cmp.Or(strings.Compare(a.Task, b.Task), strings.Compare(a.ID, b.ID))
	})
	return items, nil
}

func adviserHistoryDirectories(root fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(root, change.Dir)
	if errors.Is(err, fs.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	dirs := []string{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		rel := path.Join(change.Dir, entry.Name())
		if entry.Name() != change.Archive && entry.Name() != ".advice" {
			if !strings.HasPrefix(entry.Name(), ".") {
				dirs = append(dirs, path.Join(rel, "advisers"))
			}
			continue
		}
		children, err := fs.ReadDir(root, rel)
		if err != nil {
			return nil, err
		}
		for _, child := range children {
			if child.IsDir() && !strings.HasPrefix(child.Name(), ".") {
				history := path.Join(rel, child.Name())
				if entry.Name() == change.Archive {
					history = path.Join(history, "advisers")
				}
				dirs = append(dirs, history)
			}
		}
	}
	return dirs, nil
}

func attachAdviserTokenActivity(report *tokens.Report, items []adviserTokenItem) adviserTokenActivity {
	activity := adviserTokenActivity{byTask: map[string]*adviserTokenUsage{}}
	if len(items) == 0 {
		return activity
	}
	activity.all = &adviserTokenUsage{Usage: "unknown"}
	for _, item := range items {
		usage := activity.byTask[item.Task]
		if usage == nil {
			usage = &adviserTokenUsage{Usage: "unknown", Items: []adviserTokenItem{}}
			activity.byTask[item.Task] = usage
		}
		addAdviserUsage(usage, item)
		addAdviserUsage(activity.all, item)
	}
	for task := range activity.byTask {
		found, _, _, exists := report.Find(task)
		if exists {
			found.Complete = found.Complete && activity.byTask[task].Complete
			continue
		}
		report.Tasks = append(report.Tasks, tokens.Task{Task: task, Roles: []tokens.Entry{}, Stages: []tokens.Entry{}})
	}
	slices.SortFunc(report.Tasks, func(a, b tokens.Task) int { return strings.Compare(a.Task, b.Task) })
	return activity
}

func addAdviserUsage(usage *adviserTokenUsage, item adviserTokenItem) {
	usage.Calls++
	usage.Items = append(usage.Items, item)
	if counts := item.Tokens; counts != nil {
		usage.MeasuredCalls++
		if counts.InputTokens > math.MaxInt64-usage.InputTokens || counts.OutputTokens > math.MaxInt64-usage.OutputTokens {
			usage.Overflow = true
		} else if !usage.Overflow {
			usage.InputTokens += counts.InputTokens
			usage.OutputTokens += counts.OutputTokens
		}
	}
	usage.Complete = usage.MeasuredCalls == usage.Calls && !usage.Overflow
	switch {
	case usage.Complete:
		usage.Usage = "known"
	case usage.MeasuredCalls > 0:
		usage.Usage = "partial"
	default:
		usage.Usage = "unknown"
	}
}

func adviserGlobalTokenJSON(report *tokens.Report, activity adviserTokenActivity) any {
	if activity.all == nil {
		return report
	}
	type taskUsage struct {
		tokens.Task
		Advisers *adviserTokenUsage `json:"advisers,omitempty"`
	}
	tasks := make([]taskUsage, 0, len(report.Tasks))
	for _, task := range report.Tasks {
		tasks = append(tasks, taskUsage{Task: task, Advisers: activity.byTask[task.Task]})
	}
	return struct {
		*tokens.Report
		Tasks    []taskUsage        `json:"tasks"`
		Advisers *adviserTokenUsage `json:"advisers"`
	}{Report: report, Tasks: tasks, Advisers: activity.all}
}

func printAdviserTokenUsage(out io.Writer, usage *adviserTokenUsage) {
	if usage == nil {
		return
	}
	if usage.MeasuredCalls == 0 {
		fmt.Fprintf(out, "advisers: %s; token usage unknown, outside the measured host token totals\n", plural(usage.Calls, "call", "calls"))
	} else {
		fmt.Fprintf(out, "advisers: %s; %d input, %d output tokens (%s; %d measured calls), separate from host totals\n",
			plural(usage.Calls, "call", "calls"), usage.InputTokens, usage.OutputTokens, usage.Usage, usage.MeasuredCalls)
		if usage.Overflow {
			fmt.Fprintln(out, "adviser token totals exceeded the reporting limit; inspect individual calls")
		}
	}
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TASK\tADVISER\tREQUESTED MODEL\tREPORTED MODEL\tINPUT\tOUTPUT\tOUTCOME\tCALL")
	for _, item := range usage.Items {
		input, output := "unknown", "unknown"
		if item.Tokens != nil {
			input, output = commas(item.Tokens.InputTokens), commas(item.Tokens.OutputTokens)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", item.Task, item.Adviser, item.RequestedModel, item.ActualModel, input, output, item.Outcome, item.ID)
	}
	_ = w.Flush()
}
