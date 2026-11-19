package cli

import (
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/shellwords"
)

func uxChangesPolicy(line string) string {
	rules := []shellwords.CommandRule{
		{Path: []string{"roles", "set"}, Reason: "changes role models or reasoning, or skill selections"},
		{Path: []string{"roles", "reset"}, Reason: "changes role models or reasoning, or skill selections"},
		{Path: []string{"add"}, Reason: "changes repository check configuration"},
	}
	for _, c := range uxCommands() {
		if c.guard == "" {
			continue
		}
		reason := "changes workflow or CLI configuration"
		if strings.HasPrefix(c.path, "role ") || strings.HasPrefix(c.path, "skill ") {
			reason = "changes role models or reasoning, or skill selections"
		} else if c.guard == "gate" || c.guard == "observe" {
			reason = "turns off or observes the supporting Git gate"
		}
		rules = append(rules, shellwords.CommandRule{Path: strings.Fields(c.path), Reason: reason, ObserveOnly: c.guard == "observe"})
	}
	return shellwords.ChangesCommands(line, rules)
}

// A direct command can configure this checkout using the validated role CLI.
// Keep the policy guard for composed shell calls and legacy/global settings.
func uxRepositoryRoleCommand(root, line string) bool {
	if strings.ContainsAny(strings.TrimSpace(line), "\\$`;&|<>()#*?[]{}\r\n") {
		return false
	}
	calls, ok := shellwords.Split(line)
	if !ok || len(calls) != 1 || len(calls[0]) < 3 {
		return false
	}
	args := calls[0]
	if (args[0] != "kitsu" && args[0] != "kitsu.exe") || args[1] != "role" {
		return false
	}
	if args[2] == "init" {
		return true // role init only creates a new version 2 file in this worktree.
	}
	if args[2] != "set" {
		return false
	}
	change, _, err := parseRoleSelection(args[3:], false)
	if err != nil || change.personal || len(change.roots) != 0 || !roles.Known(change.role) {
		return false
	}
	config, err := roles.Load(root)
	return err == nil && config.Version == roles.WorkflowConfigVersion
}
