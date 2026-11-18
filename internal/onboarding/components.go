package onboarding

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"

	"github.com/belevtsev/kitsu/v3/internal/shellwords"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func taskCheck(p plugin, root, host string, s settings) Check {
	check := Check{ID: "task_skill", Status: "fail", Summary: "Installed Kitsu task skill is unavailable", Actions: []string{"Repair or update the installed Kitsu plugin and ensure its task skill is enabled and user-invocable."}}
	if p.root == "" {
		return check
	}
	paths, ok := componentPaths(p.root, p.manifest.Skills, "./skills")
	if !ok {
		check.Detail = "Plugin skill paths are malformed or escape the installation."
		return check
	}
	catalog, err := skills.DiscoverWithRoots(root, host, paths)
	if err != nil {
		check.Detail = "Cannot inspect installed skill metadata."
		return check
	}
	entries := []skills.Entry{}
	for _, entry := range catalog.Skills {
		if entry.Name == "task" && inside(p.root, entry.Path) {
			entries = append(entries, entry)
		}
	}
	if len(entries) != 1 {
		check.Detail = "No single readable task skill was found inside the selected installation."
		return check
	}
	entry := entries[0]
	// The catalog's process-directory policy can differ from Options.Dir and
	// does not include managed settings. Validate the original bytes afresh,
	// then apply only the effective policy inspected for this target directory.
	entry, err = skills.ReadMetadata(entry.Path, host)
	if err != nil {
		check.Detail = "The installed task skill metadata is malformed or unreadable."
		return check
	}
	disabled := false
	for _, rule := range s.rules {
		if rule.Name == entry.Name || samePath(rule.Path, entry.Path) {
			disabled = !*rule.Enabled
		}
	}
	if disabled || !entry.UserInvocable {
		check.Detail = "The installed task skill is disabled, malformed or unavailable for user invocation."
		return check
	}
	body, err := readFile(entry.Path)
	if err != nil || !bytes.Contains(body, []byte("<!-- kitsu-task-harness: 3 -->")) {
		check.Detail = "The installed task skill does not declare harness protocol 3. Update the Kitsu plugin to match this CLI."
		return check
	}
	check.Status, check.Summary = "pass", "Installed Kitsu task skill is usable in configuration"
	check.Detail, check.Actions = p.id+":task at "+entry.Path+"; host discovery has not been executed.", []string{}
	if entry.ManualOnly {
		check.Status, check.Summary = "warn", "Installed task skill requires explicit host invocation"
		check.Actions = []string{"Select Kitsu's task skill explicitly in the host's skill picker."}
	}
	return check
}

