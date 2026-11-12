package cli

import (
	"errors"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// taskTraceSummary is what `cw task trace --summary` prints: each criterion's
// status and only the gaps that keep a criterion from being proven.
type taskTraceSummary struct {
	Task     string            `json:"task"`
	TaskID   string            `json:"task_id"`
	Criteria map[string]string `json:"criteria"`
	Gaps     []taskTraceGap    `json:"gaps"`
	Problems []string          `json:"problems"`
}

type taskTraceGap struct {
	Criterion    string `json:"criterion"`
	AssignmentID string `json:"assignment_id"`
	Requirement  string `json:"requirement"`
	Status       string `json:"status"`
	Path         string `json:"path,omitempty"`
	Name         string `json:"name,omitempty"`
	TestStatus   string `json:"test_status,omitempty"`
	Detail       string `json:"detail,omitempty"`
}

func traceSummary(directory string, report taskflow.TraceReport) taskTraceSummary {
	value := taskTraceSummary{Task: directory, TaskID: report.TaskID, Criteria: map[string]string{}, Gaps: []taskTraceGap{}, Problems: report.Problems}
	if value.Problems == nil {
		value.Problems = []string{}
	}
	for _, criterion := range report.Criteria {
		value.Criteria[criterion.ID] = criterion.Status
		for _, requirement := range criterion.Requirements {
			gap := taskTraceGap{Criterion: criterion.ID, AssignmentID: requirement.AssignmentID, Requirement: requirement.Requirement, Status: requirement.Status}
			switch requirement.Status {
			case "pending", "uncovered":
				value.Gaps = append(value.Gaps, gap)
			case "dropped":
				gap.Detail = requirement.Dropped
				value.Gaps = append(value.Gaps, gap)
			case "unproven":
				for _, test := range requirement.Tests {
					if test.Status != "passed" {
						gap.Path, gap.Name, gap.TestStatus, gap.Detail = test.Path, test.Name, test.Status, test.Detail
						value.Gaps = append(value.Gaps, gap)
					}
				}
			}
		}
	}
	return value
}

// newTaskTraceCommand returns cw task trace.
func newTaskTraceCommand(taskDeps) *cobra.Command {
	var directory string
	var summary bool
	trace := &cobra.Command{
		Use:   "trace",
		Short: "Trace each acceptance criterion to its requirements, tests and check runs",
		Long: "Read the task and report, for each plan requirement, the contract requirements test-engineer briefs link to it, " +
			"their mapped tests judged at the current source, and the check runs whose -json results recorded each test. " +
			"It changes nothing, works on an abandoned task and exits 0 whatever the statuses.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if directory == "" {
				return usage(errors.New("trace needs --task"))
			}
			report, err := taskflow.Trace(cmd.Context(), directory)
			if err != nil {
				return err
			}
			if !summary {
				return writeJSON(cmd.OutOrStdout(), report)
			}
			record, err := taskflow.Load(directory)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), traceSummary(record.Directory, report))
		},
	}
	trace.Flags().StringVar(&directory, "task", "", "Task directory returned by start")
	trace.Flags().BoolVar(&summary, "summary", false, "Print a short summary instead of the whole report")
	return trace
}
