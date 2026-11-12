package cli

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/spf13/cobra"
)

// gateOffVariable turns cw hook git off when it is "off": a person's
// override for one git command, which the plugin config does not reach.
const gateOffVariable = "CW_GATE"

// addGitHookCommand adds cw hook git pre-commit, pre-push and
// pre-merge-commit, which git runs as its own hooks in the worktree it
// commits, pushes or merges in. The subcommand is the hook's name, which the
// script cw gate install writes passes; pre-merge-commit judges the merge
// commit as pre-commit judges any commit. A git hook cannot ask, so the
// gate's deny exits ExitDenied with its reason on stderr, and each ask
// becomes a warning on stderr with exit 0. Every other case, including any
// internal error, exits 0 without output, as cw hook pre-publish does.
func addGitHookCommand(root, hook *cobra.Command, config Config) {
	git := &cobra.Command{Use: "git", Short: "Git hooks: gate git commit, merge and push on cw task verify from git's own pre-commit, pre-push and pre-merge-commit hooks", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() }}
	hook.AddCommand(git)
	// command is what the gate judges, a commit or a push; what is the git
	// command that runs the hook, as the help names it.
	for _, publish := range []struct{ name, command, what string }{
		{"pre-commit", "commit", "git commit"},
		{"pre-push", "push", "git push"},
		{"pre-merge-commit", "commit", "git merge"},
	} {
		applies := "It applies the rules of cw hook pre-publish to the " + publish.what
		if publish.name == "pre-merge-commit" {
			applies = "Git runs it before git merge, or git pull, makes a merge commit without conflicts; a merge with conflicts is committed by git commit, which runs pre-commit. " +
				"It applies the rules of cw hook pre-publish to the merge commit as to a git commit"
		}
		git.AddCommand(&cobra.Command{
			Use:   publish.name + " [GIT-HOOK-ARGUMENTS]",
			Short: "Git " + publish.name + " hook: exit 3 (denied) while an open substantial task record for the repository lacks a passing cw task verify on the source being published",
			Long: "Run by git as its " + publish.name + " hook, in the worktree it publishes from; git's arguments and stdin are ignored. " +
				applies + ": when a record gates, it prints the deny reason on stderr and exits 3 (denied), which stops git. " +
				"A git hook cannot ask, so where cw hook pre-publish would ask, it prints the question as a warning on stderr and exits 0. Every other case, including any internal error, exits 0 without output. " +
				"Set " + gateOffVariable + "=off, or " + enforceVerifyOption + "=false and " + smallTaskPromptOption + "=false, in git's environment to turn it off; " + publish.what + " --no-verify skips git's hooks.",
			Args: cobra.ArbitraryArgs,
			// A remote URL git passes that starts with "-" must not fail the push.
			FParseErrWhitelist: cobra.FParseErrWhitelist{UnknownFlags: true},
			RunE: func(cmd *cobra.Command, _ []string) error {
				ctx, cancel := context.WithTimeout(cmd.Context(), cmp.Or(config.prePublishTimeout, hookPublishTimeout))
				defer cancel()
				message, blocked := gitHookDecision(ctx, root, config, publish.command)
				if message != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), message)
				}
				if blocked {
					return reportedExit(ExitDenied)
				}
				return nil
			},
		})
	}
}

// gitHookDecision returns what cw hook git prints for the git command
// ("commit" or "push") about to run in the working directory, and whether
// it blocks it.
func gitHookDecision(ctx context.Context, root *cobra.Command, config Config, command string) (string, bool) {
	enforce := config.Getenv(enforceVerifyOption) != "false"
	prompt := config.Getenv(smallTaskPromptOption) != "false"
	if config.Getenv(gateOffVariable) == "off" || !enforce && !prompt {
		return "", false
	}
	dir, err := config.WorkingDir()
	if err != nil {
		return "", false
	}
	path, err := hookTaskStore(root, config)
	store := &storeGuard{err: err}
	if err == nil {
		store.store = gate.TaskStore(path)
	}
	decision := gate.DecideGitHook(ctx, gate.GitHookInput{Dir: dir, Push: command == "push", Enforce: enforce, Prompt: prompt},
		gate.Deps{Git: gate.ExecGit, Now: time.Now, Store: store})
	if store.failed && decision.Permission == gate.Allow {
		decision = gate.Decision{Permission: gate.Ask, Reason: storeFailureReason(store.err, store.store != nil)}
	}
	options := " In a git hook the plugin options are environment variables of git, such as " + enforceVerifyOption + "=false or " +
		smallTaskPromptOption + "=false, and " + gateOffVariable + "=off turns the gate off for git's hooks."
	switch decision.Permission {
	case gate.Deny:
		return decision.Reason + options, true
	case gate.Ask:
		return "agent-workflows: warning: a git hook cannot ask for confirmation, so this " + command + " goes ahead where Claude Code would ask: " +
			strings.TrimPrefix(decision.Reason, "agent-workflows: ") + options, false
	}
	return "", false
}
