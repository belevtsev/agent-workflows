package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// newTaskConsultCommand returns cw task consult.
func newTaskConsultCommand(deps taskDeps) *cobra.Command {
	var directory, phase, inputFile string
	var full bool
	command := &cobra.Command{Use: "consult", Short: "Preserve and validate a version 4 consultation record that covers the task's bound advisers", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		if inputFile == "" || inputFile == "-" {
			return usage(errors.New("consult needs a distinct original artifact file via --input"))
		}
		value, err := taskEnv(deps.config).AttachConsultation(cmd.Context(), directory, phase, inputFile)
		if err != nil {
			return err
		}
		if !full {
			return writeJSON(cmd.OutOrStdout(), consultSummary(value, phase))
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(command, &directory)
	command.Example = helpText("task-consult-example")
	command.Flags().StringVar(&phase, "phase", "", "before or final")
	command.Flags().StringVar(&inputFile, "input", "", "Original version 4 record written by cw consult (raw or adjudicated)")
	fullFlag(command, &full, "the whole task record")
	return command
}
