package cli

import (
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskAssessCommand returns cw task assess.
func newTaskAssessCommand(deps taskDeps) *cobra.Command {
	var directory, inputFile string
	var full bool
	assess := &cobra.Command{Use: "assess", Short: "Record acceptance and Coordinator decisions against actual source/check references", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := requireTask(directory); err != nil {
			return err
		}
		var input taskflow.AssessmentInput
		if err := readTaskInput(cmd, inputFile, &input); err != nil {
			return err
		}
		value, err := taskEnv(deps.config).Assess(cmd.Context(), directory, input)
		if err != nil {
			return err
		}
		if !full {
			return writeJSON(cmd.OutOrStdout(), assessSummary(value, input))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(assess, &directory)
	assess.Flags().StringVar(&inputFile, "input", "", "Assessment JSON file, or - for standard input")
	fullFlag(assess, &full, "the whole task record")
	return assess
}
