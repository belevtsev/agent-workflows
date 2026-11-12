package cli

import (
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

func addTaskCommands(root *cobra.Command, config Config) {
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
		"The record binds the advisers configured now: each provider the model policy enables whose credential is set (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY, from the environment or saved keys) and that you opted in to, mapped to its pinned model, possibly none. " +
		"You opt in to a provider by listing it under advisers.enabled in preferences.json of the configuration directory or by saving its key with 'cw keys set'; --policy opts in to every provider that policy enables. A key in the environment alone binds no adviser. " +
		"An adviser disabled in your saved preferences (advisers.disabled) is never bound, even with --policy; an unreadable or invalid preferences file fails before any task is created. " +
		"Saved keys that cannot be read opt in to nothing: the task starts without them and a warning on stderr names the problem. " +
		"The binding never changes for the task's lifetime; 'cw task context' carries it to 'cw consult'. An unreadable or invalid policy fails before any task is created. " +
		"The record also fixes the pack set: the selected packs ('cw pack list') whose detect file is at the top of the selected path now; an unreadable pack selection, or a selected pack not built into cw, fails before any task is created.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		var plan taskflow.Plan
		if err := read(cmd, planFile, &plan); err != nil {
			return err
		}
		selectedPolicy, err := selectPolicy(root, config, policy)
		if err != nil {
			return err
		}
		getenv, err := credentialEnvironment(root, config)
		if err != nil {
			return err
		}
		advisers, err := consult.ConfiguredAdvisers(selectedPolicy, getenv)
		if err != nil {
			return err
		}
		withheld, err := withheldAdvisers(root, config, policy != "", cmd.ErrOrStderr())
		if err != nil {
			return err
		}
		for _, id := range withheld {
			delete(advisers, id)
		}
		packSelection, packs, err := startPacks(root, config)
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
			state, err := roots.stateDir()
			if err != nil {
				return err
			}
			chosenStore = filepath.Join(state, "workflow-tasks")
		}
		record, err := taskflow.StartWithPacks(cmd.Context(), selected, chosenStore, plan, advisers, packSelection, packs)
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
	start.Flags().StringVar(&policy, "policy", "", "Model policy whose enabled advisers are bound without a further opt-in; "+policyFlagHelp)
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
		value, err := taskflow.RunCheck(cmd.Context(), runTask, check)
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
	run.Flags().StringVar(&check, "check", "", "Check ID from the immutable explicit plan or added by 'cw task add-checks'")
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
	var bundleTask, bundleReview, bundleOutput string
	bundle := &cobra.Command{Use: "bundle", Short: "Write a review's reviewer bundle, with one reach row per changed file, for the coordinator to fill", Long: "Write the reviewer bundle for a review about to be assigned: the requirements, prior findings with their dispositions, the files changed since the task start and since the previous reviewed source, the checks with their logs, and one reach row per file changed since that review. " +
		"Each row carries hints (the shebang and up to 10 lines of docs and scripts that name the file, invocations first) and four empty facets: callers, invocation_modes, platforms and consumers. " +
		"Fill every facet from source or write 'n/a: <reason>', then give the file's path and sha256 as the review brief's bundle; 'cw task assign' refuses a missing row, an empty facet, or any other difference from the bundle it regenerates. " +
		"Prints one line with the bundle path, its sha256, the kind (full or follow_up), the row count and the changed-file count. The task record is not changed.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if err := guard(cmd); err != nil {
			return err
		}
		if bundleReview == "" || bundleOutput == "" {
			return usage(errors.New("bundle needs --review-id and --output"))
		}
		target, err := filepath.Abs(bundleOutput)
		if err != nil {
			return err
		}
		value, err := taskflow.BuildBundle(cmd.Context(), bundleTask, bundleReview)
		if err != nil {
			return err
		}
		// Resolve the parent so a symlinked spelling of a path inside the
		// project (such as /tmp for /private/tmp) is still recognized.
		resolved := target
		if parent, parentErr := filepath.EvalSymlinks(filepath.Dir(target)); parentErr == nil {
			resolved = filepath.Join(parent, filepath.Base(target))
		}
		if relative, relErr := filepath.Rel(value.Snapshot.Root, resolved); relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("write the reviewer bundle outside the project source; a file inside it changes the source the review is bound to")
		}
		data, err := taskflow.EncodeBundle(value)
		if err != nil {
			return err
		}
		file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		return output(cmd, bundleSummary(target, data, value))
	}}
	bundle.Flags().StringVar(&bundleTask, "task", "", "Task directory returned by start")
	bundle.Flags().StringVar(&bundleReview, "review-id", "", "Assignment ID the review will use")
	bundle.Flags().StringVar(&bundleOutput, "output", "", "New file for the bundle, outside the project source")
	task.AddCommand(bundle)
	var contractPath, contractSymbols string
	contractExtract := &cobra.Command{Use: "contract-extract", Short: "Print requested Go declarations, without bodies, as a test-engineer contract extract", Long: "Print the requested declarations of the Go package in --path as JSON: each function or method (Type.Method) signature, type (with struct fields and interface methods), const and var, with its doc comment and path:line, and the SHA-256 of every source file a printed declaration comes from. " +
		"Function bodies, function literal bodies and the comments inside them are never printed, and only the requested symbols are. An unknown symbol, a directory without non-test Go source or a parse error fails with nothing printed. " +
		"Save the output outside the project source, review it, and give its path and sha256 as the test-engineer brief's contract; 'cw task assign' refuses an extract that differs from what the current source yields. Other languages keep a prose contract. The task record is not changed.", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if contractPath == "" || contractSymbols == "" {
			return usage(errors.New("contract-extract needs --path and --symbols"))
		}
		symbols, err := taskflow.ParseContractSymbols(contractSymbols)
		if err != nil {
			return usage(err)
		}
		dir := contractPath
		if !filepath.IsAbs(dir) {
			working, err := config.WorkingDir()
			if err != nil {
				return err
			}
			dir = filepath.Join(working, dir)
		}
		contract, err := taskflow.ExtractContract(dir, symbols)
		if err != nil {
			return err
		}
		data, err := taskflow.EncodeContract(contract)
		if err != nil {
			return err
		}
		_, err = cmd.OutOrStdout().Write(data)
		return err
	}}
	contractExtract.Flags().StringVar(&contractPath, "path", "", "Directory of the Go package")
	contractExtract.Flags().StringVar(&contractSymbols, "symbols", "", "Comma-separated symbols: Name or Type.Method")
	task.AddCommand(contractExtract)
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
			var printed any = value
			if summary {
				printed = summarizeVerification(value)
			}
			if outputErr := output(cmd, printed); outputErr != nil {
				return outputErr
			}
		}
		return err
	}}
	verify.Flags().StringVar(&verifyTask, "task", "", "Task directory returned by start")
	verify.Flags().BoolVar(&summary, "summary", false, "Print one line with every problem and only the task-specific limitations")
	task.AddCommand(verify)
	var showTask string
	var fields []string
	show := &cobra.Command{Use: "show", Short: "Read a task and validate its immutable revision chain", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		for _, path := range fields {
			if !validFieldPath(path) {
				return usage(fmt.Errorf("--field %q must be a nonempty dotted path without empty segments", path))
			}
		}
		value, err := taskflow.Load(showTask)
		if err != nil {
			return err
		}
		if len(fields) > 0 {
			selected, err := selectFields(value, fields)
			if err != nil {
				return err
			}
			return output(cmd, selected)
		}
		return output(cmd, value)
	}}
	show.Flags().StringVar(&showTask, "task", "", "Task directory returned by start")
	show.Flags().StringArrayVar(&fields, "field", nil, "Print only this dotted path of the record (repeatable); a numeric segment indexes an array, any other selects the latest element with that id, assignment id or check id")
	task.AddCommand(show)
}
