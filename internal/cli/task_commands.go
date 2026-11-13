package cli

import (
	"errors"
	"io"
	"os"
	"path/filepath"

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
var taskCommands = []func(deps taskDeps) *cobra.Command{
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
	newTaskBriefCommand,
	newTaskReceiptCommand,
	newTaskTraceCommand,
	newTaskAuthorshipCommand,
	newTaskAbandonCommand,
	newTaskAddChecksCommand,
	newTaskTimelineCommand,
	newTaskUsageCommand,
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

// taskFlag adds --task, the task directory a command reads or changes, or its
// task ID, which requireTask checks and resolves.
func taskFlag(cmd *cobra.Command, directory *string) {
	cmd.Flags().StringVar(directory, "task", "", "Task directory returned by start, or its task ID (32 hex digits) in the task store: $CW_TASK_STORE, else the state directory's workflow-tasks")
}

// requireTask is the usage error of a command run without --task. Each task
// command checks it before reading its input or the task: an empty task path
// would otherwise resolve to the working directory. A task ID, 32 lowercase
// hex digits, becomes that task's directory in the task store the hook reads
// (hookTaskStore); write ./ID for a directory of that name.
func (deps taskDeps) requireTask(directory *string) error {
	if *directory == "" {
		return usage(errors.New("--task DIR is required"))
	}
	if !taskflow.IsTaskID(*directory) {
		return nil
	}
	store, err := hookTaskStore(deps.root, deps.config)
	if err != nil {
		return err
	}
	*directory = filepath.Join(store, *directory)
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

// fullFlag adds --full to cmd, which prints the whole value instead of the
// default one-line summary: the identifiers and hashes a coordinator needs
// next, not the record, which grows with every check and worker record.
// --summary, the former opt-in to the summary, stays accepted and changes
// nothing.
func fullFlag(cmd *cobra.Command, full *bool, what string) {
	cmd.Flags().BoolVar(full, "full", false, "Print "+what+" instead of the one-line summary")
	cmd.Flags().Bool("summary", false, "Accepted for compatibility; the summary is the default")
	_ = cmd.Flags().MarkHidden("summary")
}
