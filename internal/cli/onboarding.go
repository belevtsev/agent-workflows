package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/onboarding"
)

type onboardingArgs struct {
	host, request        string
	json, print, compact bool
}

func parseOnboardingArgs(args []string, start bool) (onboardingArgs, error) {
	var options onboardingArgs
	flags := newFlags("agent check")
	flags.Func("host", "", func(host string) error {
		if options.host != "" {
			return fmt.Errorf("select exactly one --host")
		}
		if !uxValidHost(host) {
			return fmt.Errorf("host must be codex or claude")
		}
		options.host = host
		return nil
	})
	flags.BoolVar(&options.json, "json", false, "")
	if start {
		flags.BoolVar(&options.print, "print", false, "")
		flags.BoolVar(&options.compact, "compact", false, "")
	}
	p, err := parseArgs(flags, args)
	if err != nil {
		return options, err
	}
	if options.host == "" {
		return options, usagef("select --host codex|claude or save a preference with `kitsu config set host <host>`")
	}
	if !start {
		if len(p.pos) != 0 || p.dash {
			return options, usagef("agent check takes no arguments")
		}
		return options, nil
	}
	requests := append(p.pos, p.rest...)
	if len(requests) != 1 || strings.TrimSpace(requests[0]) == "" || strings.ContainsRune(requests[0], 0) {
		return options, usagef("task start requires one nonempty request; quote the complete request")
	}
	if options.json && options.print {
		return options, usagef("--print and --json are mutually exclusive")
	}
	options.request = requests[0]
	return options, nil
}

func inspectOnboarding(ctx context.Context, cfg Config, host string) (onboarding.Report, error) {
	dir, err := os.Getwd()
	if err != nil {
		return onboarding.Report{}, failf("check coding-agent setup: %v", err)
	}
	report, err := onboarding.Inspect(ctx, onboarding.Options{Host: host, Dir: dir, Version: cfg.Version})
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return report, exit(130)
		}
		return report, failf("check coding-agent setup: %v", err)
	}
	return report, nil
}

func runUXAgentCheck(ctx context.Context, cfg Config, args []string) error {
	options, err := parseOnboardingArgs(args, false)
	if err != nil {
		return err
	}
	report, err := inspectOnboarding(ctx, cfg, options.host)
	if err != nil {
		return err
	}
	if options.json {
		if err := printJSON(cfg, report); err != nil {
			return err
		}
	} else {
		printOnboarding(cfg.Out, report)
		fmt.Fprintf(cfg.Out, "\nStart: kitsu task start \"Describe the change\" --host %s\n", report.Host)
	}
	if !report.Ready {
		return exit(ExitNotVerified)
	}
	return nil
}

func printOnboarding(out io.Writer, report onboarding.Report) {
	readiness := "required local setup needs attention"
	if report.Ready {
		readiness = "no known local blockers"
	}
	fmt.Fprintf(out, "Host: %s\nLocal setup: %s\n", report.Host, readiness)
	for _, check := range report.Checks {
		fmt.Fprintf(out, "\n[%s] %s: %s\n", check.Status, check.ID, check.Summary)
		if check.Detail != "" {
			fmt.Fprintf(out, "  %s\n", check.Detail)
		}
		for _, action := range check.Actions {
			fmt.Fprintf(out, "  Next: %s\n", action)
		}
	}
	fmt.Fprintln(out, "\nLocal checks do not verify host authentication, model access, hook trust or runtime execution.")
}

type taskHandoff struct {
	onboarding.Report
	Prompt string   `json:"prompt"`
	Argv   []string `json:"argv"`
}

func prepareTaskHandoff(report onboarding.Report, request string) taskHandoff {
	prompt := "Use the task skill from the Kitsu plugin to handle this request. Keep this host session as the workflow orchestrator and follow the skill's workflow.\n\nRequest:\n" + request
	if report.Host == "claude" {
		prompt = "/kitsu:task " + request
	}
	program := report.HostExecutable
	if program == "" {
		program = report.Host
	}
	return taskHandoff{Report: report, Prompt: prompt, Argv: []string{program, prompt}}
}

func runUXTaskStart(ctx context.Context, cfg Config, args []string) error {
	options, err := parseOnboardingArgs(args, true)
	if err != nil {
		return err
	}
	report, err := inspectOnboarding(ctx, cfg, options.host)
	if err != nil {
		return err
	}
	handoff := prepareTaskHandoff(report, options.request)
	if options.compact {
		handoff.Prompt += "\n\nKitsu compact mode (selective opt-in): read the task skill's references/compact.md before starting roles. Prefer native host reads for short whole files and scoped native search with small output. Use context read/search/collect for broad or noisy retrieval and check run --compact for noisy declared checks; expand retained logs when needed. Carry this selective policy into every fresh worker brief. Keep original full role instructions and selected skills uncompressed; preserve permissions, independent reviews and required evidence. Context artifacts cache local rendered text, not provider prompt cache; count all requests, responses and expansions when measuring."
		handoff.Argv[1] = handoff.Prompt
	}
	if options.json {
		return printJSON(cfg, handoff)
	}
	if options.print {
		printOnboarding(cfg.Out, report)
		fmt.Fprintf(cfg.Out, "\nHandoff prompt for %s:\n%s\n", report.Host, handoff.Prompt)
		return nil
	}
	printOnboarding(cfg.Err, report)
	if !report.Ready {
		return exit(ExitNotVerified)
	}
	if !onboardingTerminal(cfg) {
		return usagef("task start requires terminal input, stdout and stderr; use --print or --json to prepare a handoff")
	}
	fmt.Fprintf(cfg.Err, "\nOpening %s with the Kitsu task skill. Complete any host permission prompts in that session.\n", report.Host)
	return launchTaskHost(ctx, cfg, handoff.Argv)
}
