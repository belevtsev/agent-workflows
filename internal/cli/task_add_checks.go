package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAddChecksCommand returns cw task add-checks.
func newTaskAddChecksCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var summary bool
	addChecks := &cobra.Command{
		Use:   "add-checks",
		Short: "Add go test checks to a started substantial task after merging its base branch",
		Long: "Append an amendment that adds credited direct 'go test' checks, tied to a merge commit from the task's base branch, so the merged changes can be tested. " +
			"The input JSON names merge_commit (for example HEAD after 'git merge --no-ff origin/main'), base_ref (for example origin/main), a reason and the checks. " +
			"The plan never changes and no check is replaced, removed or narrowed: each added check needs a new ID and must cover a package or module file the merge changed that no earlier check covers. " +
			"The merge commit must be reachable from HEAD and not from the task baseline or the base, with a parent after the first reachable from the base. " +
			"An accepted amendment clears the stored verification; run the added checks with 'cw task run' and verify again.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := deps.guard(); err != nil {
				return err
			}
			if err := requireTask(directory); err != nil {
				return err
			}
			var input taskflow.AmendmentInput
			if err := readTaskInput(cmd, inputFile, &input); err != nil {
				return err
			}
			record, err := taskflow.AddChecks(cmd.Context(), directory, input)
			if err != nil {
				return err
			}
			if summary {
				amendment := record.Amendments[len(record.Amendments)-1]
				ids := []string{}
				for _, check := range amendment.Checks {
					ids = append(ids, check.ID)
				}
				return writeJSON(cmd.OutOrStdout(), addChecksSummary{Task: record.Directory, TaskID: record.ID, Amendment: len(record.Amendments) - 1, Checks: ids, MergeCommit: amendment.MergeCommit, BaseRef: amendment.BaseRef, BaseCommit: amendment.BaseCommit})
			}
			return writeJSON(cmd.OutOrStdout(), record)
		},
	}
	taskFlag(addChecks, &directory)
	addChecks.Flags().StringVar(&inputFile, "input", "", "Amendment JSON file, or - for standard input")
	summaryFlag(addChecks, &summary)
	return addChecks
}

// addChecksSummary is the --summary line of cw task add-checks.
type addChecksSummary struct {
	Task        string   `json:"task"`
	TaskID      string   `json:"task_id"`
	Amendment   int      `json:"amendment"`
	Checks      []string `json:"checks"`
	MergeCommit string   `json:"merge_commit"`
	BaseRef     string   `json:"base_ref"`
	BaseCommit  string   `json:"base_commit"`
}
