package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/gitx"
	"github.com/belevtsev/kitsu/v3/internal/roles"
)

// taskName is the form of a task's name: lower-case words joined by hyphens.
var taskName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// runRoles prints the roles or runs a role command.
func runRoles(ctx context.Context, cfg Config, args []string) error {
	if len(args) > 0 {
		switch args[0] {
		case "set", "reset":
			return runRolesEdit(ctx, cfg, args[1:], args[0] == "reset")
		case "check":
			return runRolesCheck(ctx, cfg, args[1:])
		case "text":
			return runRolesText(cfg, args[1:])
		}
	}
	flags := newFlags("roles")
	asJSON := flags.Bool("json", false, "")
	host, task := flags.String("host", "", ""), flags.String("task", "", "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) > 0 || p.dash || (*host != "" && *host != "claude" && *host != "codex") || (*task != "" && !taskName.MatchString(*task)) {
		return usagef("usage: kitsu roles [--host claude|codex] [--task <task>] [--json]; use `kitsu roles --help` for set, reset, check and text")
	}
	config, err := rolesConfig(ctx)
	if err != nil {
		return err
	}
	all := roles.All(config)
	access, err := roleAdviserAccess(ctx, *task)
	if err != nil {
		return failf("%v", err)
	}
	if *host != "" {
		return printHostRoles(cfg, all, *host, access, *asJSON)
	}
	if *asJSON {
		return printRolesJSON(cfg, all, access)
	}
	var table strings.Builder
	w := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ROLE\tCLAUDE CODE\tCODEX\tTEXT")
	for _, r := range all {
		fmt.Fprintf(w, "%s\t%s, %s\t%s, %s\t%d\n", r.ID,
			r.Models["claude"].Model, r.Models["claude"].Effort,
			r.Models["codex"].Model, r.Models["codex"].Effort, roles.TextSize(r.ID))
	}
	if err := w.Flush(); err != nil {
		return failf("%v", err)
	}
	// The job and the paths go below the row of a role, so that the table
	// fits a terminal (U15).
	rows := strings.Split(table.String(), "\n")
	fmt.Fprintln(cfg.Out, rows[0])
	for i, r := range all {
		fmt.Fprintf(cfg.Out, "%s\n    %s\n    May write: %s.\n", rows[i+1], r.Job, r.Writes)
		fmt.Fprintf(cfg.Out, "    Advisers: %s.\n", adviserListText(access, r.ID))
	}
	fmt.Fprintf(cfg.Out, "\nTEXT is the size of the role's text in characters; the coordinator's instructions have %d.\n", roles.TextSize(roles.Coordinator))
	fmt.Fprintf(cfg.Out, "Use `kitsu roles set <role> --host claude|codex --model <model> --reasoning <effort>`.\nOverrides and test paths live in %s.\n", roles.FileName)
	return nil
}

// rolesConfig reads kitsu-roles.toml of the repository the command runs in.
// Outside a repository the roles have their defaults.
func rolesConfig(ctx context.Context) (*roles.Config, error) {
	dir, err := os.Getwd()
	if err != nil {
		return nil, failf("%v", err)
	}
	repo, err := gitx.Open(ctx, dir)
	if err != nil || repo.Root == "" {
		return &roles.Config{}, nil
	}
	config, err := roles.Load(repo.Root)
	if err != nil {
		return nil, failf("%v", err)
	}
	return config, nil
}

func printRolesJSON(cfg Config, all []roles.Role, access map[string][]string) error {
	type entry struct {
		ID        string                 `json:"id"`
		Job       string                 `json:"job"`
		Writes    string                 `json:"writes"`
		TextChars int                    `json:"text_chars"`
		Advisers  []string               `json:"advisers"`
		Models    map[string]roles.Model `json:"models"`
	}
	out := struct {
		Roles            []entry `json:"roles"`
		CoordinatorChars int     `json:"coordinator_text_chars"`
	}{CoordinatorChars: roles.TextSize(roles.Coordinator)}
	for _, r := range all {
		out.Roles = append(out.Roles, entry{ID: r.ID, Job: r.Job, Writes: r.Writes, TextChars: roles.TextSize(r.ID), Models: r.Models, Advisers: allowedAdviserList(access, r.ID)})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return failf("%v", err)
	}
	fmt.Fprintln(cfg.Out, string(data))
	return nil
}

