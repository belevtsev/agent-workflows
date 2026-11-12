// Package gate decides whether a shell command an agent is about to run may
// publish: it finds each git commit and git push in the command, the
// repository each one publishes from and the task records that govern it,
// and answers allow, ask or deny with the reason shown to the agent and the
// user. It also asks before each `cw task abandon` in the command, which
// closes a task so it no longer gates: only the user may decide that.
// DecideGitHook answers the same for git's own pre-commit, pre-merge-commit
// and pre-push hooks. The hook's input and output formats belong to the caller; the gate
// reaches git, the clock and the task store only through Deps.
//
// The gate fails open: every condition that is not a positive decision,
// including any git or store failure, allows the command. A governing task
// record that cannot be read is not such a failure: it asks.
package gate

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// Input is one shell command the agent is about to run.
type Input struct {
	// Command is the shell command line.
	Command string
	// CWD is the directory the command starts in.
	CWD string
	// Home is the shell's HOME, which a leading `~` or `$HOME` in a cd or
	// -C argument names; "" when unknown, which leaves such an argument
	// unresolved.
	Home string
	// Enforce turns on the deny while a record gates and the ask before
	// pushing uncommitted changes over a verified record.
	Enforce bool
	// Prompt turns on the ask for a substantial-looking change that no
	// record covers.
	Prompt bool
}

// Deps are the gate's dependencies on its surroundings. Every member is
// required.
type Deps struct {
	// Git runs git; ExecGit runs the real one.
	Git Git
	// Now is the time records age against.
	Now func() time.Time
	// Store finds the records that govern a repository; TaskStore reads a
	// task store directory.
	Store Store
}

// Store finds the task records that govern publication from a repository.
type Store interface {
	// FindOpen returns the records that govern publication from repo at
	// now, as taskflow.FindOpen does.
	FindOpen(ctx context.Context, repo taskflow.Repository, now time.Time) ([]taskflow.OpenRecord, error)
	// Lookup returns the record in the task directory dir, an absolute
	// path, as `cw task abandon --task dir` reads it.
	Lookup(dir string) (taskflow.Record, error)
}

// TaskStore is the Store over a task store directory as cw task start
// writes it. It identifies the current source with taskflow.SourceDigest.
type TaskStore string

// FindOpen returns taskflow.FindOpen for the directory.
func (s TaskStore) FindOpen(ctx context.Context, repo taskflow.Repository, now time.Time) ([]taskflow.OpenRecord, error) {
	return taskflow.FindOpen(ctx, string(s), repo, now, taskflow.SourceDigest)
}

// Lookup returns taskflow.Load for dir, which need not be in the store
// directory: cw task abandon takes any task directory.
func (TaskStore) Lookup(dir string) (taskflow.Record, error) {
	return taskflow.Load(dir)
}

// Permission is the gate's answer for a command. Ask and Deny are spelled
// as PreToolUse permission decisions.
type Permission string

const (
	// Allow lets the command run without output.
	Allow Permission = ""
	// Ask lets the command run once the user confirms it.
	Ask Permission = "ask"
	// Deny blocks the command.
	Deny Permission = "deny"
)

// Decision is the gate's answer with the reason for an ask or a deny.
type Decision struct {
	Permission Permission
	// Reason tells the agent and the user why the command is asked about or
	// denied, and what to do; it is empty for Allow.
	Reason string
	// ProceedWithoutAsk marks an Ask that a host unable to ask, such as
	// Codex, lets run instead of denying, because a deny would block the
	// user on something they cannot fix there, such as a task record that
	// cannot be read. The ask for such a record sets it; one that joins asks
	// sets it only when every joined ask does.
	ProceedWithoutAsk bool
}

// Decide returns the decision for in. A deny for any publication in the
// command wins over an ask for another. A record that gates only
// publications after a directory change the gate cannot follow asks
// instead of denying. With enforce on, a `cw task abandon` in the command
// asks, joined with any ask its publications raise, unless a record gates
// one of them; a host that cannot ask denies it.
func Decide(ctx context.Context, in Input, deps Deps) Decision {
	if !in.Enforce && !in.Prompt {
		return Decision{}
	}
	publications := gitPublications(in.Command, in.CWD, in.Home)
	var abandons []abandonment
	if in.Enforce {
		abandons = abandonments(in.Command, in.CWD, in.Home)
	}
	if len(publications) == 0 && len(abandons) == 0 {
		return Decision{}
	}
	d := decider{in: in, deps: deps, found: map[string][]taskflow.OpenRecord{}}
	var gating []taskflow.OpenRecord
	var asks []ask
	for _, publication := range publications {
		blocking, question := d.check(ctx, publication)
		for _, record := range blocking {
			if !slices.ContainsFunc(gating, func(seen taskflow.OpenRecord) bool { return seen.Record.ID == record.Record.ID }) {
				gating = append(gating, record)
			}
		}
		if question.reason != "" && !slices.Contains(asks, question) {
			asks = append(asks, question)
		}
	}
	if len(abandons) > 0 && len(gating) == 0 {
		// A gating record denies anyway, so the abandoned records need no
		// reading then.
		asks = append([]ask{{reason: abandonReason(deps.Store, abandons)}}, asks...)
	}
	return conclude(in.Enforce, gating, asks)
}

