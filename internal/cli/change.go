package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/belevtsev/agent-workflows/v3/internal/change"
)

const changeUsage = "usage: cw change status [<task>] [--json], or cw change check [<task>]"

// runChange reads the change folders under openspec/changes/.
func runChange(ctx context.Context, cfg Config, args []string) error {
	if len(args) == 0 {
		return usagef("%s", changeUsage)
	}
	flags := newFlags("change " + args[0])
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args[1:])
	if err != nil {
		return err
	}
	switch {
	case args[0] != "status" && args[0] != "check":
		return usagef("unknown command `cw change %s`; %s", args[0], changeUsage)
	case len(p.pos) > 1 || p.dash || (*asJSON && args[0] == "check"):
		return usagef("%s", changeUsage)
	case len(p.pos) == 1 && !taskName.MatchString(p.pos[0]):
		return usagef("%q is not the name of a task: lower-case words joined by hyphens", p.pos[0])
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	tasks := p.pos
	if len(tasks) == 0 {
		if tasks, err = change.List(s.repo.Root); err != nil {
			return failf("%v", err)
		}
	}
	var folders []*change.Folder
	for _, task := range tasks {
		folder, err := change.Read(s.repo.Root, task)
		if errors.Is(err, change.ErrNoFolder) {
			return failf("%s/%s/ does not exist", change.Dir, task)
		}
		if err != nil {
			return failf("%v", err)
		}
		folders = append(folders, folder)
	}
	if args[0] == "check" {
		return s.checkChanges(folders)
	}
	return s.changeStatus(ctx, folders, len(p.pos) == 1, *asJSON)
}

// checkChanges prints every rule the folders break.
func (s *session) checkChanges(folders []*change.Folder) error {
	out, broken := s.cfg.Out, 0
	for _, folder := range folders {
		problems, err := change.Check(folder)
		if err != nil {
			return failf("%v", err)
		}
		for _, problem := range problems {
			fmt.Fprintln(out, problem)
		}
		if broken += len(problems); len(problems) == 0 {
			fmt.Fprintf(out, "%s: the files obey their rules\n", folder.Task)
		}
	}
	switch {
	case len(folders) == 0:
		fmt.Fprintf(out, "%s/ holds no change folder\n", change.Dir)
	case broken > 0:
		fmt.Fprintf(out, "broken rules: %d; each line above names the file, the line and the rule\n", broken)
		return exit(ExitNotVerified)
	}
	return nil
}

// changeGit gathers what the stage of each folder needs from git. The code
// of the working tree is read once for all folders.
func (s *session) changeGit(ctx context.Context) (func(*change.Folder) (change.Git, error), error) {
	snapshot, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return nil, err
	}
	codeOf := func(tree string) (string, error) {
		code := change.NewCode()
		err := s.repo.Entries(ctx, tree, code.Add)
		return code.ID(), err
	}
	code, err := codeOf(snapshot)
	if err != nil {
		return nil, err
	}
	head, _, err := s.repo.TreeOf(ctx, "HEAD")
	if err != nil {
		return nil, err
	}
	committed := map[string]string{}
	return func(folder *change.Folder) (change.Git, error) {
		git := change.Git{Code: code}
		if head == "" {
			return git, nil
		}
		here, _, err := s.repo.Object(ctx, snapshot, folder.Rel)
		if err != nil {
			return git, err
		}
		there, found, err := s.repo.Object(ctx, head, folder.Rel)
		if git.InHead = found && here == there; err != nil || !git.InHead {
			return git, err
		}
		commit, found, err := s.repo.LastCommit(ctx, folder.Rel)
		if err != nil || !found {
			return git, err
		}
		if _, known := committed[commit]; !known {
			if committed[commit], err = codeOf(commit); err != nil {
				return git, err
			}
		}
		git.Committed = committed[commit]
		return git, nil
	}, nil
}

// changeStatus prints the stage of each folder. With a task named it also
// prints what is missing and the content ids.
func (s *session) changeStatus(ctx context.Context, folders []*change.Folder, named, asJSON bool) error {
	gitOf, err := s.changeGit(ctx)
	if err != nil {
		return failf("%v", err)
	}
	reports := []*change.Report{}
	for _, folder := range folders {
		git, err := gitOf(folder)
		if err != nil {
			return failf("%v", err)
		}
		report, err := change.Status(folder, git)
		if err != nil {
			return failf("%v", err)
		}
		reports = append(reports, report)
	}
	out := s.cfg.Out
	switch {
	case asJSON:
		var value any = map[string]any{"changes": reports}
		if named {
			value = reports[0]
		}
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return failf("%v", err)
		}
		fmt.Fprintln(out, string(data))
	case named:
		report := reports[0]
		fmt.Fprintf(out, "%s: %s\n", report.Task, report.Stage)
		for _, missing := range report.Missing {
			fmt.Fprintf(out, "  %s\n", missing)
		}
		if report.NotAutomated > 0 {
			fmt.Fprintf(out, "scenarios that are not automated: %d\n", report.NotAutomated)
		}
		if report.BrokenRules > 0 {
			fmt.Fprintf(out, "broken rules: %d; `cw change check %s` names them. The stage says how far the record is, not that its files are valid\n", report.BrokenRules, report.Task)
		}
		fmt.Fprintf(out, "content ids: spec %s, design %s, code %s\n", orNone(report.Content.Spec), orNone(report.Content.Design), orNone(report.Content.Code))
	case len(reports) == 0:
		fmt.Fprintf(out, "%s/ holds no change folder\n", change.Dir)
	default:
		for _, report := range reports {
			line := fmt.Sprintf("%s: %s", report.Task, report.Stage)
			if n := len(report.Missing); n > 0 {
				line += fmt.Sprintf(" (%d missing; `cw change status %s` names them)", n, report.Task)
			}
			if report.BrokenRules > 0 {
				line += fmt.Sprintf("; broken rules: %d (`cw change check %s`)", report.BrokenRules, report.Task)
			}
			fmt.Fprintln(out, line)
		}
	}
	return nil
}

func orNone(id string) string {
	if id == "" {
		return "none"
	}
	return id
}
