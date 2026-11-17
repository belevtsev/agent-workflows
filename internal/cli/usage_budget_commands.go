package cli

import "context"

func usageBudgetCommands() []uxCommand {
	unit := uxOption("unit", "tokens|seconds", "unit of the estimate or declared limit", "tokens", "seconds")
	json := uxOption("json", "", "print machine-readable JSON without prompts or styling")
	commands := []uxCommand{}
	for _, action := range []string{"set", "show", "reset"} {
		command := uxCommand{path: "usage budget " + action, args: "<task>", summary: "Inspect optional task estimates and declared limits separately from actuals", flags: []uxFlag{json}, run: func(ctx context.Context, cfg Config, args []string) error {
			return runUsageBudget(ctx, cfg, append([]string{action}, args...))
		}}
		if action != "show" {
			command.flags = append(command.flags, unit)
		}
		if action == "set" {
			command.summary = "Record a sourced estimate range or declared limit; authorizes no execution"
			command.flags = append(command.flags, uxOption("min", "number", "estimated lower bound; requires --max and --basis"), uxOption("max", "number", "estimated upper bound; requires --min and --basis"), uxOption("basis", "text", "basis for the estimate range"), uxOption("assumption", "text", "estimate assumption; repeatable"), uxOption("limit", "number", "declared nonnegative limit; requires --source; no runtime enforcement"), uxOption("source", "text", "explicit source of the declared limit; not proof of approval"), uxOption("workflow", "full|small", "match the existing full task or small budget location", "full", "small"))
			command.examples = []string{"kitsu usage budget set fix-login --unit tokens --min 100000 --max 300000 --basis 'Earlier captured tasks'", "kitsu usage budget set fix-login --unit tokens --limit 400000 --source 'Owner instruction in task request'"}
		} else if action == "reset" {
			command.summary = "Remove only one unit's optional estimate and declared limit"
			command.examples = []string{"kitsu usage budget reset fix-login --unit tokens"}
		}
		commands = append(commands, command)
	}
	return commands
}
