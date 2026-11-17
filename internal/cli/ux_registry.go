package cli

import (
	"context"
	"strings"
)

func uxCommands() []uxCommand {
	host := uxOption("host", "codex|claude", "override the saved CLI host preference", "codex", "claude")
	json := uxOption("json", "", "print machine-readable JSON without prompts or styling")
	global := uxOption("global", "", "edit personal settings instead of repository settings; role settings permit skills only")
	root := uxOption("root", "directory", "include an original skill directory or collection; repeatable")
	task := uxOption("task", "task", "task whose saved records or adviser access to use")
	workflow := uxOption("workflow", "full|small", "select full or small task history", "full", "small")
	role := uxOption("role", "role", "role requesting advice or receiving skill selections")
	accessible := uxOption("accessible", "", "use descriptive accessible prompts instead of the full-screen form")
	list := []uxCommand{
		{path: "agent check", summary: "Check local CLI, plugin, task skill and hooks without executing the host", flags: []uxFlag{host, json}, run: runUXAgentCheck, host: true, requiredHost: true},
		{path: "task start", args: "<request>", summary: "Open an interactive agent host with the Kitsu task skill", flags: []uxFlag{host, uxOption("print", "", "print diagnostics and the handoff prompt without opening the host"), json}, operands: [][2]string{{"request", "one nonempty request; quote the complete request"}}, examples: []string{"kitsu task start \"Add a login screen\" --host codex", "kitsu task start \"Fix the failing test\" --host claude --print"}, run: runUXTaskStart, host: true, requiredHost: true},
		{path: "task list", summary: "List full tasks and saved small-task histories", flags: []uxFlag{json}, run: runUXTaskList},
		{path: "task show", args: "<task>", summary: "Show a full task's stage or a small task's saved history", flags: []uxFlag{json}, run: runUXTaskShow},
		{path: "task check", args: "[<task>]", summary: "Check saved task records offline", run: uxLegacy("change", "check")},
		{path: "role list", summary: "List role models, reasoning, skills and adviser access", flags: []uxFlag{host, task, json}, run: uxLegacy("roles"), host: true},
		{path: "role show", args: "<role>", summary: "Show one role's effective settings and write limits", flags: []uxFlag{host, task, json}, run: runUXRoleShow, host: true},
		{path: "role set", args: "<role>", summary: "Change only the supplied role settings", flags: []uxFlag{host, uxOption("model", "model", "set the exact host model id; availability depends on your account"), uxOption("reasoning", "effort", "set reasoning effort; omitted fields stay unchanged"), uxOption("skills", "references", "replace skill selections; an empty value suppresses inheritance"), root, global, json}, run: uxLegacy("roles", "set"), host: true, requiredHost: true, guard: "policy"},
		{path: "role edit", args: "<role>", summary: "Interactively edit a role with a preview before saving", flags: []uxFlag{host, global, root, accessible}, run: runUXRoleEdit, guard: "policy"},
		{path: "role reset", args: "<role>", summary: "Remove role overrides in this scope; restore model defaults and inherited skills", flags: []uxFlag{host, global, json}, run: uxLegacy("roles", "reset"), host: true, requiredHost: true, guard: "policy"},
		{path: "role check", args: "<role> --from <tree>", summary: "Check that a role stayed within its write limits", flags: []uxFlag{uxOption("from", "tree", "required starting Git tree"), task, uxOption("finalize", "", "permit spec-writer finalization for the named task")}, run: uxLegacy("roles", "check")},
		{path: "role text", args: "<role>", summary: "Print a role's instructions, including coordinator", run: uxLegacy("roles", "text")},
		{path: "skill list", summary: "Discover original host skills without executing them", flags: []uxFlag{host, root, json}, run: uxLegacy("skills", "list"), host: true, requiredHost: true},
		{path: "skill show", args: "<skill>", summary: "Inspect skill metadata, including unavailable sources", flags: []uxFlag{host, root, json}, run: runUXSkillShow, host: true, requiredHost: true},
		{path: "skill resolve", args: "<role>", summary: "Resolve a role's current original sources; loading remains unverified", flags: []uxFlag{host, root, task, workflow, json}, run: uxLegacy("skills", "resolve"), host: true, requiredHost: true},
		{path: "adviser list", summary: "List configured experimental advisers or a task's saved binding", flags: []uxFlag{role, task, json}, run: uxLegacy("advisers", "list")},
		{path: "adviser show", args: "<adviser>", summary: "Inspect one configured or task-bound experimental adviser", flags: []uxFlag{task, json}, run: runUXAdviserShow},
		{path: "adviser providers", summary: "List experimental provider capabilities and required credential variables offline", flags: []uxFlag{json}, run: uxLegacy("advisers", "providers")},
		{path: "adviser bind", args: "<task> --workflow <workflow>", summary: "Save the explicitly enabled adviser policy for a task", flags: []uxFlag{workflow, uxOption("refresh", "", "explicitly adopt policy changes or promote a small history"), json}, run: runUXAdviserBind},
		{path: "adviser ask", args: "<adviser> --task <task> --role <role> --stage <stage> --request <file>", summary: "Consult an explicitly enabled experimental adviser and record its answer", flags: []uxFlag{task, role, uxOption("stage", "stage", "required workflow stage of this request", "lookup", "spec", "design", "tests", "code", "review", "commit"), uxOption("request", "file|-", "required version 1 or 2 request JSON; use - for standard input"), uxOption("checkpoint", "id", "required checkpoint this request addresses"), json}, run: runUXAdviserAsk},
		{path: "adviser resolve", args: "<id> --task <task> --disposition <disposition> --reason <text>", summary: "Legacy disposition spelling; prefer adviser accept, reject or defer", flags: []uxFlag{task, uxOption("disposition", "accept|reject|defer", "required disposition", "accept", "reject", "defer"), uxOption("reason", "text", "required reason"), uxOption("choice", "id", "selected choice from a decision request"), json}, run: uxLegacy("advisers", "resolve"), hidden: true},
		{path: "check list", summary: "List this repository's declared checks", flags: []uxFlag{json}, run: runUXCheckList},
		{path: "check add", args: "<id> -- <program> [<argument>...]", summary: "Declare a repository check without invoking a shell", flags: []uxFlag{uxOption("paths", "pattern", "dependent paths; repeatable; omitted means all content"), uxOption("timeout", "duration", "check timeout from 1s to 6h; default 10m")}, run: uxLegacy("add"), guard: "policy"},
		{path: "check run", args: "[<id>...]", summary: "Run declared checks that lack passing evidence for current content", flags: []uxFlag{uxOption("force", "", "run even when passing evidence exists"), uxOption("base", "rev", "reuse evidence only when checked content matches this revision")}, run: uxLegacy("run")},
		{path: "check stats", summary: "Report recorded check runs and gate decisions", flags: []uxFlag{uxOption("since", "period", "reporting period such as 90m, 7d or 2w; default 30d"), json}, run: uxLegacy("stats")},
		{path: "gate init", summary: "Declare checks when needed and install Git hooks", flags: []uxFlag{uxOption("observe", "", "record decisions while allowing commits and pushes")}, run: uxLegacy("init"), guard: "observe"},
		{path: "gate status", summary: "Report whether current content has evidence to permit a commit", flags: []uxFlag{json}, run: uxLegacy("status")},
		{path: "gate uninstall", summary: "Remove Kitsu's Git hooks and local records; keep configuration", run: uxLegacy("uninstall"), guard: "gate"},
		{path: "usage show", summary: "Report task tokens and workflow overhead; unknown usage stays unknown", flags: []uxFlag{task, json}, run: uxLegacy("tokens")},
		{path: "usage record", summary: "Record a host token notice from standard input", flags: []uxFlag{uxOption("host", "codex|claude", "required notice format; explicit host avoids protocol guessing", "codex", "claude")}, run: runUXUsageRecord},
	}
	list = append(list, compactCommands()...)
	list = append(list, workerCommands()...)
	for i := range list {
		if list[i].path == "check run" {
			list[i].flags = append(list[i].flags, compactCheckFlags()...)
		}
		if list[i].path == "task start" {
			list[i].flags = append(list[i].flags, uxOption("compact", "", "opt in to Go context collection, compact briefs and check results"))
		}
	}
	return uxExtraCommands(list, []uxFlag{host, json, global, root, task, role, accessible})
}