// GitHookInput is the commit or push that git is about to make, as its
// pre-commit, pre-merge-commit or pre-push hook sees it.
type GitHookInput struct {
	// Dir is the directory git runs the hook in: the top of the worktree.
	Dir string
	// Push is true for the pre-push hook and false for pre-commit and
	// pre-merge-commit, which git runs before a merge commit.
	Push bool
	// MergeHeads, for pre-merge-commit only, are the commits the merge
	// commit takes in besides HEAD (MergeHeads reads them from git's
	// environment).
	MergeHeads []string
	// Enforce and Prompt are Input's.
	Enforce, Prompt bool
}

// DecideGitHook returns the decision for in, as Decide returns it for one
// git commit or git push run in in.Dir: the hook knows the directory, so
// nothing asks about a directory change. A commit is measured by the index
// git commits, which git's pre-commit and pre-merge-commit hooks name in
// GIT_INDEX_FILE and the gate's git inherits, so a commit with -a or with
// paths counts what it records. A merge commit whose every merge head is
// reachable from a remote-tracking ref, such as a merge of the task's base
// branch, is allowed: it takes in published work, not local work no record
// covers. Any other merge commit is judged as a commit.
func DecideGitHook(ctx context.Context, in GitHookInput, deps Deps) Decision {
	if !in.Enforce && !in.Prompt || fromRemote(ctx, deps.Git, in.Dir, in.MergeHeads) {
		return Decision{}
	}
	d := decider{in: Input{Enforce: in.Enforce, Prompt: in.Prompt}, deps: deps, found: map[string][]taskflow.OpenRecord{}}
	gating, question := d.check(ctx, publication{dir: in.Dir, push: in.Push})
	var asks []ask
	if question.reason != "" {
		asks = append(asks, question)
	}
	return conclude(in.Enforce, gating, asks)
}

// mergeHeadPrefix starts the name of each variable in which git merge (and
// git pull through it) names a commit it merges in, as GITHEAD_<object
// name>=<name as given>, for its hooks too. pre-merge-commit runs before git
// writes MERGE_HEAD, so these variables are where the hook finds the merge
// heads.
const mergeHeadPrefix = "GITHEAD_"

// MergeHeads returns the merge heads git names in environ, a process
// environment as os.Environ returns it: the object name in each
// GITHEAD_<object name> variable, once, in the order found. A name that is
// not a full SHA-1 or SHA-256 object name in lower-case hexadecimal is
// skipped.
func MergeHeads(environ []string) []string {
	var heads []string
	for _, variable := range environ {
		name, _, _ := strings.Cut(variable, "=")
		head, ok := strings.CutPrefix(name, mergeHeadPrefix)
		if ok && isObjectName(head) && !slices.Contains(heads, head) {
			heads = append(heads, head)
		}
	}
	return heads
}

// isObjectName reports whether s is a full SHA-1 or SHA-256 object name in
// lower-case hexadecimal.
func isObjectName(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	return strings.Trim(s, "0123456789abcdef") == ""
}

// fromRemote reports whether heads is not empty and each of them is
// reachable from a remote-tracking ref (refs/remotes/) of the repository at
// dir. A git failure counts as not reachable, so the merge commit is judged
// as a commit.
func fromRemote(ctx context.Context, git Git, dir string, heads []string) bool {
	for _, head := range heads {
		if !isObjectName(head) {
			return false
		}
		if _, err := git.output(ctx, dir, "for-each-ref", "--count=1", "--format=%(refname)", "--contains", head, "refs/remotes/"); err != nil {
			return false
		}
	}
	return len(heads) > 0
}

