package cli

import (
	"io"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// newTaskStartCommand returns cw task start.
func newTaskStartCommand(deps taskDeps) *cobra.Command {
	var flags taskStartFlags
	var planFile string
	var full bool
	start := &cobra.Command{Use: "start", Short: "Capture a baseline, an explicit JSON requirement/check plan and the configured adviser binding", Long: helpText("task-start"), Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var plan taskflow.Plan
		if err := readTaskInput(cmd, planFile, &plan); err != nil {
			return err
		}
		options, err := deps.startOptions(flags, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		record, err := taskEnv(deps.config).StartWithPacks(cmd.Context(), options.repo, options.store, plan, options.advisers, options.packSelection, options.packs)
		if err != nil {
			return err
		}
		if !full {
			return writeJSON(cmd.OutOrStdout(), startSummary(record))
		}
		return writeJSON(cmd.OutOrStdout(), record)
	}}
	start.Example = helpText("task-start-example")
	start.Flags().StringVar(&flags.repo, "repo", "", "Selected project directory (default: current working directory)")
	start.Flags().StringVar(&planFile, "plan", "", "Explicit plan JSON file, or - for standard input")
	start.Flags().StringVar(&flags.store, "store", "", "Private task storage directory outside the target project")
	start.Flags().StringVar(&flags.policy, "policy", "", "Model policy whose enabled advisers are bound without a further opt-in; "+policyFlagHelp)
	fullFlag(start, &full, "the whole task record")
	return start
}

// taskStartFlags are the cw task start flags that choose what a new task
// binds besides its plan.
type taskStartFlags struct {
	repo, store, policy string
}

// taskStartOptions is what a new task binds besides its plan: the selected
// project, the task store, the adviser binding and the pack set.
type taskStartOptions struct {
	repo, store   string
	advisers      map[string]string
	packSelection string
	packs         []langpack.Manifest
}

// startOptions resolves what cw task start binds, failing in this order: the
// model policy, the configuration directory, the configured advisers, the
// adviser opt-ins, the pack selection, the working directory and the state
// directory. Saved keys that cannot be read are not used and opt in to
// nothing, with one warning on stderr. The bound advisers are those the
// policy configures less those withheld.
func (deps taskDeps) startOptions(flags taskStartFlags, stderr io.Writer) (taskStartOptions, error) {
	root, config := deps.root, deps.config
	selectedPolicy, err := selectPolicy(root, config, flags.policy)
	if err != nil {
		return taskStartOptions{}, err
	}
	getenv, unread, err := credentialEnvironment(root, config, stderr)
	if err != nil {
		return taskStartOptions{}, err
	}
	options := taskStartOptions{repo: flags.repo, store: flags.store}
	if options.advisers, err = consult.ConfiguredAdvisers(selectedPolicy, getenv); err != nil {
		return taskStartOptions{}, err
	}
	withheld, err := withheldAdvisers(root, config, flags.policy != "", unread, stderr)
	if err != nil {
		return taskStartOptions{}, err
	}
	for _, id := range withheld {
		delete(options.advisers, id)
	}
	if options.packSelection, options.packs, err = startPacks(root, config); err != nil {
		return taskStartOptions{}, err
	}
	if options.repo == "" {
		if options.repo, err = config.WorkingDir(); err != nil {
			return taskStartOptions{}, err
		}
	}
	if options.store == "" {
		roots, err := resolveRoots(root, config)
		if err != nil {
			return taskStartOptions{}, err
		}
		state, err := roots.stateDir()
		if err != nil {
			return taskStartOptions{}, err
		}
		options.store = filepath.Join(state, "workflow-tasks")
	}
	return options, nil
}
