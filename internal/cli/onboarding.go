package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/onboarding"
)

type onboardingArgs struct {
	reviewLimits
	host, request, until, resume        string
	json, print, compact, requireReview bool
}

func parseOnboardingArgs(args []string, start bool) (onboardingArgs, error) {
	var options onboardingArgs
	flags := newFlags("agent check")
	bindOnboardingHost(flags, &options.host)
	flags.BoolVar(&options.json, "json", false, "")
	if start {
		options.bindLimitFlags(flags)
		flags.BoolVar(&options.requireReview, "require-review", false, "")
		flags.StringVar(&options.resume, "resume", "", "")
		taskUntil.bindChoice(flags, &options.until)
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
	if err := options.validateLimits(flags); err != nil {
		return options, err
	}
	if options.reserve > 0 && options.limit == 0 {
		return options, usagef("--finish-reserve requires --time-limit")
	}
	if options.resume != "" && options.requireReview {
		return options, usagef("use task extend --require-review to add review to a saved task")
	}
	if options.resume != "" && options.reviewLimits != (reviewLimits{}) {
		return options, usagef("use task extend to change a saved task limit")
	}
	return taskStartOptions(options, args, p)
}

func bindOnboardingHost(flags *flag.FlagSet, value *string) {
	flags.Func("host", "", func(host string) error {
		if *value != "" {
			return errors.New("select exactly one --host")
		}
		if !uxValidHost(host) {
			return errors.New("host must be codex or claude")
		}
		*value = host
		return nil
	})
}

func taskStartOptions(options onboardingArgs, args []string, p parsed) (onboardingArgs, error) {
	requests := append(p.pos, p.rest...)
	if uxFlagPresent(args, "resume") {
		if !taskName.MatchString(options.resume) || len(options.resume) > 100 || len(requests) != 0 || uxFlagPresent(args, "until") {
			return options, usagef("--resume requires one saved task name and no new request or outcome")
		}
		options.request = "Resume with `kitsu task next " + options.resume + "`. Recover existing claims before starting workers."
	} else {
		if len(requests) != 1 || strings.TrimSpace(requests[0]) == "" || strings.ContainsRune(requests[0], 0) {
			return options, usagef("task start requires one nonempty request; quote the complete request")
		}
		options.request = requests[0]
	}
	if options.requireReview && options.until == "plan" {
		return options, usagef("--require-review requires a coding or review outcome")
	}
	if options.json && options.print {
		return options, usagef("--print and --json are mutually exclusive")
	}
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
		if report.Ready {
			fmt.Fprintf(cfg.Out, "\nStart: kitsu task start \"Describe the change\" --host %s\n", report.Host)
		} else {
			fmt.Fprintln(cfg.Out, "\nFollow the setup steps above, then repeat agent check.")
		}
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

// A preview has no pinned task. Give the host the flags that create its limits.
func (o reviewLimits) handoffFlags() string {
	var args []string
	if o.limit > 0 {
		args = append(args, "--time-limit", o.limit.String())
	}
	if o.reviews > 0 {
		args = append(args, "--review-limit", strconv.Itoa(o.reviews))
	}
	if o.reserve > 0 {
		args = append(args, "--finish-reserve", o.reserve.String())
	}
	return strings.Join(args, " ")
}

func (o onboardingArgs) resumeCommand(id, host string) string {
	command := "kitsu task start --resume " + id + " --host " + host
	if o.compact {
		command += " --compact"
	}
	return command
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
	request := options.request
	if options.requireReview {
		request = "Require independent review. Use --require-review when binding the task.\n\n" + request
	}
	if options.until != "" {
		request = "Use the task harness. Requested stopping point: " + options.until + ".\n\n" + request
	}
	if options.reviewLimits != (reviewLimits{}) {
		if options.print || options.json {
			request = "Pass " + options.handoffFlags() + " to kitsu task open.\n\n" + request
		} else {
			request = "Startup applied " + options.handoffFlags() + ". Omit the limit flags when binding the saved task.\n\n" + request
		}
	}
	handoff := prepareTaskHandoff(report, request)
	if options.compact {
		handoff.Prompt += "\n\nKitsu compact mode (selective opt-in): use native reads for short files and compact output for noisy searches and logs. Expand retained evidence when needed. See references/compact.md of the kitsu-team plugin's managed skill for commands. Keep the requested workflow."
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
	id, err := pinHostTask(ctx, cfg, options, report.Repository)
	if err != nil {
		return err
	}
	if id != "" {
		fmt.Fprintf(cfg.Err, "Task %s. Resume with %s\n", id, options.resumeCommand(id, report.Host))
		if options.resume == "" {
			until := options.until
			if until == "" {
				until = "OUTCOME"
			}
			handoff.Prompt += "\n\nFor this new daily task, bind the saved request with kitsu task open " + id + " --entry STAGE --until " + until + " --brief. Choose the stage and outcome from the request and add its authorized scope flags. The original request is already saved."
			handoff.Argv[1] = handoff.Prompt
		}
	}
	fmt.Fprintf(cfg.Err, "\nOpening %s with the Kitsu task skill. Complete any host permission prompts in that session.\n", report.Host)
	return launchTaskHost(ctx, cfg, handoff.Argv)
}