// conclude returns the decision for the records that gate a command's
// publications and the asks they raise.
func conclude(enforce bool, gating []taskflow.OpenRecord, asks []ask) Decision {
	switch {
	case enforce && len(gating) > 0:
		return Decision{Permission: Deny, Reason: denyReason(gating)}
	case len(asks) > 0:
		decision := Decision{Permission: Ask, ProceedWithoutAsk: true}
		reasons := make([]string, 0, len(asks))
		for _, question := range asks {
			reasons = append(reasons, question.reason)
			decision.ProceedWithoutAsk = decision.ProceedWithoutAsk && question.proceed
		}
		decision.Reason = strings.Join(reasons, " ")
		return decision
	}
	return Decision{}
}

// ask is one publication's question for the user, and whether a host that
// cannot ask lets the command run instead (Decision.ProceedWithoutAsk).
type ask struct {
	reason  string
	proceed bool
}

// decider holds one Decide call's state.
type decider struct {
	in   Input
	deps Deps
	// found holds the open records per worktree: `git commit && git push`
	// publishes from one worktree twice; find its records, and digest its
	// source, once.
	found map[string][]taskflow.OpenRecord
}

// check returns the records that gate one publication and, when none does,
// the ask it raises, if any. A repository that cannot be resolved, or whose
// store cannot be read, yields neither. With enforce on, a governing record
// that cannot be read asks, unless another record gates, joined with any ask
// the publication raises without it; with enforce off, such a record counts
// as no record. Records that gate a publication marked unresolved, which
// the gate checks in the session's directory for want of its own, ask
// instead of gating; a host that cannot ask denies.
func (d *decider) check(ctx context.Context, publication publication) ([]taskflow.OpenRecord, ask) {
	repo, err := taskflow.ResolveRepository(ctx, publication.dir, d.deps.Git.output)
	if err != nil {
		return nil, ask{}
	}
	open, seen := d.found[repo.Worktree]
	if !seen {
		if open, err = d.deps.Store.FindOpen(ctx, repo, d.deps.Now()); err != nil {
			return nil, ask{}
		}
		d.found[repo.Worktree] = open
	}
	var gating, unreadable, verified []taskflow.OpenRecord
	for _, record := range open {
		switch {
		case record.Gates():
			gating = append(gating, record)
		case record.NeedsConfirmation():
			unreadable = append(unreadable, record)
		default:
			verified = append(verified, record)
		}
	}
	if len(gating) > 0 {
		if publication.unresolved == "" || !d.in.Enforce {
			return gating, ask{}
		}
		return nil, ask{reason: unresolvedReason(publication.unresolved, repo.Worktree, gating)}
	}
	// other is the ask the publication raises as if the unreadable records
	// did not exist.
	other := ""
	switch {
	case len(verified) > 0:
		if d.in.Enforce && publication.push && uncommittedTrackedChanges(ctx, d.deps.Git, repo.Worktree) {
			other = uncommittedReason(verified)
		}
	case d.in.Prompt:
		if signs := substantialChangeSigns(ctx, d.deps.Git, repo.Worktree, publication); len(signs) >= 2 {
			other = unrecordedChangeReason(repo.Worktree, publication, signs, len(unreadable) > 0)
		}
	}
	switch {
	case !d.in.Enforce || len(unreadable) == 0:
		return nil, ask{reason: other}
	case other != "":
		// The other ask keeps its effect: a host that cannot ask denies.
		return nil, ask{reason: other + " " + unreadableReason(unreadable)}
	}
	// A deny would block the user on a file the agent's host may give them
	// no way to repair, so a host that cannot ask lets it run.
	return nil, ask{reason: unreadableReason(unreadable), proceed: true}
}

// denyReason names the first gating records, why each gates, how to verify
// it, and that abandoning it needs the user's confirmation, and counts the
// rest.
func denyReason(gating []taskflow.OpenRecord) string {
	var reason strings.Builder
	reason.WriteString("agent-workflows: git commit and push stay blocked while a task for this repository is open: each must pass cw task verify on the source being published, or be closed.")
	writeGating(&reason, gating)
	reason.WriteString(" To turn the gate off, set enforce_verify=false in the plugin config.")
	return reason.String()
}

// namedGating is how many gating records a reason names. The rest are
// counted, so that the reason stays short however many tasks are open.
const namedGating = 3

