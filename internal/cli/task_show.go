package cli

import (
	"fmt"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskShowCommand returns cw task show. It only reads, so the global
// --dry-run changes nothing.
func newTaskShowCommand(deps taskDeps) *cobra.Command {
	var directory string
	var fields []string
	show := &cobra.Command{Use: "show", Short: "Read a task and validate its immutable revision chain", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.requireTask(&directory); err != nil {
			return err
		}
		for _, path := range fields {
			if !validFieldPath(path) {
				return usage(fmt.Errorf("--field %q must be a nonempty dotted path without empty segments", path))
			}
		}
		value, err := taskflow.Load(directory)
		if err != nil {
			return err
		}
		if len(fields) > 0 {
			selected, err := selectFields(value, fields)
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), selected)
		}
		return writeJSON(cmd.OutOrStdout(), value)
	}}
	taskFlag(show, &directory)
	show.Flags().StringArrayVar(&fields, "field", nil, "Print only this dotted path of the record (repeatable); a numeric segment indexes an array, any other selects the latest element with that id, assignment id or check id")
	return show
}