func uxExtraCommands(list []uxCommand, common []uxFlag) []uxCommand {
	host, json, global, root := common[0], common[1], common[2], common[3]
	task, role, accessible := common[4], common[5], common[6]
	for _, action := range []string{"attach", "detach", "clear", "reset"} {
		args, summary := "<skill>", "Add a skill without replacing existing selections"
		switch action {
		case "detach":
			summary = "Remove a selected skill, even when its original source is gone"
		case "clear":
			args, summary = "", "Save an empty skill list, suppressing inherited personal choices"
		case "reset":
			args, summary = "", "Remove only the skill override and restore inheritance"
		}
		list = append(list, uxCommand{path: "skill " + action, args: args + " --role <role>", summary: summary, flags: []uxFlag{host, role, root, global, json}, run: func(ctx context.Context, cfg Config, args []string) error {
			return runUXSkillEdit(ctx, cfg, action, args)
		}, host: true, requiredHost: true, guard: "policy"})
	}
	for _, action := range []string{"status", "check"} {
		list = append(list, uxCommand{path: "adviser " + action, args: "<task>", summary: "Inspect " + action + " readiness of saved experimental adviser checkpoints", flags: []uxFlag{uxOption("checkpoint", "id", "consider only this checkpoint"), json}, run: func(ctx context.Context, cfg Config, args []string) error {
			return runUXAdviserTask(ctx, cfg, action, args)
		}})
	}
	for _, action := range []string{"accept", "reject", "defer"} {
		list = append(list, uxCommand{path: "adviser " + action, args: "<id> --task <task> --reason <text>", summary: "Record an explicit " + action + " disposition without replacing reviews or approvals", flags: []uxFlag{task, uxOption("reason", "text", "required explanation; keep the original answer"), uxOption("choice", "id", "selected choice from a decision request"), json}, run: func(ctx context.Context, cfg Config, args []string) error {
			return runUXAdviserDisposition(ctx, cfg, action, args)
		}})
	}
	for _, action := range []string{"show", "get", "set", "reset", "edit"} {
		args := map[string]string{"get": "<key>", "set": "<key> <value>", "reset": "<key>"}[action]
		flags := []uxFlag{json}
		if action == "set" || action == "reset" || action == "edit" {
			flags = append(flags, uxOption("global", "", "edit personal CLI preferences instead of this private checkout"))
		}
		guard := ""
		if action == "edit" {
			flags, guard = []uxFlag{uxOption("global", "", "edit personal CLI preferences instead of this private checkout"), accessible}, "policy"
		} else if action == "set" || action == "reset" {
			guard = "policy"
		}
		list = append(list, uxCommand{path: "config " + action, args: args, summary: "Manage CLI host preferences: " + action, flags: flags, run: func(ctx context.Context, cfg Config, args []string) error {
			return runUXConfig(ctx, cfg, append([]string{action}, args...))
		}, guard: guard})
	}
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		list = append(list, uxCommand{path: "completion " + shell, summary: "Print a " + shell + " completion script for this CLI", run: func(ctx context.Context, cfg Config, args []string) error {
			return runUXCompletion(ctx, cfg, append([]string{shell}, args...))
		}})
	}
	for i := range list {
		c := &list[i]
		if len(c.examples) == 0 {
			c.examples = []string{"kitsu " + c.path + uxExampleArgs(*c)}
		}
	}
	return list
}

