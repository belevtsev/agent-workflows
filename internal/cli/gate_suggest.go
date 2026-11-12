package cli

import (
	"context"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// Where cw points at cw gate install: one session-start line, and cw
// doctor's git_hooks check. cw never installs the hooks itself.

// hookGateNoteTimeout bounds the session-start look at the task store and
// the git hooks. It runs before the doctor summary, which has a deadline of
// its own (hookDoctorTimeout); together they stay below the 15 second
// SessionStart hook timeout in hooks/hooks.json.
const hookGateNoteTimeout = 5 * time.Second

// gateShared sums up a hooks directory that other repositories use too and
// that lacks a hook cw gate install would write there with --shared.
const gateShared = "shared"

// gateInstallLine is the session-start line for a repository with an open
// substantial task whose git hooks are absent. The session-start context is
// paid for in every session, so it is one short line.
const gateInstallLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"

// summary sums up the report in one word: gateInstalled when both hooks are
// cw's own, gateForeign when either is a file cw gate install did not write,
// gateShared when one is absent from a hooks directory other repositories
// use too, and gateAbsent when one is absent from the repository's own.
func (report gateStatusReport) summary() string {
	installed := 0
	for _, hook := range report.Hooks {
		switch hook.State {
		case gateForeign:
			return gateForeign
		case gateInstalled:
			installed++
		}
	}
	switch {
	case installed == len(report.Hooks):
		return gateInstalled
	case report.Shared:
		return gateShared
	}
	return gateAbsent
}

// gitHooksState returns the summary of cw gate status for the repository
// at dir.
func gitHooksState(ctx context.Context, dir string) (string, error) {
	hooks, err := resolveGateHooks(ctx, dir)
	if err != nil {
		return "", err
	}
	report, err := gateStatus(hooks)
	if err != nil {
		return "", err
	}
	return report.summary(), nil
}

// hasOpenTask reports whether a substantial task record governs publication
// from the repository at dir: one that cw hook pre-publish judges, whether
// or not it gates now. Whether it gates needs the current source digest,
// which this does not capture.
func hasOpenTask(ctx context.Context, root *cobra.Command, config Config, dir string) (bool, error) {
	store, err := hookTaskStore(root, config)
	if err != nil {
		return false, err
	}
	repo, err := taskflow.ResolveRepository(ctx, dir, gateGit)
	if err != nil {
		return false, err
	}
	open, err := taskflow.FindOpen(ctx, store, repo, time.Now(), func(context.Context, string) (string, error) { return "", nil })
	return len(open) > 0, err
}

// gateInstallNote returns gateInstallLine when the repository at dir has
// an open substantial task and its git hooks are absent, and "" otherwise.
// It also says nothing when the gate is off, when a hook cw gate install did
// not write is there (a hook manager's, which may already run the gate), when
// the hooks directory serves other repositories too (cw gate install would
// refuse it without --shared), and on any failure.
func gateInstallNote(ctx context.Context, root *cobra.Command, config Config, dir string) string {
	if config.Getenv(enforceVerifyOption) == "false" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, hookGateNoteTimeout)
	defer cancel()
	if open, err := hasOpenTask(ctx, root, config, dir); err != nil || !open {
		return ""
	}
	if state, err := gitHooksState(ctx, dir); err != nil || state != gateAbsent {
		return ""
	}
	return gateInstallLine
}

// gitHooksCheck is cw doctor's git_hooks check for the repository at dir.
// Absent hooks need attention (missing) only while a substantial task is
// open there; a hook cw gate install did not write, and a hooks directory
// other repositories use too, are reported as detected, with a hint.
func gitHooksCheck(ctx context.Context, root *cobra.Command, config Config, dir string) projectctx.Check {
	check := func(status, detail, source string) projectctx.Check {
		return projectctx.Check{Status: status, Detail: detail, Source: source}
	}
	state, err := gitHooksState(ctx, dir)
	switch {
	case err != nil:
		return check("unchecked", "The git hooks directory could not be located, for example outside a Git repository; raw diagnostics were suppressed", "git_hooks_directory")
	case state == gateInstalled:
		return check("configured", "cw gate install wrote git's pre-commit and pre-push hooks, so git runs the commit gate however a commit or push starts", "git_hooks_directory")
	case state == gateForeign:
		return check("detected", "A pre-commit or pre-push hook that cw gate install did not write is in place; whether it runs the commit gate is unchecked, and cw gate install prints the line to add to it", "git_hooks_directory")
	case state == gateShared:
		return check("detected", "The git hooks directory serves other repositories too, so cw gate install leaves it unchanged without --shared; cw gate status says why", "git_hooks_directory")
	}
	open, err := hasOpenTask(ctx, root, config, dir)
	switch {
	case err != nil:
		return check("unchecked", "Git's hooks do not run the commit gate, and whether an open substantial task governs this repository could not be determined; raw diagnostics were suppressed", "git_hooks_directory_and_task_store")
	case open:
		return check("missing", "An open substantial task governs this repository, but git's hooks do not run the commit gate, so a commit or push from a script, an alias or a terminal skips it; cw gate install adds them", "git_hooks_directory_and_task_store")
	}
	return check("not_applicable", "Git's hooks do not run the commit gate, and no open substantial task governs this repository; cw gate install adds them", "git_hooks_directory_and_task_store")
}
