package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/decl"
	"github.com/belevtsev/kitsu/v3/internal/taskbudget"
)

const usageBudgetNotice = "Estimates and limits are user-authored metadata. A declared source is not proof of authorization; these records do not authorize paid execution or enforce hard runtime caps."

type usageBudgetOptions struct {
	action, task, unit, workflow string
	update                       taskbudget.Entry
	json                         bool
}

type usageBudgetDeclarations struct {
	min, max, limit int64
	basis, source   string
	assumptions     stringList
}

func parseUsageBudget(args []string) (usageBudgetOptions, error) {
	o := usageBudgetOptions{}
	if len(args) == 0 || !validUsageBudgetAction(args[0]) {
		return o, usagef("usage: kitsu usage budget set|show|reset <task> [options]")
	}
	o.action = args[0]
	args = args[1:]
	flags := newFlags("usage budget " + o.action)
	flags.BoolVar(&o.json, "json", false, "")
	var declarations usageBudgetDeclarations
	if o.action != "show" {
		flags.StringVar(&o.unit, "unit", "", "")
	}
	if o.action == "set" {
		flags.StringVar(&o.workflow, "workflow", "", "")
		flags.Int64Var(&declarations.min, "min", 0, "")
		flags.Int64Var(&declarations.max, "max", 0, "")
		flags.StringVar(&declarations.basis, "basis", "", "")
		flags.Var(&declarations.assumptions, "assumption", "")
		flags.Int64Var(&declarations.limit, "limit", 0, "")
		flags.StringVar(&declarations.source, "source", "", "")
	}
	p, err := parseArgs(flags, args)
	if err != nil {
		return o, err
	}
	if p.dash || len(p.pos) != 1 || !taskName.MatchString(p.pos[0]) {
		return o, usagef("usage: kitsu usage budget %s <task> [options]", o.action)
	}
	o.task = p.pos[0]
	if o.action != "show" && o.unit != taskbudget.Tokens && o.unit != taskbudget.Seconds {
		return o, usagef("--unit tokens|seconds is required")
	}
	if o.action != "set" {
		return o, nil
	}
	if o.workflow != "" && o.workflow != "full" && o.workflow != "small" {
		return o, usagef("--workflow must be full or small")
	}
	o.update.Estimate, err = declarations.estimate(args)
	if err != nil {
		return o, err
	}
	o.update.Limit, err = declarations.declaredLimit(args)
	if err != nil {
		return o, err
	}
	if o.update.Estimate == nil && o.update.Limit == nil {
		return o, usagef("set requires an estimate (--min --max --basis), a declared limit (--limit --source), or both")
	}
	return o, nil
}

func validUsageBudgetAction(action string) bool {
	return action == "set" || action == "show" || action == "reset"
}

func (d usageBudgetDeclarations) estimate(args []string) (*taskbudget.Estimate, error) {
	hasMin, hasMax, hasBasis := uxFlagPresent(args, "min"), uxFlagPresent(args, "max"), uxFlagPresent(args, "basis")
	if !hasMin && !hasMax && !hasBasis && len(d.assumptions) == 0 {
		return nil, nil
	}
	if !hasMin || !hasMax || !hasBasis || d.min < 0 || d.max < d.min || strings.TrimSpace(d.basis) == "" {
		return nil, usagef("estimates require --min N --max N --basis text together, with 0 <= min <= max")
	}
	for _, a := range d.assumptions {
		if strings.TrimSpace(a) == "" {
			return nil, usagef("--assumption must not be empty")
		}
	}
	return &taskbudget.Estimate{Min: d.min, Max: d.max, Basis: d.basis, Assumptions: d.assumptions}, nil
}

func (d usageBudgetDeclarations) declaredLimit(args []string) (*taskbudget.Limit, error) {
	hasLimit, hasSource := uxFlagPresent(args, "limit"), uxFlagPresent(args, "source")
	if !hasLimit && !hasSource {
		return nil, nil
	}
	if !hasLimit || !hasSource || d.limit < 0 || strings.TrimSpace(d.source) == "" {
		return nil, usagef("limits require --limit N --source text together with a nonnegative value; a source does not authorize execution")
	}
	return &taskbudget.Limit{N: d.limit, Source: d.source}, nil
}

