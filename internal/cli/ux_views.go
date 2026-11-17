package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/change"
	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

type uxViewArgs struct {
	host, task string
	json       bool
	roots      stringList
}

func uxParseView(name string, args []string, extra ...string) (parsed, uxViewArgs, error) {
	flags, options := newFlags(name), uxViewArgs{}
	flags.BoolVar(&options.json, "json", false, "")
	for _, field := range extra {
		switch field {
		case "host":
			flags.StringVar(&options.host, "host", "", "")
		case "task":
			flags.StringVar(&options.task, "task", "", "")
		case "root":
			flags.Var(&options.roots, "root", "")
		}
	}
	p, err := parseArgs(flags, args)
	if err == nil && (p.dash || options.task != "" && !taskName.MatchString(options.task)) {
		err = usagef("invalid arguments or task name")
	}
	return p, options, err
}

func runUXRoleShow(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("role show", args, "host", "task")
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || !roles.Known(p.pos[0]) && p.pos[0] != roles.Coordinator {
		return usagef("show requires one known role, including coordinator")
	}
	config, err := rolesConfig(ctx)
	if err != nil {
		return err
	}
	access, err := roleAdviserAccess(ctx, options.task)
	if err != nil {
		return failf("%v", err)
	}
	id := p.pos[0]
	selected := roles.Role{ID: id, Job: "Coordinates roles and records", Writes: "workflow coordination records", Models: map[string]roles.Model{}}
	for _, host := range roles.Hosts {
		selected.Models[host] = roles.Model{Model: "host session", Effort: "host setting"}
	}
	for _, role := range roles.All(config) {
		if role.ID == id {
			selected = role
		}
	}
	if options.host != "" && !slices.Contains(roles.Hosts, options.host) {
		return usagef("--host must be codex or claude")
	}
	if options.json {
		models, boundSkills := selected.Models, roleSkills(config, id)
		if options.host != "" {
			models = map[string]roles.Model{options.host: models[options.host]}
			boundSkills = map[string][]string{options.host: boundSkills[options.host]}
		}
		return printJSON(cfg, struct {
			Role     string                 `json:"role"`
			Job      string                 `json:"job"`
			Writes   string                 `json:"writes"`
			Models   map[string]roles.Model `json:"models"`
			Skills   map[string][]string    `json:"skills"`
			Advisers []string               `json:"advisers"`
		}{id, selected.Job, selected.Writes, models, boundSkills, allowedAdviserList(access, id)})
	}
	fmt.Fprintf(cfg.Out, "%s: %s\nMay change: %s\n", id, selected.Job, selected.Writes)
	for _, host := range roles.Hosts {
		if options.host != "" && host != options.host {
			continue
		}
		model := selected.Models[host]
		writeRoleLine(cfg.Out, fmt.Sprintf("%s: %s, reasoning %s", host, model.Model, model.Effort))
		writeRoleLine(cfg.Out, "  Skills: "+skillListText(selectedSkills(config, host, id)))
	}
	writeRoleLine(cfg.Out, "Advisers: "+adviserListText(access, id))
	return nil
}

func runUXSkillShow(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("skill show", args, "host", "root")
	if err != nil {
		return err
	}
	if len(p.pos) != 1 {
		return usagef("show requires one skill reference")
	}
	root, err := skillWorkingRoot(ctx)
	if err != nil {
		return failf("%v", err)
	}
	if filepath.IsAbs(p.pos[0]) {
		dir := p.pos[0]
		if filepath.Base(dir) == "SKILL.md" {
			dir = filepath.Dir(dir)
		}
		options.roots = append(options.roots, dir)
	}
	catalog, err := skills.DiscoverWithRoots(root, options.host, options.roots)
	if err != nil {
		return failf("%v", err)
	}
	var found []skills.Entry
	for _, entry := range catalog.Skills {
		if entry.ID == p.pos[0] || uxSameSkillPath(entry.Path, p.pos[0]) {
			found = []skills.Entry{entry}
			break
		}
		if entry.Name == p.pos[0] {
			found = append(found, entry)
		}
	}
	if len(found) != 1 {
		return failf("skill %q is missing or ambiguous; use an id from `kitsu skill list`", p.pos[0])
	}
	catalog.Skills = found
	return printSkills(cfg, options.host, catalog, options.json)
}

func uxSameSkillPath(path, ref string) bool {
	if !filepath.IsAbs(ref) {
		return false
	}
	if filepath.Base(ref) != "SKILL.md" {
		ref = filepath.Join(ref, "SKILL.md")
	}
	physical := func(value string) string {
		if resolved, err := filepath.EvalSymlinks(value); err == nil {
			return filepath.Clean(resolved)
		}
		return filepath.Clean(value)
	}
	return physical(path) == physical(ref)
}

type uxTaskView struct {
	Task         string                `json:"task"`
	Workflow     string                `json:"workflow"`
	Stage        string                `json:"stage"`
	Report       *change.Report        `json:"report,omitempty"`
	Verification *taskVerificationView `json:"verification,omitempty"`
	UsageBudget  []string              `json:"usage_budget_reference,omitempty"`
}

