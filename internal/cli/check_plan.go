package cli

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/verification"
)

func runCheckPlan(ctx context.Context, cfg Config, args []string) error {
	flags := newFlags("check plan")
	from := flags.String("from", "HEAD", "")
	jsonOutput := flags.Bool("json", false, "")
	p, err := parseArgs(flags, args)
	if err != nil {
		return err
	}
	if len(p.pos) != 0 || p.dash || *from == "" {
		return usagef("usage: kitsu check plan [--from <revision>] [--json]")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	tree, err := s.repo.Snapshot(ctx, s.store.Tmp())
	if err != nil {
		return failf("%v", err)
	}
	plan, err := verification.Build(ctx, s.repo, tree, *from)
	if err != nil {
		return failf("%v", err)
	}
	if *jsonOutput {
		return printJSON(cfg, plan)
	}
	fmt.Fprintf(cfg.Out, "Verification plan from %s to %s\nAll %d declared checks remain in the check gate. No checks were executed.\n", plan.BaseTree, plan.SourceTree, len(plan.Checks))
	w := tabwriter.NewWriter(cfg.Out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CHECK\tPRIORITY\tREQUIRED\tREASON / EXPECTED BEHAVIOR")
	for _, check := range plan.Checks {
		text := check.Reason
		if check.Expect != "" {
			text += "; expect: " + check.Expect
		}
		fmt.Fprintf(w, "%s\t%t\t%t\t%s\n", check.ID, check.Priority, check.Required, decl.Plain(text))
		if len(check.MatchedPaths) > 0 {
			fmt.Fprintf(w, "\t\t\tmatched: %s\n", planPathSample(check.MatchedPaths))
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(cfg.Out, "Uncovered changed paths (%d): %s\n", len(plan.Uncovered), planPathSample(plan.Uncovered))
	return nil
}

func planPathSample(paths []string) string {
	if len(paths) == 0 {
		return "none"
	}
	const limit = 8
	items := []string{}
	for _, path := range paths[:min(len(paths), limit)] {
		items = append(items, decl.Plain(path))
	}
	text := strings.Join(items, ", ")
	if len(paths) > limit {
		text += fmt.Sprintf(" ... and %d more", len(paths)-limit)
	}
	return text
}
