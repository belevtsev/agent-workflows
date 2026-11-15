package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
	"github.com/belevtsev/kitsu/v3/internal/change"
)

func runAdvisers(ctx context.Context, cfg Config, args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		return runAdviserList(ctx, cfg, args[1:])
	case "bind":
		return runAdviserBind(ctx, cfg, args[1:])
	case "resolve":
		return runAdviserResolve(ctx, cfg, args[1:])
	case "status", "check":
		return runAdviserStatus(ctx, cfg, args[0], args[1:])
	default:
		return usagef("unknown adviser command %q; use list, bind, resolve, status or check", args[0])
	}
}

func runAdviserList(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("advisers list")
	role, task := flags.String("role", "", ""), flags.String("task", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash || (*role != "" && !advisers.ValidRole(*role)) {
		return usagef("usage: kitsu advisers list [--role <role>] [--task <task>] [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	if *task != "" {
		b, err := advisers.LoadBinding(s.repo.Root, *task)
		if err != nil {
			return failf("%v", err)
		}
		return printBoundAdvisers(cfg, b, *role, *asJSON)
	}
	settings, err := advisers.Load(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	if *role != "" {
		for name := range settings.Advisers {
			if !advisers.Allowed(settings.Access, *role, name) {
				delete(settings.Advisers, name)
			}
		}
	}
	if *asJSON {
		return printJSON(cfg, settings)
	}
	w := tabwriter.NewWriter(cfg.Out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ADVISER\tMODEL\tTIMEOUT\tSOURCE")
	for _, name := range sortedAdviserNames(settings.Advisers) {
		d := settings.Advisers[name]
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, d.Model, d.Timeout, d.Source)
	}
	return w.Flush()
}

func printBoundAdvisers(cfg Config, b *advisers.Binding, role string, asJSON bool) error {
	if role != "" {
		for name := range b.Advisers {
			if !advisers.Allowed(b.Access, role, name) {
				delete(b.Advisers, name)
			}
		}
	}
	if asJSON {
		return printJSON(cfg, b)
	}
	for _, name := range sortedAdviserNames(b.Advisers) {
		fmt.Fprintf(cfg.Out, "%s\t%s\tbound to %s\n", name, b.Advisers[name].Model, b.ID)
	}
	return nil
}

func sortedAdviserNames[V any](values map[string]V) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

func runAdviserBind(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("advisers bind")
	task, workflow := flags.String("task", "", ""), flags.String("workflow", "", "")
	refresh, asJSON := flags.Bool("refresh", false, ""), flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash || !taskName.MatchString(*task) || (*workflow != "full" && *workflow != "small") {
		return usagef("usage: kitsu advisers bind --task <task> --workflow full|small [--refresh] [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	settings, err := advisers.Load(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	b, err := advisers.Bind(s.repo.Root, *task, *workflow, settings, *refresh)
	if err != nil {
		return failf("%v", err)
	}
	if *asJSON {
		return printJSON(cfg, b)
	}
	fmt.Fprintf(cfg.Out, "%s: bound adviser configuration %s (%s workflow)\n", b.Task, b.ID, b.Workflow)
	return nil
}

func runAdviserResolve(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("advisers resolve")
	task, disposition := flags.String("task", "", ""), flags.String("disposition", "", "")
	reason, choice := flags.String("reason", "", ""), flags.String("choice", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 1 || p.dash || !taskName.MatchString(*task) || *disposition == "" || strings.TrimSpace(*reason) == "" {
		return usagef("usage: kitsu advisers resolve --task <task> <consultation-id> --disposition accept|reject|defer --reason <text> [--choice <id>] [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	d, err := advisers.Resolve(s.repo.Root, *task, p.pos[0], *disposition, *reason, *choice)
	if err != nil {
		return failf("%v", err)
	}
	if *asJSON {
		return printJSON(cfg, d)
	}
	fmt.Fprintf(cfg.Out, "%s: %s; %s\n", d.Consultation, d.Disposition, d.Reason)
	return nil
}

func runAdviserStatus(ctx context.Context, cfg Config, action string, args []string) error {
	flags := newFlags("advisers " + action)
	task, checkpoint := flags.String("task", "", ""), flags.String("checkpoint", "", "")
	asJSON := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash || !taskName.MatchString(*task) {
		return usagef("usage: kitsu advisers %s --task <task> [--checkpoint <id>] [--json]", action)
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	content, historical, err := s.adviserContent(ctx, *task, action == "status")
	if err != nil {
		return failf("%v", err)
	}
	report, err := advisers.Evaluate(s.repo.Root, *task, content, historical, *checkpoint)
	if err != nil {
		return failf("%v", err)
	}
	if *asJSON {
		if err := printJSON(cfg, report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(cfg.Out, "%s: adviser checkpoints ready: %t\n", report.Task, report.Ready)
		for _, r := range report.Requirements {
			fmt.Fprintf(cfg.Out, "  %s (%s, %s, %s): %s\n", r.Checkpoint, r.Stage, r.Role, r.Adviser, r.State)
		}
		for _, problem := range report.Problems {
			fmt.Fprintln(cfg.Out, "  "+problem)
		}
	}
	if action == "check" && !report.Ready {
		return exit(ExitNotVerified)
	}
	return nil
}

// adviserContent gathers current artifacts; historical status also accepts
// the committed baseline of an unchanged record. Explicit checks use current work.
func (s *session) adviserContent(ctx context.Context, task string, history bool) (advisers.Content, string, error) {
	var content advisers.Content
	tree, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return content, "", err
	}
	codeOf := func(tree string) (string, error) {
		code := change.NewCode()
		err := s.repo.Entries(ctx, tree, code.Add)
		return code.ID(), err
	}
	content.Code, err = codeOf(tree)
	if err != nil {
		return content, "", err
	}
	f, err := change.Read(s.repo.Root, task)
	if err == nil {
		r, err := change.Status(f, change.Git{Code: content.Code})
		if err != nil {
			return content, "", err
		}
		content = r.Content
	} else if !errors.Is(err, change.ErrNoFolder) {
		return content, "", err
	}
	if !history {
		return content, "", nil
	}
	rel, err := advisers.Location(s.repo.Root, task)
	if err != nil {
		return content, "", err
	}
	if f != nil && f.Task != "" {
		rel = f.Rel
	}
	head, found, err := s.repo.TreeOf(ctx, "HEAD")
	if err != nil || !found {
		return content, "", err
	}
	here, _, err := s.repo.Object(ctx, tree, rel)
	if err != nil {
		return content, "", err
	}
	there, found, err := s.repo.Object(ctx, head, rel)
	if err != nil || !found || here != there {
		return content, "", err
	}
	commit, found, err := s.repo.LastCommit(ctx, rel)
	if err != nil || !found {
		return content, "", err
	}
	historical, err := codeOf(commit)
	return content, historical, err
}

func adviserRequest(cfg Config, file string) (advisers.Request, error) {
	if file == "-" {
		return advisers.ParseRequest(cfg.In)
	}
	f, err := os.Open(filepath.Clean(file))
	if err != nil {
		return advisers.Request{}, err
	}
	defer func() { _ = f.Close() }()
	return advisers.ParseRequest(f)
}