func uxTaskViews(ctx context.Context, s *session, named string) ([]uxTaskView, error) {
	tasks := []string{named}
	if named == "" {
		var err error
		tasks, err = uxSavedTasks(s.repo.Root)
		if err != nil {
			return nil, err
		}
	}
	views := []uxTaskView{}
	var gitOf func(*change.Folder) (change.Git, error)
	for _, task := range tasks {
		problems, err := skills.ValidateSelections(s.repo.Root, task)
		if err != nil || len(problems) > 0 {
			return nil, fmt.Errorf("unsafe or invalid skill history for %s: %v %s", task, err, strings.Join(problems, "; "))
		}
		folder, err := change.Read(s.repo.Root, task)
		if errors.Is(err, change.ErrNoFolder) {
			view, err := uxSmallTaskView(ctx, s, task)
			if err != nil {
				return nil, err
			}
			views = append(views, view)
			continue
		}
		if err != nil {
			return nil, err
		}
		if gitOf == nil {
			gitOf, err = s.changeGit(ctx)
			if err != nil {
				return nil, err
			}
		}
		report, err := uxTaskReport(s, folder, gitOf)
		if err != nil {
			return nil, err
		}
		v, err := observeTaskVerification(ctx, s, task)
		if err != nil {
			return nil, err
		}
		applyTaskVerification(report, v)
		views = append(views, uxTaskView{Task: task, Workflow: "full", Stage: report.Stage, Report: report, Verification: v})
	}
	return appendUsageBudgetReferences(s.repo.Root, views)
}

func uxTaskReport(s *session, folder *change.Folder, gitOf func(*change.Folder) (change.Git, error)) (*change.Report, error) {
	state, err := gitOf(folder)
	if err != nil {
		return nil, err
	}
	report, err := change.Status(folder, state)
	if err == nil {
		err = applyAdviserRequirements(s.repo.Root, report, state.Committed)
	}
	return report, err
}

func runUXTaskList(ctx context.Context, cfg Config, args []string) error {
	return uxPrintTasks(ctx, cfg, args, false)
}

func runUXTaskShow(ctx context.Context, cfg Config, args []string) error {
	return runCoordinatorTaskShow(ctx, cfg, args)
}

func uxPrintTasks(ctx context.Context, cfg Config, args []string, named bool) error {
	p, options, err := uxParseView("task", args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 && !named || named && (len(p.pos) != 1 || !taskName.MatchString(p.pos[0])) {
		return usagef("list accepts no task; show requires one valid task name")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	task := ""
	if named {
		task = p.pos[0]
	}
	views, err := uxTaskViews(ctx, s, task)
	if err != nil {
		return failf("%v", err)
	}
	if options.json {
		if named {
			return printJSON(cfg, views[0])
		}
		return printJSON(cfg, map[string]any{"tasks": views})
	}
	if len(views) == 0 {
		fmt.Fprintln(cfg.Out, "No saved task records.")
	}
	for _, view := range views {
		fmt.Fprintf(cfg.Out, "%s: %s (%s)\n", view.Task, view.Stage, view.Workflow)
		if named {
			printTaskVerificationSummary(cfg, view.Verification)
		}
		if view.Workflow == "small" {
			fmt.Fprintln(cfg.Out, "  Saved history only; the workflow stage cannot be recovered from these files.")
		} else if named {
			for _, missing := range view.Report.Missing {
				fmt.Fprintln(cfg.Out, "  "+missing)
			}
			fmt.Fprintf(cfg.Out, "content ids: spec %s, design %s, code %s\n", orNone(view.Report.Content.Spec), orNone(view.Report.Content.Design), orNone(view.Report.Content.Code))
			if view.Report.NotAutomated > 0 || view.Report.BrokenRules > 0 {
				fmt.Fprintf(cfg.Out, "scenarios not automated: %d; broken rules: %d (kitsu task check %s)\n", view.Report.NotAutomated, view.Report.BrokenRules, view.Task)
			}
		}
	}
	return nil
}

func runUXCheckList(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("check list", args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 {
		return usagef("list accepts no arguments")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(s.repo.Root, decl.FileName))
	if err != nil {
		return failf("%v", err)
	}
	file, err := decl.Parse(data)
	if err != nil {
		return failf("%v", err)
	}
	if options.json {
		return printJSON(cfg, file)
	}
	for _, check := range file.Checks {
		fmt.Fprintf(cfg.Out, "%s  %s  (timeout %s)\n", check.ID, decl.Plain(strings.Join(check.Run, " ")), check.Timeout)
		if len(check.When) > 0 || check.Verify || check.Expect != "" {
			fmt.Fprintf(cfg.Out, "  when: %s; verify: %t; expect: %s\n", decl.Plain(strings.Join(check.When, ", ")), check.Verify, decl.Plain(check.Expect))
		}
	}
	return nil
}
