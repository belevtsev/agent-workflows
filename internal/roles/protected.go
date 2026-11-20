package roles

import "strings"

// HostConfigurationPath identifies repository files that can change how a
// coding host starts or what it loads. The runner uses an operator-owned profile.
func HostConfigurationPath(name string) bool {
	for component := range strings.SplitSeq(strings.ToLower(name), "/") {
		switch strings.TrimRight(component, " .") {
		case ".claude", ".codex", ".agents", ".mcp.json", "agents.md", "claude.md", "claude.local.md":
			return true
		}
	}
	return false
}

func ProtectedWorkflowPath(name string) bool {
	switch strings.ToLower(name) {
	case "kitsu.toml", "kitsu-roles.toml", "kitsu-advisers.toml":
		return true
	}
	if HostConfigurationPath(name) {
		return true
	}
	for component := range strings.SplitSeq(strings.ToLower(name), "/") {
		switch strings.TrimRight(component, " .") {
		case ".git", ".gitignore", ".gitattributes", ".gitmodules", ".lfsconfig":
			return true
		}
	}
	return false
}

// ObservedModelMatches requires an exact model ID or a recognized Claude family.
func ObservedModelMatches(host, requested, observed string) bool {
	if observed == "" {
		return false
	}
	if requested == observed || requested == "" {
		return true
	}
	switch requested {
	case "opus", "sonnet", "haiku":
		return host == "claude" && strings.HasPrefix(observed, "claude-"+requested+"-")
	}
	return false
}