type usageBudgetView struct {
	Task          string                           `json:"task"`
	Configuration *taskbudget.Record               `json:"configuration"`
	Comparison    map[string]taskbudget.Comparison `json:"comparison"`
	Notice        string                           `json:"notice"`
}

func runUsageBudget(ctx context.Context, cfg Config, args []string) error {
	o, err := parseUsageBudget(args)
	if err != nil {
		return err
	}
	s, err := open(ctx, cfg, false)
	if err != nil {
		return err
	}
	var record *taskbudget.Record
	switch o.action {
	case "set":
		record, err = taskbudget.Set(s.repo.Root, o.task, o.workflow, o.unit, o.update)
	case "reset":
		record, err = taskbudget.Reset(s.repo.Root, o.task, o.unit)
	case "show":
		record, err = taskbudget.Load(s.repo.Root, o.task)
	}
	if err != nil {
		return failf("%v", err)
	}
	// Reset may remove the sole small-task record. Its successful mutation is
	// still displayable; show requires a structurally existing task.
	if o.action == "show" {
		exists, err := taskbudget.TaskExists(s.repo.Root, o.task)
		if err != nil {
			return failf("%v", err)
		}
		if !exists {
			return failf("task %q has no full directory or small budget history", o.task)
		}
	}
	actuals, err := usageActualForTask(ctx, s, o.task)
	if err != nil {
		return failf("%v", err)
	}
	view := usageBudgetView{Task: o.task, Configuration: record, Comparison: map[string]taskbudget.Comparison{}, Notice: usageBudgetNotice}
	for _, unit := range []string{taskbudget.Tokens, taskbudget.Seconds} {
		var entry taskbudget.Entry
		if record != nil {
			entry = record.Entries[unit]
		}
		view.Comparison[unit] = taskbudget.Compare(entry, actuals[unit])
	}
	if o.json {
		return printJSON(cfg, view)
	}
	printUsageBudget(cfg.Out, view)
	return nil
}

func printUsageBudget(out io.Writer, view usageBudgetView) {
	fmt.Fprintln(out, decl.Plain(view.Task)+": usage budget")
	for _, unit := range []string{taskbudget.Tokens, taskbudget.Seconds} {
		comparison := view.Comparison[unit]
		fmt.Fprintf(out, "%s: actual ", unit)
		switch {
		case !comparison.Actual.Known:
			fmt.Fprint(out, "unknown")
		case !comparison.Actual.Complete:
			fmt.Fprintf(out, "at least %d (incomplete)", comparison.Actual.N)
		default:
			fmt.Fprint(out, comparison.Actual.N)
		}
		if comparison.Actual.Basis != "" {
			fmt.Fprintf(out, "; basis: %s", decl.Plain(comparison.Actual.Basis))
		}
		fmt.Fprintln(out)
		var entry taskbudget.Entry
		if view.Configuration != nil {
			entry = view.Configuration.Entries[unit]
		}
		if entry.Estimate != nil {
			fmt.Fprintf(out, "  estimate: %d..%d; basis: %s; comparison: %s\n", entry.Estimate.Min, entry.Estimate.Max, decl.Plain(entry.Estimate.Basis), comparison.EstimateStatus)
			for _, a := range entry.Estimate.Assumptions {
				fmt.Fprintf(out, "  assumption: %s\n", decl.Plain(a))
			}
		} else {
			fmt.Fprintln(out, "  estimate: absent")
		}
		if entry.Limit != nil {
			fmt.Fprintf(out, "  declared limit: %d; source: %s; comparison: %s\n", entry.Limit.N, decl.Plain(entry.Limit.Source), comparison.LimitStatus)
		} else {
			fmt.Fprintln(out, "  declared limit: absent")
		}
	}
	fmt.Fprintln(out, usageBudgetNotice)
}