func uxLegacy(name string, prefix ...string) func(context.Context, Config, []string) error {
	return func(ctx context.Context, cfg Config, args []string) error {
		for _, c := range commands() {
			if c.name == name {
				return c.run(ctx, cfg, append(append([]string{}, prefix...), args...))
			}
		}
		return usagef("unknown legacy command %s", name)
	}
}

func uxExampleArgs(c uxCommand) string {
	switch c.path {
	case "role set":
		return " architect --host codex --model gpt-6.1-sol --reasoning max"
	case "role check":
		return " reviewer --from HEAD"
	case "check add":
		return " test -- go test ./..."
	case "config set":
		return " host codex --global"
	case "config get", "config reset":
		return " host"
	case "adviser ask":
		return " design --task add-login --role architect --stage design --request request.json"
	case "adviser bind":
		return " add-login --workflow full"
	case "adviser accept", "adviser reject", "adviser defer":
		return " <consultation-id> --task add-login --reason 'Reviewed the answer'"
	case "adviser resolve":
		return " <consultation-id> --task add-login --disposition accept --reason 'Reviewed the answer'"
	case "usage record":
		return " --host codex < notice.json"
	}
	args := ""
	if strings.Contains(c.args, "<role>") && !strings.Contains(c.args, "--role") {
		args = " developer"
	} else if strings.Contains(c.args, "<skill>") {
		args = " project-review"
	} else if strings.Contains(c.args, "<task>") {
		args = " add-login"
	} else if strings.Contains(c.args, "<adviser>") {
		args = " design"
	}
	if strings.Contains(c.args, "--role") {
		args += " --role developer"
	}
	if c.requiredHost {
		args += " --host codex"
	}
	return args
}
