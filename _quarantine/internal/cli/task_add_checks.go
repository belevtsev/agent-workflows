package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAddChecksCommand returns cw task add-checks.
func newTaskAddChecksCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var full bool
	addChecks := &cobra.Command{
		Use:   "add-checks",
		Short: "Add go test checks to a started substantial task after merging its base branch",
		Long:  helpText("task-add-checks"),
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.requireTask(&directory); err != nil {
				return err
			}
			var input taskflow.AmendmentInput
			if err := readTaskInput(cmd, inputFile, &input); err != nil {
				return err
			}
			record, err := taskEnv(deps.config).AddChecks(cmd.Context(), directory, input)
			if err != nil {
				return err
			}
			if !full {
				amendment := record.Amendments[len(record.Amendments)-1]
				ids := []string{}
				for _, check := range amendment.Checks {
					ids = append(ids, check.ID)
				}
				return writeJSON(cmd.OutOrStdout(), addChecksSummary{TaskID: record.ID, Amendment: len(record.Amendments) - 1, Checks: ids, MergeCommit: amendment.MergeCommit, BaseRef: amendment.BaseRef, BaseCommit: amendment.BaseCommit})
			}
			return writeJSON(cmd.OutOrStdout(), record)
		},
	}
	taskFlag(addChecks, &directory)
	addChecks.Example = helpText("task-add-checks-example")
	addChecks.Flags().StringVar(&inputFile, "input", "", "Amendment JSON file, or - for standard input")
	fullFlag(addChecks, &full, "the whole task record")
	return addChecks
}

// addChecksSummary is the default summary line of cw task add-checks.
type addChecksSummary struct {
	TaskID      string   `json:"task_id"`
	Amendment   int      `json:"amendment"`
	Checks      []string `json:"checks"`
	MergeCommit string   `json:"merge_commit"`
	BaseRef     string   `json:"base_ref"`
	BaseCommit  string   `json:"base_commit"`
}
