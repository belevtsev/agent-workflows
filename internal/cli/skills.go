package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func runSkills(ctx context.Context, cfg Config, args []string) error {
	if len(args) == 0 || args[0] != "list" && args[0] != "resolve" {
		return usagef("usage: kitsu skills list --host claude|codex [--root <directory>] [--json] | resolve <role> --host claude|codex [--task <task>] [--workflow full|small] [--root <directory>] [--json]")
	}
	flags := newFlags("skills " + args[0])
	host := flags.String("host", "", "")
	asJSON := flags.Bool("json", false, "")
	var roots stringList
	flags.Var(&roots, "root", "")
	task, workflow := "", "full"
	if args[0] == "resolve" {
		flags.StringVar(&task, "task", "", "")
		flags.StringVar(&workflow, "workflow", "full", "")
	}
	p, err := parseArgs(flags, args[1:])
	if err != nil {
		return err
	}
	if uxFlagSet(flags, "host") && !uxValidHost(*host) {
		return usagef("--host must be codex or claude")
	}
	if err := validateSkillArguments(p, args[0], *host); err != nil {
		return err
	}
	workflowSet := false
	flags.Visit(func(f *flag.Flag) { workflowSet = workflowSet || f.Name == "workflow" })
	if (workflowSet && task == "") || (workflow != "full" && workflow != "small") || (task != "" && !taskName.MatchString(task)) {
		return usagef("--task must name a task; --workflow full|small requires --task")
	}
	root, err := skillWorkingRoot(ctx)
	if err != nil {
		return failf("%v", err)
	}
	if args[0] == "resolve" {
		return resolveRoleSkills(cfg, root, *host, p.pos[0], task, workflow, roots, *asJSON)
	}
	catalog, err := skills.DiscoverWithRoots(root, *host, roots)
	if err != nil {
		return failf("%v", err)
	}
	return printSkills(cfg, *host, catalog, *asJSON)
}

func validateSkillArguments(p parsed, mode, host string) error {
	validList := mode == "list" && uxValidHost(host) && len(p.pos) == 0
	validResolution := mode == "resolve" && (host == "" || uxValidHost(host)) && len(p.pos) == 1
	if p.dash || !validList && !validResolution {
		return usagef("skills %s requires --host claude|codex and %s; see `kitsu skills --help`", mode, map[string]string{"list": "no role", "resolve": "one role"}[mode])
	}
	return nil
}

func resolveRoleSkills(cfg Config, root, host, role, task, workflow string, roots []string, asJSON bool) error {
	if !roles.Known(role) && role != roles.Coordinator {
		return usagef("unknown role %q", role)
	}
	if task != "" && root == "" {
		return failf("recording a skill selection requires a git working tree")
	}
	config, err := roles.Load(root)
	if err != nil {
		return failf("%v", err)
	}
	host, err = roleSkillHost(config, host, role)
	if err != nil {
		return usagef("%v", err)
	}
	catalog, err := skills.DiscoverWithRoots(root, host, roots)
	if err != nil {
		return failf("%v", err)
	}
	refs := selectedSkills(config, host, role)
	entries, err := catalog.Resolve(refs)
	if err != nil {
		return failf("%v", err)
	}
	id := ""
	if task != "" {
		id, err = skills.RecordSelection(root, task, workflow, host, role, entries)
		if err != nil {
			return failf("%v", err)
		}
	}
	return printSkillResolution(cfg, host, role, refs, entries, id, asJSON)
}

func roleSkillHost(config *roles.Config, host, role string) (string, error) {
	if role != roles.Coordinator {
		return config.SpecialistHost(host, role)
	}
	if !uxValidHost(host) {
		return "", fmt.Errorf("coordinator skills require --host codex|claude")
	}
	return host, nil
}

func skillWorkingRoot(ctx context.Context) (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	repo, err := gitx.Open(ctx, dir)
	if errors.Is(err, gitx.ErrNotRepo) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return repo.Root, nil
}

func printSkills(cfg Config, host string, catalog skills.Catalog, asJSON bool) error {
	if asJSON {
		return printJSON(cfg, struct {
			Host string `json:"host"`
			skills.Catalog
		}{host, catalog})
	}
	for _, warning := range catalog.Warnings {
		fmt.Fprintln(cfg.Err, "kitsu: warning:", warning)
	}
	fmt.Fprintf(cfg.Out, "Skills available to %s:\n", host)
	for _, entry := range catalog.Skills {
		fmt.Fprintf(cfg.Out, "%s\n    %s: %s\n", entry.ID, entry.Name, entry.Description)
		fmt.Fprintf(cfg.Out, "    Source: %s\n", entry.Path)
		if entry.Disabled {
			fmt.Fprintf(cfg.Out, "    Disabled: %s\n", entry.Reason)
		}
		if entry.ManualOnly {
			fmt.Fprintln(cfg.Out, "    Manual-only: cannot be selected for automatic workers")
		}
	}
	return nil
}

func printSkillResolution(cfg Config, host, role string, refs []string, entries []skills.Entry, id string, asJSON bool) error {
	if entries == nil {
		entries = []skills.Entry{}
	}
	if asJSON {
		return printJSON(cfg, struct {
			Host         string         `json:"host"`
			Role         string         `json:"role"`
			SelectedRefs []string       `json:"selected_refs"`
			Skills       []skills.Entry `json:"skills"`
			SelectionID  string         `json:"selection_id,omitempty"`
			LoadStatus   string         `json:"load_status"`
		}{host, role, refs, entries, id, "unverified"})
	}
	fmt.Fprintf(cfg.Out, "Skill selection for %s on %s (loading unverified):\n", role, host)
	if id != "" {
		fmt.Fprintf(cfg.Out, "Selection: %s\n", id)
	}
	for _, entry := range entries {
		fmt.Fprintf(cfg.Out, "%s\n    Source: %s\n    SHA256: %s\n", entry.ID, entry.Path, entry.SHA256)
	}
	if len(entries) == 0 {
		fmt.Fprintln(cfg.Out, "No pinned skills.")
	}
	return nil
}
