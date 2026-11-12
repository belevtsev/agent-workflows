package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
	"github.com/spf13/cobra"
)

// Where cw points at cw gate install: one session-start line, once per open
// task, and cw doctor's git_hooks check. cw never installs the hooks itself.

// hookGateNoteTimeout bounds the session-start look at the task store and
// the git hooks. It runs before the doctor summary, which has a deadline of
// its own (hookDoctorTimeout); together they stay below the 15 second
// SessionStart hook timeout in hooks/hooks.json.
const hookGateNoteTimeout = 5 * time.Second

// gateSuggestedDirectory is the directory under the session-start cache
// (--cache) that holds one empty file per task, named by its ID, for each
// task whose session start has suggested cw gate install.
const gateSuggestedDirectory = "gate-suggested"

// gateShared sums up a hooks directory that other repositories use too and
// that lacks a hook cw gate install would write there with --shared.
const gateShared = "shared"

// gateInstallLine is the session-start line for a repository with an open
// substantial task whose git hooks are absent. The session-start context is
// paid for in every session, so it is one short line.
const gateInstallLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"

// summary sums up the report in one word: gateInstalled when every hook is
// cw's own, gateForeign when any is a file cw gate install did not write,
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

// openTasks returns the substantial task records that govern publication
// from the repository at dir: the ones cw hook pre-publish judges, whether
// or not they gate now. Whether one gates needs the current source digest,
// which this does not capture.
func openTasks(ctx context.Context, root *cobra.Command, config Config, dir string) ([]taskflow.OpenRecord, error) {
	store, err := hookTaskStore(root, config)
	if err != nil {
		return nil, err
	}
	repo, err := taskflow.ResolveRepository(ctx, dir, gateGit)
	if err != nil {
		return nil, err
	}
	return taskflow.FindOpen(ctx, store, repo, time.Now(), func(context.Context, string) (string, error) { return "", nil })
}

// gateSuggestedMarker is the file that records that session start suggested
// cw gate install for the task id, or "" when there is no cache or the ID is
// not a plain file name.
func gateSuggestedMarker(cache, id string) string {
	if cache == "" || id == "" || id == "." || id == ".." || strings.ContainsAny(id, `/\`) {
		return ""
	}
	return filepath.Join(cache, gateSuggestedDirectory, id)
}

// gateInstallNote returns gateInstallLine when the repository at dir has an
// open substantial task for which session start has not suggested cw gate
// install yet, and its git hooks are absent; it then records the suggestion
// for every open task under cache. A task whose suggestion cannot be recorded
// gets the line again next time. It returns "" otherwise: when the gate is
// off, when a hook cw gate install did not write is there (a hook manager's,
// which may already run the gate), when the hooks directory serves other
// repositories too (cw gate install would refuse it without --shared), and on
// any failure.
func gateInstallNote(ctx context.Context, root *cobra.Command, config Config, dir, cache string) string {
	if config.Getenv(enforceVerifyOption) == "false" {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, hookGateNoteTimeout)
	defer cancel()
	open, err := openTasks(ctx, root, config, dir)
	if err != nil {
		return ""
	}
	var unsuggested []string
	for _, record := range open {
		marker := gateSuggestedMarker(cache, record.Record.ID)
		if _, err := os.Lstat(marker); marker == "" || err != nil {
			unsuggested = append(unsuggested, marker)
		}
	}
	if len(unsuggested) == 0 {
		return ""
	}
	if state, err := gitHooksState(ctx, dir); err != nil || state != gateAbsent {
		return ""
	}
	for _, marker := range unsuggested {
		if marker == "" || os.MkdirAll(filepath.Dir(marker), 0o700) != nil {
			continue
		}
		if file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE, 0o600); err == nil {
			file.Close()
		}
	}
	return gateInstallLine
}

// gitHooksCheck is cw doctor's git_hooks check for the repository at dir.
// Absent hooks need attention (missing) only while a substantial task is
// open there and the gate is on; a hook cw gate install did not write, and a
// hooks directory other repositories use too, are reported as detected, with
// a hint.
func gitHooksCheck(ctx context.Context, root *cobra.Command, config Config, dir string) doctor.Check {
	check := func(status, detail, source string) doctor.Check {
		return doctor.Check{Status: status, Detail: detail, Source: source}
	}
	if config.Getenv(enforceVerifyOption) == "false" {
		return check("not_applicable", "The commit gate is off ("+enforceVerifyOption+"=false), so git's hooks were not inspected", "plugin_option")
	}
	state, err := gitHooksState(ctx, dir)
	switch {
	case err != nil:
		return check("unchecked", "The git hooks directory could not be located, for example outside a Git repository; raw diagnostics were suppressed", "git_hooks_directory")
	case state == gateInstalled:
		return check("configured", "cw gate install wrote git's pre-commit, pre-push and pre-merge-commit hooks, so git runs the commit gate however a commit, merge commit or push starts", "git_hooks_directory")
	case state == gateForeign:
		return check("detected", "A pre-commit, pre-push or pre-merge-commit hook that cw gate install did not write is in place; whether it runs the commit gate is unchecked, and cw gate install prints the line to add to it", "git_hooks_directory")
	case state == gateShared:
		return check("detected", "The git hooks directory serves other repositories too, so cw gate install leaves it unchanged without --shared; cw gate status says why", "git_hooks_directory")
	}
	open, err := openTasks(ctx, root, config, dir)
	switch {
	case err != nil:
		return check("unchecked", "Git's hooks do not run the commit gate, and whether an open substantial task governs this repository could not be determined; raw diagnostics were suppressed", "git_hooks_directory_and_task_store")
	case len(open) > 0:
		return check("missing", "An open substantial task governs this repository, but git's hooks do not run the commit gate, so a commit or push from a script, an alias or a terminal skips it; cw gate install adds them", "git_hooks_directory_and_task_store")
	}
	return check("not_applicable", "Git's hooks do not run the commit gate, and no open substantial task governs this repository; cw gate install adds them", "git_hooks_directory_and_task_store")
}
