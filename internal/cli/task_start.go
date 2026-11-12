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
	var summary bool
	start := &cobra.Command{Use: "start", Short: "Capture a baseline, an explicit JSON requirement/check plan and the configured adviser binding", Long: "Capture the selected project's baseline and an explicit JSON requirement/check plan in a new private task record. " +
		"The record binds the advisers configured now: each provider the model policy enables whose credential is set (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY, from the environment or saved keys) and that you opted in to, mapped to its pinned model, possibly none. " +
		"You opt in to a provider by listing it under advisers.enabled in preferences.json of the configuration directory or by saving its key with 'cw keys set'; --policy opts in to every provider that policy enables. A key in the environment alone binds no adviser. " +
		"An adviser disabled in your saved preferences (advisers.disabled) is never bound, even with --policy; an unreadable or invalid preferences file fails before any task is created. " +
		"Saved keys that cannot be read opt in to nothing: the task starts without them and a warning on stderr names the problem. " +
		"The binding never changes for the task's lifetime; 'cw task context' carries it to 'cw consult'. An unreadable or invalid policy fails before any task is created. " +
		"The record also fixes the pack set: the selected packs ('cw pack list') whose detect file is at the top of the selected path now; an unreadable pack selection, or a selected pack not built into cw, fails before any task is created.", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := deps.guard(); err != nil {
			return err
		}
		var plan taskflow.Plan
		if err := readTaskInput(cmd, planFile, &plan); err != nil {
			return err
		}
		options, err := deps.startOptions(flags, cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		record, err := taskflow.StartWithPacks(cmd.Context(), options.repo, options.store, plan, options.advisers, options.packSelection, options.packs)
		if err != nil {
			return err
		}
		if summary {
			return writeJSON(cmd.OutOrStdout(), startSummary(record))
		}
		return writeJSON(cmd.OutOrStdout(), record)
	}}
	start.Flags().StringVar(&flags.repo, "repo", "", "Selected project directory (default: current working directory)")
	start.Flags().StringVar(&planFile, "plan", "", "Explicit plan JSON file, or - for standard input")
	start.Flags().StringVar(&flags.store, "store", "", "Private task storage directory outside the target project")
	start.Flags().StringVar(&flags.policy, "policy", "", "Model policy whose enabled advisers are bound without a further opt-in; "+policyFlagHelp)
	summaryFlag(start, &summary)
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
// model policy, the credential environment, the configured advisers, the
// adviser opt-ins (a warning about unreadable saved keys goes to stderr), the
// pack selection, the working directory and the state directory. The bound
// advisers are those the policy configures less those withheld.
func (deps taskDeps) startOptions(flags taskStartFlags, stderr io.Writer) (taskStartOptions, error) {
	root, config := deps.root, deps.config
	selectedPolicy, err := selectPolicy(root, config, flags.policy)
	if err != nil {
		return taskStartOptions{}, err
	}
	getenv, err := credentialEnvironment(root, config)
	if err != nil {
		return taskStartOptions{}, err
	}
	options := taskStartOptions{repo: flags.repo, store: flags.store}
	if options.advisers, err = consult.ConfiguredAdvisers(selectedPolicy, getenv); err != nil {
		return taskStartOptions{}, err
	}
	withheld, err := withheldAdvisers(root, config, flags.policy != "", stderr)
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
