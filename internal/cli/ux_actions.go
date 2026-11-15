package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/advisers"
)

func uxRequiredOperand(args []string, flagsWithValues []string) (string, []string, error) {
	for i := 0; i < len(args); i++ {
		name, _, assigned := strings.Cut(args[i], "=")
		if args[i] == "--" {
			break
		}
		if strings.HasPrefix(name, "-") {
			if !assigned && slices.Contains(flagsWithValues, strings.TrimLeft(name, "-")) {
				i++
			}
			continue
		}
		return args[i], append(append([]string{}, args[:i]...), args[i+1:]...), nil
	}
	return "", nil, usagef("one primary argument is required")
}

func uxFlagPresent(args []string, name string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		key, _, _ := strings.Cut(arg, "=")
		if key == "--"+name || key == "-"+name {
			return true
		}
	}
	return false
}

func runUXUsageRecord(ctx context.Context, cfg Config, args []string) error {
	if !uxFlagPresent(args, "host") {
		return usagef("--host is required for host token notices; preferences do not select a protocol")
	}
	return runTokensRecord(ctx, cfg, args)
}

func uxTaskOperand(args []string) ([]string, error) {
	task, remaining, err := uxRequiredOperand(args, []string{"workflow", "checkpoint"})
	if err != nil || !taskName.MatchString(task) {
		return nil, usagef("one valid task name is required")
	}
	if uxFlagPresent(remaining, "task") {
		return nil, usagef("supply the task once as a positional argument")
	}
	return append([]string{"--task", task}, remaining...), nil
}

func runUXAdviserBind(ctx context.Context, cfg Config, args []string) error {
	translated, err := uxTaskOperand(args)
	if err != nil {
		return err
	}
	return runAdviserBind(ctx, cfg, translated)
}

func runUXAdviserTask(ctx context.Context, cfg Config, action string, args []string) error {
	translated, err := uxTaskOperand(args)
	if err != nil {
		return err
	}
	return runAdviserStatus(ctx, cfg, action, translated)
}

func runUXAdviserAsk(ctx context.Context, cfg Config, args []string) error {
	name, remaining, err := uxRequiredOperand(args, []string{"task", "role", "stage", "request", "checkpoint"})
	if err != nil {
		return err
	}
	if uxFlagPresent(remaining, "adviser") {
		return usagef("supply the adviser once as a positional argument")
	}
	return runConsult(ctx, cfg, append([]string{"--adviser", name}, remaining...))
}

func runUXAdviserDisposition(ctx context.Context, cfg Config, action string, args []string) error {
	if uxFlagPresent(args, "disposition") {
		return usagef("%s already specifies the disposition; do not supply --disposition", action)
	}
	return uxLegacy("advisers", "resolve")(ctx, cfg, append([]string{"--disposition", action}, args...))
}

func runUXAdviserShow(ctx context.Context, cfg Config, args []string) error {
	p, options, err := uxParseView("adviser show", args, "task")
	if err != nil {
		return err
	}
	if len(p.pos) != 1 {
		return usagef("show requires one adviser name")
	}
	s, err := open(ctx, cfg, true)
	if err != nil {
		return err
	}
	if options.task != "" {
		binding, err := advisers.LoadBinding(s.repo.Root, options.task)
		if err != nil {
			return failf("%v", err)
		}
		identity, ok := binding.Advisers[p.pos[0]]
		if !ok {
			return failf("adviser %q is not bound to this task", p.pos[0])
		}
		if options.json {
			return printJSON(cfg, identity)
		}
		fmt.Fprintf(cfg.Out, "%s: provider %s, model %s\nBinding: %s (%s)\n", p.pos[0], adviserProviderName(identity.Provider), identity.Model, binding.ID, options.task)
		return nil
	}
	settings, err := advisers.Load(s.repo.Root)
	if err != nil {
		return failf("%v", err)
	}
	definition, ok := settings.Advisers[p.pos[0]]
	if !ok {
		return failf("adviser %q is not configured", p.pos[0])
	}
	if options.json {
		settings.Advisers = map[string]advisers.Definition{p.pos[0]: definition}
		return printConfiguredAdvisersJSON(cfg, settings)
	}
	fmt.Fprintf(cfg.Out, "%s: provider %s, model %s\nKinds: %s\nSource: %s\n", p.pos[0], adviserProviderName(definition.Provider), definition.Model, strings.Join(advisers.Kinds(definition.Provider), ", "), definition.Source)
	return nil
}
