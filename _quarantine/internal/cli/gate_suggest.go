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
// SessionStart hook timeout in hooks/hooks.json. It is a variable only so
// that tests can set it; cw never changes it.
var hookGateNoteTimeout = 5 * time.Second

// gateSuggestedDirectory is the directory under the session-start cache
// (--cache) that holds one empty file per task, named by its ID, for each
// task whose session start has suggested cw gate install.
const gateSuggestedDirectory = "gate-suggested"

// gateShared sums up a hooks directory that other repositories use too and
// that lacks a hook cw gate install would write there with --shared.
const gateShared = "shared"

// gateInstallLine is the session-start line for a repository with an open
// substantial task whose git hooks are all absent. The session-start context
// is paid for in every session, so it is one short line.
const gateInstallLine = "git hooks absent, so a commit or push from a script, an alias or a terminal skips the verify gate: suggest cw gate install to the user"

// gateInstallLineFor is the session-start line for a repository with an
// open substantial task and the hooks named in absent absent:
// gateInstallLine when all are, and otherwise one that names them and says
// what skips the gate, such as only a merge commit after an install by a cw
// that wrote no pre-merge-commit.
func gateInstallLineFor(absent []string) string {
	if len(absent) == len(gateHookNames) {
		return gateInstallLine
	}
	hooks := "git hook "
	if len(absent) > 1 {
		hooks = "git hooks "
	}
	return hooks + gateJoin(absent, "and") + " absent, so " + gateSkips(absent) + " skips the verify gate: suggest cw gate install to the user"
}

// gateAbsentHooks says which of git's hooks are absent, for a sentence of
// the doctor check: all of them, or by name, with its verb, and the pronoun
// that stands for them.
func gateAbsentHooks(absent []string) (hooks, pronoun string) {
	switch {
	case len(absent) == len(gateHookNames):
		return "git's hooks are absent", "them"
	case len(absent) == 1:
		return "git's " + absent[0] + " hook is absent", "it"
	}
	return "git's " + gateJoin(absent, "and") + " hooks are absent", "them"
}

// gateSkips says what skips the commit gate while the hooks named in absent
// are absent. cw hook pre-publish still checks a commit or push the agent runs
// in its shell tool, so only one from a script, an alias or a terminal skips
// it, but nothing else checks a merge commit that git makes without
// conflicts.
func gateSkips(absent []string) string {
	var verbs, what []string
	merge := false
	for _, name := range absent {
		switch name {
		case "pre-commit":
			verbs = append(verbs, "commit")
		case "pre-push":
			verbs = append(verbs, "push")
		case "pre-merge-commit":
			merge = true
		}
	}
	if len(verbs) > 0 {
		what = append(what, "a "+strings.Join(verbs, " or ")+" from a script, an alias or a terminal")
	}
	if merge {
		what = append(what, "a merge commit made without conflicts")
	}
	return strings.Join(what, ", or ")
}

// gateJoin joins words as a list in prose with the conjunction: "a", "a and
// b", "a, b and c".
func gateJoin(words []string, conjunction string) string {
	if len(words) < 2 {
		return strings.Join(words, "")
	}
	return strings.Join(words[:len(words)-1], ", ") + " " + conjunction + " " + words[len(words)-1]
}

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
// at dir, and the names of the hooks it reports absent.
func gitHooksState(ctx context.Context, dir string) (string, []string, error) {
	hooks, err := resolveGateHooks(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	report, err := gateStatus(hooks)
	if err != nil {
		return "", nil, err
	}
	var absent []string
	for _, hook := range report.Hooks {
		if hook.State == gateAbsent {
			absent = append(absent, hook.Name)
		}
	}
	return report.summary(), absent, nil
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
	return taskflow.FindOpen(ctx, store, repo, config.Now(), func(context.Context, string) (string, error) { return "", nil })
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

// gateInstallNote returns gateInstallLineFor the absent hooks when the
// repository at dir has an open substantial task for which session start has
// not suggested cw gate install yet, and a git hook is absent; it then
// records the suggestion for every open task under cache. A task whose
// suggestion cannot be recorded gets the line again next time. It returns ""
// otherwise: when the gate is off, when a hook cw gate install did not write
// is there (a hook manager's, which may already run the gate), when the
// hooks directory serves other repositories too (cw gate install would
// refuse it without --shared), and on any failure.
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
	state, absent, err := gitHooksState(ctx, dir)
	if err != nil || state != gateAbsent {
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
	return gateInstallLineFor(absent)
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
	state, absent, err := gitHooksState(ctx, dir)
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
	// A hook is absent from the repository's own hooks directory: name it,
	// and say what skips the gate without it.
	hooks, pronoun := gateAbsentHooks(absent)
	skips := hooks + ", so " + gateSkips(absent) + " skips the commit gate"
	open, err := openTasks(ctx, root, config, dir)
	switch {
	case err != nil:
		return check("unchecked", upperFirst(skips)+", and whether an open substantial task governs this repository could not be determined; raw diagnostics were suppressed", "git_hooks_directory_and_task_store")
	case len(open) > 0:
		return check("missing", "An open substantial task governs this repository, but "+skips+"; cw gate install adds "+pronoun, "git_hooks_directory_and_task_store")
	}
	return check("not_applicable", upperFirst(skips)+", and no open substantial task governs this repository; cw gate install adds "+pronoun, "git_hooks_directory_and_task_store")
}

// upperFirst returns s with its first byte in upper case; s starts with an
// ASCII letter.
func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
