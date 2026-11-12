package cli

import (
	"context"
	"errors"
	"io"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/v3/internal/consult"
	"github.com/belevtsev/agent-workflows/v3/internal/graphify"
	"github.com/spf13/cobra"
)

// reportedExit is a process status for a command that has already written its
// own result, so the entrypoint adds no further error line.
type reportedExit int

func (reportedExit) Error() string { return "" }

func finish(status int, err error) error {
	if err != nil {
		return err
	}
	if status != 0 {
		return reportedExit(status)
	}
	return nil
}

func noPositional(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return usage(err)
	}
	return nil
}

func selectedHome(root *cobra.Command, config Config) (string, error) {
	if home, _ := root.PersistentFlags().GetString("home"); home != "" {
		return home, nil
	}
	return config.HomeDir()
}

func addConsultCommand(root *cobra.Command, config Config) {
	var options consult.Options
	var policy string
	var summary bool
	command := &cobra.Command{
		Use:   "consult",
		Short: "Collect independent adviser judgments, or validate a recorded decision offline",
		Long: "With --request, validate a shared Choice request (at most 8 questions) and send it once to each configured adviser under the policy's batch deadline, writing a new private version 4 record. " +
			"The participating advisers are the task's adviser binding when the request state holds task_context or task_contexts, and otherwise the policy's enabled providers. " +
			"Without a task, an adviser disabled in your saved preferences (advisers.disabled in preferences.json of the configuration directory) is recorded as not_configured and never contacted, even with --policy; so is one you have not opted in to, unless --policy is given: opt in by listing it under advisers.enabled or saving its key with 'cw keys set'. A task keeps its binding. " +
			"A participating adviser is configured when its credential is set (TYPESAFE_API_KEY, SUBQ_API_KEY or OPENAI_API_KEY, from the environment or saved keys); one that is not is recorded as not_configured and never contacted. " +
			"With no configured adviser, nothing is sent and the record status is no_advisers_configured. " +
			"--dry-run writes the same validated preview, marking each adviser dry_run or not_configured, without network access. " +
			"With --adjudicate and --decision, validate the coordinator's decision against a saved version 4 record, using no network, credentials or policy.\n\n" +
			"Exits 0 when complete (including no_advisers_configured and dry runs), 2 when coverage or the decision is incomplete or a task-bound adviser is not configured (the record is still written, and stderr names the variable to set), and 1 when input is invalid or the output path already exists.",
		Args: noPositional,
		RunE: func(cmd *cobra.Command, _ []string) error {
			options.DryRun, _ = root.PersistentFlags().GetBool("dry-run")
			if err := options.Check(); err != nil {
				return usage(err)
			}
			if options.Request != "" {
				selected, err := selectPolicy(root, config, policy)
				if err != nil {
					return err
				}
				options.Policy = selected
			}
			options.Getenv, options.Stdout, options.Stderr = config.Getenv, cmd.OutOrStdout(), cmd.ErrOrStderr()
			// A dry run also resolves credentials, only to report which advisers
			// are configured; no credential value is written or sent.
			if options.Request != "" {
				getenv, err := credentialEnvironment(root, config)
				if err != nil {
					return err
				}
				options.Getenv = getenv
				// consult reads the withheld advisers only for a request
				// without a task binding.
				options.Disabled = func() ([]string, error) { return withheldAdvisers(root, config, policy != "") }
			}
			if !summary {
				return finish(consult.Run(cmd.Context(), options))
			}
			// The human status line is replaced by the one-line view of the
			// record this run wrote; stderr and the exit status are unchanged.
			options.Stdout = io.Discard
			status, err := consult.Run(cmd.Context(), options)
			if err != nil || status == 1 {
				return finish(status, err)
			}
			view, err := options.View()
			if err != nil {
				return err
			}
			if err := writeJSON(cmd.OutOrStdout(), view); err != nil {
				return err
			}
			return finish(status, nil)
		},
	}
	flags := command.Flags()
	flags.StringVar(&options.Request, "request", "", "Shared Choice request JSON file")
	flags.StringVar(&options.Adjudicate, "adjudicate", "", "Saved consultation record to finalize offline")
	flags.StringVar(&options.Decision, "decision", "", "Coordinator decision JSON file, with --adjudicate")
	flags.StringVar(&options.Output, "output", "", "New private record path; an existing file is never replaced")
	flags.StringVar(&policy, "policy", "", policyFlagHelp)
	flags.BoolVar(&summary, "summary", false, "After writing the record, print a one-line JSON view of it (statuses, answers, comparison) without the request body")
	root.AddCommand(command)
}

