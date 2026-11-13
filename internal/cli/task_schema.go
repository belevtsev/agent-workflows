package cli

import (
	"embed"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"
)

// inputSchemas holds the JSON Schema of each task command's JSON input, one
// file per command, named after it.
//
//go:embed schemas/*.json
var inputSchemas embed.FS

// schemaCommands are the cw task commands whose JSON input cw task schema
// prints. cw task consult reads the record cw consult wrote, which is
// experimental (docs/operations.md, Stability), so it has no schema here.
var schemaCommands = []string{"start", "assign", "handoff", "accept", "assess", "add-checks"}

// newTaskSchemaCommand returns cw task schema.
func newTaskSchemaCommand(taskDeps) *cobra.Command {
	names := strings.Join(schemaCommands, "|")
	return &cobra.Command{
		Use:   "schema <" + names + ">",
		Short: "Print the JSON Schema of a task command's JSON input",
		Long: "Print the JSON Schema (draft 2020-12) of the JSON input that a cw task command reads, embedded in cw. " +
			"It gives every member and its type; the command checks further rules, such as that a requirement's check_ids name planned checks.",
		Example:   "cw task schema assign >brief.schema.json",
		ValidArgs: schemaCommands,
		Args: func(_ *cobra.Command, args []string) error {
			if len(args) != 1 || !slices.Contains(schemaCommands, args[0]) {
				return usage(fmt.Errorf("name one command: %s", names))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := inputSchemas.ReadFile("schemas/" + args[0] + ".json")
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}
