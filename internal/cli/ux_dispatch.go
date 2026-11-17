package cli

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

var uxGroups = [][2]string{
	{"agent", "Check coding-agent setup"},
	{"task", "Start a task or inspect task stages and saved records"},
	{"role", "Choose models, reasoning, skills and role instructions"},
	{"skill", "Discover original skills and select them per role"},
	{"adviser", "Inspect and consult optional experimental advisers"},
	{"check", "Declare and run repository checks"},
	{"gate", "Manage the supporting Git gate"},
	{"usage", "Inspect recorded tokens and workflow overhead"},
	{"config", "Manage private CLI host preferences"},
	{"completion", "Generate shell completion scripts"},
}

// Execute routes preferred commands without changing legacy ingress protocols.
// Help is resolved before configuration, repository lookup or execution.
func Execute(ctx context.Context, args []string, cfg Config) error {
	if len(args) == 0 {
		return uxHelp(cfg, nil, false)
	}
	if args[0] == "help" {
		return uxHelp(cfg, args[1:], slices.Contains(args[1:], "--all"))
	}
	if args[0] == "--help" || args[0] == "-h" {
		return uxHelp(cfg, nil, slices.Contains(args[1:], "--all"))
	}
	if args[0] == "__complete" {
		if uxWantsHelp(args[1:]) {
			return uxCompletionHelp(cfg)
		}
		return runUXComplete(ctx, cfg, args[1:])
	}
	preferred := slices.ContainsFunc(uxGroups, func(g [2]string) bool { return g[0] == args[0] })
	if !preferred {
		if uxWantsHelp(args[1:]) && len(args) > 2 && uxLegacyChild(args[0], args[1]) {
			return uxHelp(cfg, args, false)
		}
		if len(args) > 1 && args[1] == "help" {
			return uxHelp(cfg, append([]string{args[0]}, args[2:]...), false)
		}
		err := executeLegacy(ctx, args, cfg)
		if ExitCode(err) == ExitUsage && strings.HasPrefix(err.Error(), "unknown command") {
			return uxUnknown(args[0], "", uxGroupNames())
		}
		return err
	}
	return uxExecutePreferred(ctx, cfg, args)
}

func uxLegacyChild(parent, child string) bool {
	children := map[string][]string{
		"roles": {"set", "reset", "check", "text"}, "skills": {"list", "resolve"},
		"advisers": {"list", "providers", "bind", "resolve", "status", "check"},
		"change":   {"status", "check"}, "tokens": {"record"},
	}
	return slices.Contains(children[parent], child)
}

func uxExecutePreferred(ctx context.Context, cfg Config, args []string) error {
	if len(args) == 1 || args[1] == "--help" || args[1] == "-h" {
		return uxHelp(cfg, args[:1], false)
	}
	if args[1] == "help" {
		return uxHelp(cfg, append([]string{args[0]}, args[2:]...), false)
	}
	path := args[0] + " " + args[1]
	for _, c := range uxCommands() {
		if c.path != path {
			continue
		}
		if uxWantsHelp(args[2:]) {
			return uxPrintLeaf(cfg, c)
		}
		remaining := args[2:]
		var err error
		if c.host {
			remaining, err = uxHostArgs(ctx, remaining, c.requiredHost)
		}
		if err == nil {
			err = c.run(ctx, cfg, remaining)
		}
		if ExitCode(err) == ExitUsage && err.Error() != "" {
			message, _, _ := strings.Cut(err.Error(), "usage:")
			message = strings.TrimRight(strings.TrimSpace(message), ";, ")
			if message == "" {
				message = "invalid or missing arguments"
			}
			return usagef("%s\nusage: %s\nRun `kitsu %s --help` for arguments and examples.", message, uxUsage(c), c.path)
		}
		return err
	}
	return uxUnknown(args[1], args[0], uxChildren(args[0]))
}

func uxWantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func uxAliasPath(args []string) []string {
	out := slices.Clone(args)
	if len(out) == 0 {
		return out
	}
	if replacement, ok := map[string]string{"roles": "role", "skills": "skill", "advisers": "adviser", "change": "task", "tokens": "usage"}[out[0]]; ok {
		out[0] = replacement
	}
	if len(out) > 1 && out[0] == "task" && out[1] == "status" {
		out[1] = "show"
	}
	return out
}

func uxGroupNames() []string {
	names := []string{"help", "version"}
	for _, group := range uxGroups {
		names = append(names, group[0])
	}
	return names
}

func uxChildren(group string) []string {
	var names []string
	for _, c := range uxCommands() {
		if parent, child, ok := strings.Cut(c.path, " "); ok && parent == group && !c.hidden {
			names = append(names, child)
		}
	}
	return names
}

func uxUnknown(word, group string, candidates []string) error {
	closest, score := "", 3
	for _, candidate := range candidates {
		previous := make([]int, len(candidate)+1)
		for i := range previous {
			previous[i] = i
		}
		for i := range len(word) {
			row := []int{i + 1}
			for j := range len(candidate) {
				cost := 0
				if word[i] != candidate[j] {
					cost = 1
				}
				row = append(row, min(row[j]+1, previous[j+1]+1, previous[j]+cost))
			}
			previous = row
		}
		if distance := previous[len(candidate)]; distance < score {
			closest, score = candidate, distance
		}
	}
	hint := ""
	if closest != "" {
		hint = fmt.Sprintf(" Did you mean %q?", strings.TrimSpace(group+" "+closest))
	}
	return usagef("unknown command %q.%s Run `%s` for available commands.", strings.TrimSpace(group+" "+word), hint, strings.TrimSpace("kitsu help "+group))
}
