package cli

import (
	"errors"
	"io"
	"os"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// taskDeps is what the cw task subcommands share: the root command, whose
// persistent flags select the directories and --dry-run, and the
// configuration.
type taskDeps struct {
	root   *cobra.Command
	config Config
}

// taskCommands are the constructors of the cw task subcommands, one per file.
var taskCommands = []func(taskDeps) *cobra.Command{
	newTaskStartCommand,
	newTaskContextCommand,
	newTaskRunCommand,
	newTaskAssessCommand,
	newTaskBundleCommand,
	newTaskContractExtractCommand,
	newTaskAssignCommand,
	newTaskHandoffCommand,
	newTaskAcceptCommand,
	newTaskConsultCommand,
	newTaskVerifyCommand,
	newTaskShowCommand,
	newTaskTraceCommand,
	newTaskAuthorshipCommand,
	newTaskAbandonCommand,
	newTaskAddChecksCommand,
	newTaskTimelineCommand,
}

// addTaskCommands adds cw task and its subcommands to root.
func addTaskCommands(root *cobra.Command, config Config) {
	task := &cobra.Command{Use: "task", Short: "Record source-bound requirements, worker handoffs, checks and completion", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.AddCommand(task)
	deps := taskDeps{root: root, config: config}
	for _, constructor := range taskCommands {
		task.AddCommand(constructor(deps))
	}
}

// guard rejects the global --dry-run: a task state command always writes.
func (deps taskDeps) guard() error {
	if dryRun, _ := deps.root.PersistentFlags().GetBool("dry-run"); dryRun {
		return usage(errors.New("task state commands reject --dry-run; no files or checks were changed"))
	}
	return nil
}

// readTaskInput decodes the JSON input file at path, or standard input for
// "-", into value.
func readTaskInput(cmd *cobra.Command, path string, value any) error {
	if path == "" {
		return usage(errors.New("a JSON input file is required"))
	}
	var reader io.Reader = cmd.InOrStdin()
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	return taskflow.Decode(reader, value)
}

// summaryFlag adds --summary to cmd. It prints one compact line with the
// identifiers and hashes a coordinator needs next, instead of the whole
// record, which grows with every check and worker record.
func summaryFlag(cmd *cobra.Command, summary *bool) {
	cmd.Flags().BoolVar(summary, "summary", false, "Print a one-line summary instead of the whole task record")
}
