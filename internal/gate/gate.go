// Package gate decides whether a shell command an agent is about to run may
// publish: it finds each git commit and git push in the command, the
// repository each one publishes from and the task records that govern it,
// and answers allow, ask or deny with the reason shown to the agent and the
// user. The hook's input and output formats belong to the caller; the gate
// reaches git, the clock and the task store only through Deps.
//
// The gate fails open: every condition that is not a positive decision,
// including any git or store failure, allows the command.
package gate

import (
	"context"
	"fmt"
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
}

// TaskStore is the Store over a task store directory as cw task start
// writes it. It identifies the current source with taskflow.SourceDigest.
type TaskStore string

// FindOpen returns taskflow.FindOpen for the directory.
func (s TaskStore) FindOpen(ctx context.Context, repo taskflow.Repository, now time.Time) ([]taskflow.OpenRecord, error) {
	return taskflow.FindOpen(ctx, string(s), repo, now, taskflow.SourceDigest)
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
}

// Decide returns the decision for in. A deny for any publication in the
// command wins over an ask for another.
func Decide(ctx context.Context, in Input, deps Deps) Decision {
	if !in.Enforce && !in.Prompt {
		return Decision{}
	}
	publications := gitPublications(in.Command, in.CWD)
	if len(publications) == 0 {
		return Decision{}
	}
	d := decider{in: in, deps: deps, found: map[string][]taskflow.OpenRecord{}}
	var gating []taskflow.OpenRecord
	var asks []string
	for _, publication := range publications {
		blocking, ask := d.check(ctx, publication)
		for _, record := range blocking {
			if !slices.ContainsFunc(gating, func(seen taskflow.OpenRecord) bool { return seen.Record.ID == record.Record.ID }) {
				gating = append(gating, record)
			}
		}
		if ask != "" && !slices.Contains(asks, ask) {
			asks = append(asks, ask)
		}
	}
	switch {
	case in.Enforce && len(gating) > 0:
		return Decision{Permission: Deny, Reason: denyReason(gating)}
	case len(asks) > 0:
		return Decision{Permission: Ask, Reason: strings.Join(asks, " ")}
	}
	return Decision{}
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
// records cannot be read, yields neither.
func (d *decider) check(ctx context.Context, publication publication) ([]taskflow.OpenRecord, string) {
	repo, err := taskflow.ResolveRepository(ctx, publication.dir, d.deps.Git.output)
	if err != nil {
		return nil, ""
	}
	open, seen := d.found[repo.Worktree]
	if !seen {
		if open, err = d.deps.Store.FindOpen(ctx, repo, d.deps.Now()); err != nil {
			return nil, ""
		}
		d.found[repo.Worktree] = open
	}
	var gating []taskflow.OpenRecord
	for _, record := range open {
		if record.Gates() {
			gating = append(gating, record)
		}
	}
	switch {
	case len(gating) > 0:
		return gating, ""
	case len(open) > 0:
		if d.in.Enforce && publication.push && uncommittedTrackedChanges(ctx, d.deps.Git, repo.Worktree) {
			return nil, uncommittedReason(open)
		}
	case d.in.Prompt:
		if signs := substantialChangeSigns(ctx, d.deps.Git, repo.Worktree, publication); len(signs) >= 2 {
			return nil, unrecordedChangeReason(repo.Worktree, publication, signs)
		}
	}
	return nil, ""
}

// denyReason names every gating record, why it gates, and how to verify or
// abandon it.
func denyReason(gating []taskflow.OpenRecord) string {
	var reason strings.Builder
	reason.WriteString("agent-workflows: git commit and push stay blocked while a task for this repository is open: each must pass cw task verify on the source being published, or be closed.")
	for _, open := range gating {
		record := open.Record
		if open.Legacy {
			// Verify and abandon cannot load it; the problem names the way out.
			fmt.Fprintf(&reason, " Task %s (%s): %s.", record.ID, record.Plan.Title, open.Problem)
			continue
		}
		fmt.Fprintf(&reason, " Task %s (%s): %s; run `cw task verify --task %s --summary`, or close it with `cw task abandon --task %s --reason TEXT` if it is finished or was given up.", record.ID, record.Plan.Title, open.Problem, record.Directory, record.Directory)
	}
	reason.WriteString(" To turn the gate off, set enforce_verify=false in the plugin config.")
	return reason.String()
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
