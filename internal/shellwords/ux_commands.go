package shellwords

import "slices"

// CommandRule classifies a known Kitsu command without executing shell text.
type CommandRule struct {
	Path        []string
	Reason      string
	ObserveOnly bool
}

// ChangesCommands recognizes policy mutations in wrappers and nested shell
// calls using the same paths the CLI dispatches. Help requests remain read-only.
func ChangesCommands(line string, rules []CommandRule) string {
	return judge(line, 3, true, rules)
}

func policyCall(args []string, rules []CommandRule) string {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--help" || arg == "-h" {
			return ""
		}
	}
	for _, rule := range rules {
		if len(args) < len(rule.Path) || !slices.Equal(args[:len(rule.Path)], rule.Path) {
			continue
		}
		if !rule.ObserveOnly || slices.ContainsFunc(args[len(rule.Path):], observes) {
			return rule.Reason
		}
	}
	return ""
}