// writeGating writes, for each of the first namedGating gating records, why
// it gates and how to verify it, naming its directory once; then that
// abandoning a task needs the user's confirmation; then how many more gate,
// and the task store they are in.
func writeGating(reason *strings.Builder, gating []taskflow.OpenRecord) {
	n := min(len(gating), namedGating)
	named, rest := gating[:n], gating[n:]
	abandonable := false
	for _, open := range named {
		record := open.Record
		if open.Legacy {
			// Verify and abandon cannot load it; the problem names the way out.
			fmt.Fprintf(reason, " Task %s (%s): %s.", record.ID, record.Plan.Title, open.Problem)
			continue
		}
		abandonable = true
		fmt.Fprintf(reason, " Task %s (%s): %s; run `cw task verify --task %s --summary`.", record.ID, record.Plan.Title, open.Problem, record.Directory)
	}
	if abandonable {
		reason.WriteString(" For a task that is finished or was given up, ask the user before abandoning it: `cw task abandon --task <task-dir> --reason TEXT` needs their confirmation.")
	}
	if len(rest) == 0 {
		return
	}
	var stores []string
	legacy := false
	for _, open := range rest {
		if store := filepath.Dir(open.Record.Directory); !slices.Contains(stores, store) {
			stores = append(stores, store)
		}
		legacy = legacy || open.Legacy
	}
	tasks := "tasks"
	if len(rest) == 1 {
		tasks = "task"
	}
	// U1 (`cw task list`) should point here at `cw task list` instead of the
	// task store, once it exists.
	fmt.Fprintf(reason, " And %d more open %s in %s; each needs `cw task verify` or the user's confirmation to abandon", len(rest), tasks, strings.Join(stores, ", "))
	if legacy {
		reason.WriteString(", or, if this cw cannot read its record, moving its directory out of the task store")
	}
	reason.WriteString(".")
}

// unresolvedReason explains the ask for a publication after the directory
// change unresolved, which the gate could not follow: it checked the session
// directory's repository repo instead, where gating records gate.
func unresolvedReason(unresolved, repo string, gating []taskflow.OpenRecord) string {
	var reason strings.Builder
	fmt.Fprintf(&reason, "agent-workflows: the commit and push gate cannot tell which directory `%s` leads to, so it checked the session's repository %s instead, where a task is open:", unresolved, repo)
	writeGating(&reason, gating)
	fmt.Fprintf(&reason, " Confirm only if the git commit or push does not run in %s.", repo)
	return reason.String()
}

// unreadableReason explains the ask for a publication that records cw cannot
// read may gate: it names each one's directory and load error, and how to
// clear it.
func unreadableReason(unreadable []taskflow.OpenRecord) string {
	var reason strings.Builder
	reason.WriteString("agent-workflows: cw cannot read a task record for this repository, so the commit and push gate cannot tell whether it allows this command.")
	for _, open := range unreadable {
		fmt.Fprintf(&reason, " Task directory %s: %s", open.Record.Directory, open.Unreadable)
		if open.Newer {
			// The load error already says to upgrade cw.
			reason.WriteString(".")
			continue
		}
		reason.WriteString("; if the record is damaged, remove the directory or move it out of the task store, then start a new task with `cw task start` if the work is still open.")
	}
	reason.WriteString(" Confirm to run this command without the gate.")
	return reason.String()
}

// abandonReason explains the ask for a command that abandons tasks: it names
// each one's ID, title and directory where store can read it, else what the
// gate knows of its directory.
func abandonReason(store Store, abandons []abandonment) string {
	tasks := make([]string, 0, len(abandons))
	for _, abandon := range abandons {
		switch {
		case abandon.dir != "":
			if record, err := store.Lookup(abandon.dir); err == nil {
				tasks = append(tasks, fmt.Sprintf("task %s (%s) in %s", record.ID, record.Plan.Title, abandon.dir))
				continue
			}
			tasks = append(tasks, "the task in "+abandon.dir)
		case abandon.task != "":
			tasks = append(tasks, fmt.Sprintf("the task in `%s`, a directory the gate cannot resolve", abandon.task))
		default:
			tasks = append(tasks, "a task the gate cannot identify")
		}
	}
	return "agent-workflows: `cw task abandon` closes a task so that it no longer gates git commit and push, and needs the user's confirmation. " +
		"This command abandons " + strings.Join(tasks, "; ") + ". Confirm only if the user wants it abandoned because it is finished or was given up."
}

// uncommittedReason explains the ask for a push whose verified records may
// not cover what is pushed.
func uncommittedReason(open []taskflow.OpenRecord) string {
	tasks := make([]string, 0, len(open))
	for _, record := range open {
		tasks = append(tasks, fmt.Sprintf("%s (%s)", record.Record.ID, record.Record.Plan.Title))
	}
	subject := "task"
	if len(tasks) > 1 {
		subject = "tasks"
	}
	return fmt.Sprintf("agent-workflows: %s %s passed cw task verify on the working tree, but the working tree has uncommitted changes to tracked files, so the pushed commits may not hold the verified source. Commit the changes and verify again, or confirm to push anyway.", subject, strings.Join(tasks, ", "))
}