// runRolesText prints the one text of a role, for a host that has no role
// files of its own.
func runRolesText(cfg Config, args []string) error {
	p, err := parseArgs(newFlags("roles text"), args)
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || p.dash {
		return usagef("usage: kitsu roles text <role>; the roles are %s, and %s", strings.Join(roles.IDs(), ", "), roles.Coordinator)
	}
	text, err := roles.Text(p.pos[0])
	if err != nil {
		return usagef("%v", err)
	}
	fmt.Fprint(cfg.Out, text)
	return nil
}

// runRolesCheck compares what the working tree holds with a tree from
// before a role started, and says which paths the role changed outside its
// limit. It checks afterwards because no host can limit a shell to paths.
func runRolesCheck(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("roles check")
	from := flags.String("from", "", "")
	task := flags.String("task", "", "")
	finalize := flags.Bool("finalize", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	const usage = "usage: kitsu roles check <role> --from <tree> [--task <task>] [--finalize]"
	switch {
	case len(p.pos) != 1 || p.dash || *from == "":
		return usagef("%s", usage)
	case !roles.Known(p.pos[0]):
		return usagef("unknown role %q; the roles are %s", p.pos[0], strings.Join(roles.IDs(), ", "))
	case *task != "" && !taskName.MatchString(*task):
		return usagef("%q is not the name of a task: lower-case words joined by hyphens", *task)
	case *finalize && (p.pos[0] != "spec-writer" || *task == ""):
		return usagef("--finalize requires spec-writer and --task <task>; %s", usage)
	}
	role := p.pos[0]
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	config, err := roles.Load(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	changes, before, err := s.changedSince(ctx, *from)
	if err != nil {
		return failf("%v", err)
	}
	var outside []roles.Violation
	if *finalize {
		outside, err = s.checkFinalization(ctx, changes, before, *task)
	} else {
		outside, err = roles.Check(role, changes, config.Tests, *task)
	}
	switch {
	case errors.Is(err, roles.ErrNeedsTask):
		return usagef("%v; %s", err, usage)
	case err != nil:
		return failf("%v", err)
	}
	if gate.State(ctx, s.repo) != gate.Installed {
		s.warn([]string{hooksAdvice(ctx, s.repo)})
	}
	return printRoleCheck(cfg, role, *from, len(changes), outside)
}

func printRoleCheck(cfg Config, role, from string, changed int, outside []roles.Violation) error {
	if len(outside) == 0 {
		fmt.Fprintf(cfg.Out, "`%s` stayed within its limit: %s from %s\n", role, plural(changed, "path differs", "paths differ"), from)
		return nil
	}
	fmt.Fprintf(cfg.Out, "`%s` changed %d of %d paths outside its limit:\n", role, len(outside), changed)
	for _, v := range outside {
		fmt.Fprintf(cfg.Out, "  %s (%s): %s\n", v.Path, v.Kind, v.Why)
	}
	advice := "start `%s` again with these paths; do not accept the work and do not repair it yourself\n"
	for _, r := range roles.All(nil) {
		if r.ID == role && r.Writes == "nothing" {
			advice = "`%s` writes nothing: ask the developer to remove these paths, or remove them after their yes; do not accept the work\n"
		}
	}
	fmt.Fprintf(cfg.Out, advice, role)
	return exit(ExitNotVerified)
}

// changedSince lists the paths of the working tree, as it is on disk, that
// differ from a tree, and returns that starting tree's resolved id.
func (s *session) changedSince(ctx context.Context, from string) ([]gitx.Change, string, error) {
	before, found, err := s.repo.TreeOf(ctx, from)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "", fmt.Errorf("`%s` names no tree of this repository", from)
	}
	snapshot, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return nil, "", err
	}
	changes, err := s.repo.Diff(ctx, before, snapshot)
	return changes, before, err
}

func (s *session) checkFinalization(ctx context.Context, changes []gitx.Change, before, task string) ([]roles.Violation, error) {
	var entries []string
	err := s.repo.Entries(ctx, before, func(_ []byte, path string) {
		if strings.HasPrefix(path, roles.ChangeFolder(task)+"specs/") {
			entries = append(entries, path)
		}
	})
	if err != nil {
		return nil, err
	}
	return roles.CheckFinalization(changes, task, entries)
}