// graphifyFailure is the structured unavailable report; discovery continues
// from source, so a refusal is data on standard output rather than an error.
type graphifyFailure struct {
	Status   string `json:"status"`
	Code     string `json:"code"`
	Detail   string `json:"detail"`
	Fallback string `json:"fallback"`
}

func addGraphifyCommands(root *cobra.Command, config Config) {
	parent := &cobra.Command{Use: "graphify", Short: "Build and query a guarded local structural graph of tracked Go source", Args: noPositional, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	report := func(cmd *cobra.Command, value any, err error) error {
		if err == nil {
			return writeJSON(cmd.OutOrStdout(), value)
		}
		if errors.Is(err, context.Canceled) {
			return err
		}
		code := "filesystem"
		if refused, ok := errors.AsType[*graphify.Error](err); ok {
			code = refused.Code
		}
		if err := writeJSON(cmd.OutOrStdout(), graphifyFailure{"unavailable", code, err.Error(), graphify.Fallback}); err != nil {
			return err
		}
		return reportedExit(2)
	}
	define := func(use, short string, timeout float64, positional cobra.PositionalArgs, execute func(*cobra.Command, []string, graphify.Options) error) *cobra.Command {
		options := &graphify.Options{}
		seconds := new(float64)
		command := &cobra.Command{Use: use, Short: short, Args: positional, RunE: func(cmd *cobra.Command, args []string) error {
			if options.Repo == "" {
				return usage(errors.New("--repo is required"))
			}
			if math.IsNaN(*seconds) || math.IsInf(*seconds, 0) || *seconds <= 0 || *seconds > 600 {
				return usage(errors.New("--timeout must be finite, positive and at most 600 seconds"))
			}
			selected := *options
			selected.Timeout = time.Duration(*seconds * float64(time.Second))
			selected.Home = func() (string, error) { return selectedHome(root, config) }
			selected.Executable = config.Executable
			return execute(cmd, args, selected)
		}}
		command.Flags().StringVar(&options.Repo, "repo", "", "Repository to index or query")
		command.Flags().StringVar(&options.Runtime, "runtime", "", "Pinned Graphify executable; checked on explicit use")
		command.Flags().StringVar(&options.CacheRoot, "cache-root", "", "External cache directory")
		command.Flags().Float64Var(seconds, "timeout", timeout, "Operation deadline in seconds, at most 600")
		parent.AddCommand(command)
		return command
	}
	define("build", "Reuse a current graph or extract one from a tracked Go snapshot", 300, noPositional, func(cmd *cobra.Command, _ []string, options graphify.Options) error {
		if dryRun, _ := root.PersistentFlags().GetBool("dry-run"); dryRun {
			return usage(errors.New("graphify build rejects --dry-run; no graph was built"))
		}
		result, err := graphify.Build(cmd.Context(), options)
		return report(cmd, result, err)
	})
	var budget int
	query := define("query QUESTION", "Ask a focused structural question of the graph matching current source", 30, func(_ *cobra.Command, args []string) error {
		if len(args) != 1 {
			return usage(errors.New("graphify query requires exactly one QUESTION"))
		}
		return nil
	}, func(cmd *cobra.Command, args []string, options graphify.Options) error {
		if budget < 100 || budget > 16000 || strings.TrimSpace(args[0]) == "" || utf8.RuneCountInString(args[0]) > 8000 {
			return usage(errors.New("query needs a nonempty question up to 8000 characters and --budget between 100 and 16000"))
		}
		options.Question, options.Budget = args[0], budget
		result, err := graphify.Query(cmd.Context(), options)
		return report(cmd, result, err)
	})
	query.Flags().IntVar(&budget, "budget", 2000, "Graphify context budget, from 100 to 16000")
	root.AddCommand(parent)
}
