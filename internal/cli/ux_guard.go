package cli

import (
	"strings"

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
