// Package gate decides whether a shell command an agent is about to run may
// publish: it finds each git commit and git push in the command, the
// repository each one publishes from and the task records that govern it,
// and answers allow, ask or deny with the reason shown to the agent and the
// user. It also asks before each `cw task abandon` in the command, which
// closes a task so it no longer gates: only the user may decide that; and
// before each cw --owner-approval, whose words only the user may give.
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
	"strconv"
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
	var approvals []string
	if in.Enforce {
		abandons = abandonments(in.Command, in.CWD, in.Home)
		approvals = ownerApprovals(in.Command, in.CWD, in.Home)
	}
	if len(publications) == 0 && len(abandons) == 0 && len(approvals) == 0 {
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
	if len(approvals) > 0 && len(gating) == 0 {
		asks = append([]ask{{reason: approvalReason(approvals)}}, asks...)
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

// namedTasks is how many tasks a reason names. The rest are counted
// (countRest), so that the reason stays short however many tasks are open.
const namedTasks = 3

// splitNamed splits a reason's tasks into the first namedTasks, which it
// names, and the rest, which it counts with countListed or countRest.
func splitNamed[T any](tasks []T) (named, rest []T) {
	n := min(len(tasks), namedTasks)
	return tasks[:n], tasks[n:]
}

// countListed counts the task records of the repository that a reason does
// not name: "N more <noun>s (`cw task list` lists them)". `cw task list`,
// run in the repository, reads the task store the gate read and lists each
// of them, by ID and state.
func countListed(noun string, records []taskflow.OpenRecord) string {
	if len(records) == 1 {
		return "1 more " + noun + " (`cw task list` lists it)"
	}
	return fmt.Sprintf("%d more %ss (`cw task list` lists them)", len(records), noun)
}

// countRest counts the tasks an abandon reason does not name, given their task
// directories: "N more <noun>s in <stores>", the stores being the
// directories that hold them, in the order first seen. A "" in dirs is a
// task whose directory the gate does not know; then it names no store.
func countRest(noun string, dirs []string) string {
	var stores []string
	for _, dir := range dirs {
		if dir == "" {
			stores = nil
			break
		}
		if store := filepath.Dir(dir); !slices.Contains(stores, store) {
			stores = append(stores, store)
		}
	}
	if len(dirs) != 1 {
		noun += "s"
	}
	count := fmt.Sprintf("%d more %s", len(dirs), noun)
	if len(stores) > 0 {
		count += " in " + strings.Join(stores, ", ")
	}
	return count
}

// writeGating writes, for each of the first namedTasks gating records, why
// it gates and how to verify it, naming it by its ID, which --task takes in
// the task store the gate read; then that
// abandoning a task needs the user's confirmation; then how many more gate,
// which `cw task list` lists.
func writeGating(reason *strings.Builder, gating []taskflow.OpenRecord) {
	named, rest := splitNamed(gating)
	abandonable := false
	for _, open := range named {
		record := open.Record
		if open.Legacy {
			// Verify and abandon cannot load it; the problem names the way out.
			fmt.Fprintf(reason, " Task %s (%s): %s.", record.ID, record.Plan.Title, open.Problem)
			continue
		}
		abandonable = true
		fmt.Fprintf(reason, " Task %s (%s): %s; run `cw task verify --task %s`.", record.ID, record.Plan.Title, open.Problem, record.ID)
	}
	if abandonable {
		reason.WriteString(" For a task that is finished or was given up, ask the user before abandoning it: `cw task abandon --task <task-id> --reason TEXT` needs their confirmation.")
	}
	if len(rest) == 0 {
		return
	}
	legacy := slices.ContainsFunc(rest, func(open taskflow.OpenRecord) bool { return open.Legacy })
	fmt.Fprintf(reason, " And %s; each needs `cw task verify` or the user's confirmation to abandon", countListed("open task", rest))
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
// read may gate: it names the directory and load error of the first
// namedTasks, and how to clear each, and counts the rest.
func unreadableReason(unreadable []taskflow.OpenRecord) string {
	var reason strings.Builder
	reason.WriteString("agent-workflows: cw cannot read a task record for this repository, so the commit and push gate cannot tell whether it allows this command.")
	named, rest := splitNamed(unreadable)
	for _, open := range named {
		fmt.Fprintf(&reason, " Task directory %s: %s", open.Record.Directory, open.Unreadable)
		if open.Newer {
			// The load error already says to upgrade cw.
			reason.WriteString(".")
			continue
		}
		reason.WriteString("; if the record is damaged, remove the directory or move it out of the task store, then start a new task with `cw task start` if the work is still open.")
	}
	if len(rest) > 0 {
		fmt.Fprintf(&reason, " And %s; each needs a cw that can read it or, if it is damaged, its directory moved out of the task store.", countListed("unreadable task record", rest))
	}
	reason.WriteString(" Confirm to run this command without the gate.")
	return reason.String()
}

// abandonReason explains the ask for a command that abandons tasks: it names
// the first namedTasks by ID, title and directory where store can read it,
// else by what the gate knows of its directory, and counts the rest.
func abandonReason(store Store, abandons []abandonment) string {
	named, rest := splitNamed(abandons)
	tasks := make([]string, 0, len(named)+1)
	for _, abandon := range named {
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
	if len(rest) > 0 {
		dirs := make([]string, 0, len(rest))
		for _, abandon := range rest {
			dirs = append(dirs, abandon.dir)
		}
		tasks = append(tasks, "and "+countRest("task", dirs))
	}
	return "agent-workflows: `cw task abandon` closes a task so that it no longer gates git commit and push, and needs the user's confirmation. " +
		"This command abandons " + strings.Join(tasks, "; ") + ". Confirm only if the user wants it abandoned because it is finished or was given up."
}

// approvalReason explains the ask for a command that records the owner's
// approval: it quotes the first namedTasks texts, or says the gate cannot
// read one, and counts the rest.
func approvalReason(approvals []string) string {
	named, rest := splitNamed(approvals)
	quoted := make([]string, 0, len(named)+1)
	for _, text := range named {
		if text == "" {
			quoted = append(quoted, "words the gate cannot read")
			continue
		}
		quoted = append(quoted, strconv.Quote(text))
	}
	if len(rest) > 0 {
		quoted = append(quoted, fmt.Sprintf("and %d more", len(rest)))
	}
	return "agent-workflows: `--owner-approval` records the owner's words that let a task go past a limit of its profile, such as a stage's round cap, and only the user may give them. " +
		"This command records " + strings.Join(quoted, "; ") + " as the owner's approval. Confirm only if these are the user's own words."
}

// uncommittedReason explains the ask for a push whose verified records may
// not cover what is pushed: it names the first namedTasks and counts the
// rest.
func uncommittedReason(open []taskflow.OpenRecord) string {
	named, rest := splitNamed(open)
	tasks := make([]string, 0, len(named)+1)
	for _, record := range named {
		tasks = append(tasks, fmt.Sprintf("%s (%s)", record.Record.ID, record.Record.Plan.Title))
	}
	if len(rest) > 0 {
		tasks = append(tasks, "and "+countListed("task", rest))
	}
	subject := "task"
	if len(tasks) > 1 {
		subject = "tasks"
	}
	return fmt.Sprintf("agent-workflows: %s %s passed cw task verify on the working tree, but the working tree has uncommitted changes to tracked files, so the pushed commits may not hold the verified source. Commit the changes and verify again, or confirm to push anyway.", subject, strings.Join(tasks, ", "))
}