func componentPaths(root string, raw json.RawMessage, fallback string) ([]string, bool) {
	paths := []string{fallback}
	if len(raw) != 0 {
		var one string
		if json.Unmarshal(raw, &one) == nil {
			paths = []string{one}
		} else if json.Unmarshal(raw, &paths) != nil {
			return nil, false
		}
	}
	if len(paths) == 0 || len(paths) > 16 {
		return nil, false
	}
	for i, path := range paths {
		if !strings.HasPrefix(path, "./") || strings.Contains(path, `\`) || slices.Contains(strings.Split(path, "/"), "..") || !inside(root, filepath.Join(root, path)) {
			return nil, false
		}
		paths[i] = filepath.Join(root, path)
	}
	return paths, true
}

func inside(root, path string) bool {
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		path = resolved
	}
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

type hookGroup struct {
	Matcher  string `json:"matcher"`
	Enabled  *bool  `json:"enabled"`
	Disabled bool   `json:"disabled"`
	Hooks    []struct {
		Type     string `json:"type"`
		Command  string `json:"command"`
		Windows  string `json:"commandWindows"`
		Enabled  *bool  `json:"enabled"`
		Disabled bool   `json:"disabled"`
	} `json:"hooks"`
}

func hooksCheck(p plugin, host string, s settings) Check {
	check := Check{ID: "hooks", Status: "fail", Summary: "Required Kitsu hook configuration is unavailable", Actions: []string{"Repair or update the installed Kitsu hook bundle; inspect SessionStart, PreToolUse, SubagentStart, SubagentStop and SessionEnd in /hooks."}}
	if s.disabled || s.managedOnly && !(host == "claude" && s.managed[p.id]) {
		check.Detail = "Hooks are disabled or managed-only policy blocks this plugin's hooks."
		check.Actions = []string{"Review effective hook settings and managed restrictions with the host or your administrator."}
		return check
	}
	if p.root == "" {
		return check
	}
	if host == "codex" && len(p.manifest.Hooks) == 0 {
		check.Detail = "Codex manifest does not declare a hook bundle."
		return check
	}
	paths, ok := componentPaths(p.root, p.manifest.Hooks, "./hooks/hooks.json")
	if !ok {
		check.Detail = "Plugin hook paths are malformed or escape the installation."
		return check
	}
	events := map[string][]hookGroup{}
	for _, path := range paths {
		data, err := readFile(path)
		var bundle struct {
			Hooks map[string][]hookGroup `json:"hooks"`
		}
		if err != nil || json.Unmarshal(data, &bundle) != nil || bundle.Hooks == nil {
			check.Detail = "Cannot read a valid installed hook bundle."
			return check
		}
		for event, groups := range bundle.Hooks {
			events[event] = append(events[event], groups...)
		}
	}
	for _, event := range []string{"SessionStart", "PreToolUse", "SubagentStart", "SubagentStop", "SessionEnd"} {
		command := "tokens record"
		if event == "SessionStart" {
			command = "context"
		}
		if event == "PreToolUse" {
			command = "guard"
		}
		if !hasHook(events[event], host, event, command) {
			check.Detail = event + " is missing an enabled Kitsu " + command + " --host " + host + " command with the required matcher coverage."
			return check
		}
	}
	check.Status, check.Summary = "pass", "Kitsu's five required hook events have command definitions"
	check.Detail, check.Actions = "Installed hook bundle and local settings were inspected; hook trust, loading and execution remain unverified.", []string{}
	return check
}

func hasHook(groups []hookGroup, host, event, command string) bool {
	for _, group := range groups {
		if group.Disabled || group.Enabled != nil && !*group.Enabled || !coversTools(group.Matcher, host, event) {
			continue
		}
		for _, hook := range group.Hooks {
			line := hook.Command
			if runtime.GOOS == "windows" && hook.Windows != "" {
				line = hook.Windows
			}
			if hook.Type == "command" && !hook.Disabled && (hook.Enabled == nil || *hook.Enabled) && callsKitsu(line, command, host) {
				return true
			}
		}
	}
	return false
}

func coversTools(matcher, host, event string) bool {
	if matcher == "" || matcher == "*" {
		return true
	}
	if event != "PreToolUse" {
		return false
	}
	pattern, err := regexp.Compile("^(?:" + matcher + ")$")
	if err != nil {
		return false
	}
	tools := []string{"Bash", "apply_patch"}
	if host == "claude" {
		tools = []string{"Bash", "PowerShell", "Edit", "Write", "MultiEdit"}
	}
	for _, tool := range tools {
		if !pattern.MatchString(tool) {
			return false
		}
	}
	return true
}

func callsKitsu(line, command, host string) bool {
	return callsKitsuDepth(line, command, host, 3)
}

func callsKitsuDepth(line, command, host string, depth int) bool {
	commands, ok := shellwords.Split(line)
	if !ok {
		return false
	}
	want := strings.Fields(command + " --host " + host)
	for _, words := range commands {
		if depth > 0 && len(words) == 3 && (words[0] == "sh" || words[0] == "bash") && (words[1] == "-c" || words[1] == "-lc") {
			if callsKitsuDepth(words[2], command, host, depth-1) {
				return true
			}
		}
		if len(words) < len(want)+1 {
			continue
		}
		program := strings.ToLower(filepath.Base(words[0]))
		variable := (program == "$c" || program == "${c}") && strings.Contains(line, "c=$(command -v kitsu")
		if program != "kitsu" && program != "kitsu.exe" && !variable {
			continue
		}
		if slices.Equal(words[1:len(want)+1], want) && (len(words) == len(want)+1 || strings.HasPrefix(words[len(want)+1], "2>")) {
			return true
		}
	}
	return false
}
