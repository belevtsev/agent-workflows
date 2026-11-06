package cli

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/belevtsev/agent-workflows/internal/consult"
	"github.com/belevtsev/agent-workflows/internal/taskflow"
	"github.com/spf13/cobra"
)

func addTaskCommands(root *cobra.Command, config Config, _ options) {
	noArgs := func(cmd *cobra.Command, args []string) error {
		if err := cobra.NoArgs(cmd, args); err != nil {
			return usage(err)
		}
		return nil
	}
	task := &cobra.Command{Use: "task", Short: "Record source-bound requirements, worker handoffs, checks and completion", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	root.AddCommand(task)
	output := func(cmd *cobra.Command, value any) error {
		data, err := json.Marshal(value, json.Deterministic(true))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return err
	}
	read := func(cmd *cobra.Command, path string, value any) error {
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
	guard := func(cmd *cobra.Command) error {
		dryRun, _ := root.PersistentFlags().GetBool("dry-run")
		if dryRun {
			return usage(errors.New("task state commands reject --dry-run; no files or checks were changed"))
		}
		return nil
	}
	// --summary prints one compact line with the identifiers and hashes a
	// coordinator needs next, instead of the whole record, which grows with
	// every check and worker record.
	var summary bool
	summaryFlag := func(cmd *cobra.Command) {
		cmd.Flags().BoolVar(&summary, "summary", false, "Print a one-line summary instead of the whole task record")
	}
	var repo, planFile, store, policy string
	start := &cobra.Command{Use: "start", Short: "Capture a baseline, an explicit JSON requirement/check plan and the configured adviser binding", Long: "Capture the selected project's baseline and an explicit JSON requirement/check plan in a new private task record. " +
		"The record binds the advisers configured now: each provider the model policy enables whose credential is set (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY, from the environment or saved keys), mapped to its pinned model, possibly none. " +
		"The binding never changes for the task's lifetime; 'cw task context' carries it to 'cw consult'. An unreadable or invalid policy fails before any task is created.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var plan taskflow.Plan
		if err := read(cmd, planFile, &plan); err != nil {
			return err
		}
		selectedPolicy := policy
		if selectedPolicy == "" {
			var err error
			selectedPolicy, err = activePolicy(root, config)
			if err != nil {
				return err
			}
		}
		getenv, err := credentialEnvironment(root, config)
		if err != nil {
			return err
		}
		advisers, err := consult.ConfiguredAdvisers(selectedPolicy, getenv)
		if err != nil {
			return err
		}
		selected := repo
		if selected == "" {
			var err error
			selected, err = config.WorkingDir()
			if err != nil {
				return err
			}
		}
		chosenStore := store
		if chosenStore == "" {
			roots, err := resolveRoots(root, config)
			if err != nil {
				return err
			}
			chosenStore = filepath.Join(roots.state, "workflow-tasks")
		}
		record, err := taskflow.Start(cmd.Context(), selected, chosenStore, plan, advisers)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, startSummary(record))
		}
		return output(cmd, record)
	}}
	start.Flags().StringVar(&repo, "repo", "", "Selected project directory (default: current working directory)")
	start.Flags().StringVar(&planFile, "plan", "", "Explicit plan JSON file, or - for standard input")
	start.Flags().StringVar(&store, "store", "", "Private task storage directory outside the target project")
	start.Flags().StringVar(&policy, "policy", "", "Validated model policy whose enabled advisers are bound (defaults to the installed release's)")
	summaryFlag(start)
	task.AddCommand(start)
	var contextTask, phase string
	contextCommand := &cobra.Command{Use: "context", Short: "Emit the exact before/final source context, with the task's adviser binding, for shared adviser state", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		value, err := taskflow.PrepareContext(cmd.Context(), contextTask, phase)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	contextCommand.Flags().StringVar(&contextTask, "task", "", "Task directory returned by start")
	contextCommand.Flags().StringVar(&phase, "phase", "", "before or final")
	task.AddCommand(contextCommand)
	var runTask, check string
	run := &cobra.Command{Use: "run", Short: "Execute one planned argv check with bounded private logs", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		value, err := taskflow.Run(cmd.Context(), runTask, check)
		if value.ID != "" {
			var printed any = value
			if summary {
				printed = runSummary(runTask, value)
			}
			if outputErr := output(cmd, printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	run.Flags().StringVar(&runTask, "task", "", "Task directory returned by start")
	run.Flags().StringVar(&check, "check", "", "Check ID from the immutable explicit plan")
	summaryFlag(run)
	task.AddCommand(run)
	var assessTask, assessmentFile string
	assess := &cobra.Command{Use: "assess", Short: "Record acceptance and Coordinator decisions against actual source/check references", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var input taskflow.AssessmentInput
		if err := read(cmd, assessmentFile, &input); err != nil {
			return err
		}
		value, err := taskflow.Assess(cmd.Context(), assessTask, input)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, assessSummary(value, input))
		}
		return output(cmd, value)
	}}
	assess.Flags().StringVar(&assessTask, "task", "", "Task directory returned by start")
	assess.Flags().StringVar(&assessmentFile, "input", "", "Assessment JSON file, or - for standard input")
	summaryFlag(assess)
	task.AddCommand(assess)
	var assignTask, briefFile string
	assign := &cobra.Command{Use: "assign", Short: "Bind a worker brief and exclusive writer scope to the current source", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var input taskflow.WorkerBrief
		if err := read(cmd, briefFile, &input); err != nil {
			return err
		}
		value, err := taskflow.AssignWorker(cmd.Context(), assignTask, input)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, assignSummary(value, input.ID))
		}
		return output(cmd, value)
	}}
	assign.Flags().StringVar(&assignTask, "task", "", "Task directory returned by start")
	assign.Flags().StringVar(&briefFile, "input", "", "Worker brief JSON file, or - for standard input")
	summaryFlag(assign)
	task.AddCommand(assign)
	var handoffTask, resultFile string
	handoff := &cobra.Command{Use: "handoff", Short: "Validate a worker result against its assignment, source and actual checks", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var input taskflow.WorkerResult
		if err := read(cmd, resultFile, &input); err != nil {
			return err
		}
		value, err := taskflow.SubmitWorkerResult(cmd.Context(), handoffTask, input)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, handoffSummary(value, input.AssignmentID))
		}
		return output(cmd, value)
	}}
	handoff.Flags().StringVar(&handoffTask, "task", "", "Task directory returned by start")
	handoff.Flags().StringVar(&resultFile, "input", "", "Worker result JSON file, or - for standard input")
	summaryFlag(handoff)
	task.AddCommand(handoff)
	var acceptTask, acceptanceFile string
	accept := &cobra.Command{Use: "accept", Short: "Record the coordinator's source-backed disposition of an exact worker result", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var input taskflow.WorkerAcceptance
		if err := read(cmd, acceptanceFile, &input); err != nil {
			return err
		}
		value, err := taskflow.AcceptWorkerResult(cmd.Context(), acceptTask, input)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, acceptSummary(value, input.AssignmentID))
		}
		return output(cmd, value)
	}}
	accept.Flags().StringVar(&acceptTask, "task", "", "Task directory returned by start")
	accept.Flags().StringVar(&acceptanceFile, "input", "", "Coordinator acceptance JSON file, or - for standard input")
	summaryFlag(accept)
	task.AddCommand(accept)
	var consultTask, consultPhase, consultFile string
	consult := &cobra.Command{Use: "consult", Short: "Preserve and validate a version 4 consultation record that covers the task's bound advisers", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		if consultFile == "" || consultFile == "-" {
			return usage(errors.New("consult needs a distinct original artifact file via --input"))
		}
		value, err := taskflow.AttachConsultation(cmd.Context(), consultTask, consultPhase, consultFile)
		if err != nil {
			return err
		}
		if summary {
			return output(cmd, consultSummary(value, consultPhase))
		}
		return output(cmd, value)
	}}
	consult.Flags().StringVar(&consultTask, "task", "", "Task directory returned by start")
	consult.Flags().StringVar(&consultPhase, "phase", "", "before or final")
	consult.Flags().StringVar(&consultFile, "input", "", "Original version 4 record written by cw consult (raw or adjudicated)")
	summaryFlag(consult)
	task.AddCommand(consult)
	var verifyTask string
	verify := &cobra.Command{Use: "verify", Short: "Gate completion against current source, requirements, checks and Coordinator decisions", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		value, err := taskflow.Verify(cmd.Context(), verifyTask)
		if value.TaskID != "" {
			if outputErr := output(cmd, value); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	verify.Flags().StringVar(&verifyTask, "task", "", "Task directory returned by start")
	task.AddCommand(verify)
	var showTask string
	show := &cobra.Command{Use: "show", Short: "Read a task and validate its immutable revision chain", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		value, err := taskflow.Load(showTask)
		if err != nil {
			return err
		}
		return output(cmd, value)
	}}
	show.Flags().StringVar(&showTask, "task", "", "Task directory returned by start")
	task.AddCommand(show)
}
