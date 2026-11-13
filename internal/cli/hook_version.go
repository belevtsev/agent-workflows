package cli

import (
	"cmp"
	json "encoding/json/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Version notes for the session-start context. The plugin and cw are
// installed apart, so either can be older than the other, and the plugin
// needs a minimum Claude Code client. Each note is one short line, because
// the session-start context is paid for in every session.
const (
	// minClaudeCodeVersion is the oldest Claude Code client README.md
	// supports: the coordinator passes effort on every dispatch.
	minClaudeCodeVersion = "2.1.292"
	// pluginManifestLimit bounds the plugin manifest read at session start.
	pluginManifestLimit = 64 << 10
	// codexPluginRoot is set by Codex, not by Claude Code, for a plugin hook;
	// Codex documents it as its own extension. Without --host it names the
	// host at session start, whose Codex input has no turn_id (hookHost).
	codexPluginRoot = "PLUGIN_ROOT"
	// claudeCodeAgentVariable is where Claude Code, since 2.1.120, names
	// itself and its version for its subprocesses, hooks included, unless the
	// environment it starts in already names another agent.
	claudeCodeAgentVariable = "AI_AGENT"
)

// claudeCodeAgent matches AI_AGENT as Claude Code sets it for a hook, such as
// claude-code_2-1-311_harness: the client version with dashes for dots. It is
// not in Claude Code's reference documentation; any other value gives no note.
var claudeCodeAgent = regexp.MustCompile(`^claude-code_([0-9]{1,9})-([0-9]{1,9})-([0-9]{1,9})_[a-z]+$`)

// pseudoVersion matches the pre-release of a Go pseudo-version, which ends in
// a 14-digit UTC time and a 12-character commit prefix.
var pseudoVersion = regexp.MustCompile(`(^|\.)[0-9]{14}-[0-9a-f]{12}$`)

// versionNotes returns the session-start lines that report version drift,
// one per line, or "" when there is none or it cannot be told. It compares
// the version of the plugin manifest beside the instructions file with cw's
// own, and, under Claude Code (codex false), the client's version with
// minClaudeCodeVersion.
func versionNotes(config Config, instructions string, codex bool) string {
	var notes []string
	if note := pluginVersionNote(pluginManifestVersion(instructions, codex), config.Version); note != "" {
		notes = append(notes, note)
	}
	if !codex {
		if note := clientVersionNote(config.Getenv(claudeCodeAgentVariable)); note != "" {
			notes = append(notes, note)
		}
	}
	return strings.Join(notes, "\n")
}

// pluginManifestVersion returns the version member of the manifest of the
// host that runs the hook, in the plugin root above the instructions file
// (global/AGENTS.md): .codex-plugin/plugin.json under Codex, else
// .claude-plugin/plugin.json. It returns "" when the manifest cannot be read.
func pluginManifestVersion(instructions string, codex bool) string {
	manifest := filepath.Join(".claude-plugin", "plugin.json")
	if codex {
		manifest = filepath.Join(".codex-plugin", "plugin.json")
	}
	path := filepath.Join(filepath.Dir(filepath.Dir(instructions)), manifest)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > pluginManifestLimit {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var decoded struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return ""
	}
	return decoded.Version
}

// pluginVersionNote returns the line for a plugin and cw whose versions differ
// on major.minor.patch or pre-release, naming the one to update. A cw built
// from source between tags (dev, or a Go pseudo-version) and a version that
// is not semver give no note.
func pluginVersionNote(plugin, cw string) string {
	pluginVersion, ok := parseSemver(plugin)
	if !ok {
		return ""
	}
	cwVersion, ok := parseSemver(cw)
	if !ok || pseudoVersion.MatchString(strings.Join(cwVersion.pre, ".")) {
		return ""
	}
	advice := "upgrade cw"
	switch pluginVersion.compare(cwVersion) {
	case 0:
		return ""
	case -1:
		advice = "update the plugin"
	}
	return "plugin " + strings.TrimPrefix(plugin, "v") + ", cw " + strings.TrimPrefix(cw, "v") + ": " + advice
}

// clientVersionNote returns the line for a Claude Code client older than
// minClaudeCodeVersion, read from agent, the hook's AI_AGENT. It returns ""
// for any value claudeCodeAgent does not match.
func clientVersionNote(agent string) string {
	match := claudeCodeAgent.FindStringSubmatch(agent)
	if match == nil {
		return ""
	}
	client, ok := parseSemver(match[1] + "." + match[2] + "." + match[3])
	minimum, _ := parseSemver(minClaudeCodeVersion)
	if !ok || client.compare(minimum) >= 0 {
		return ""
	}
	return "Claude Code " + strings.Join(match[1:], ".") + ", agent-workflows needs " + minClaudeCodeVersion + ": upgrade Claude Code"
}

// semver is a semantic version without its build metadata, which does not
// count towards precedence.
type semver struct {
	core [3]int
	pre  []string
}

// parseSemver parses a semantic version with an optional leading v, as cw's
// release builds and go install report it.
func parseSemver(version string) (semver, bool) {
	rest, build, hasBuild := strings.Cut(strings.TrimPrefix(version, "v"), "+")
	if hasBuild && !identifiers(build) {
		return semver{}, false
	}
	core, pre, hasPre := strings.Cut(rest, "-")
	var parsed semver
	parts := strings.Split(core, ".")
	if len(parts) != len(parsed.core) {
		return semver{}, false
	}
	for i, part := range parts {
		if !digits(part) || len(part) > 9 {
			return semver{}, false
		}
		parsed.core[i], _ = strconv.Atoi(part)
	}
	if hasPre {
		if !identifiers(pre) {
			return semver{}, false
		}
		parsed.pre = strings.Split(pre, ".")
	}
	return parsed, true
}

// compare orders a and b by semantic version precedence: -1, 0 or +1.
func (a semver) compare(b semver) int {
	for i := range a.core {
		if c := cmp.Compare(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	if len(a.pre) == 0 || len(b.pre) == 0 {
		// A release ranks above its pre-releases.
		return cmp.Compare(len(b.pre), len(a.pre))
	}
	for i := range min(len(a.pre), len(b.pre)) {
		if c := comparePrerelease(a.pre[i], b.pre[i]); c != 0 {
			return c
		}
	}
	return cmp.Compare(len(a.pre), len(b.pre))
}

// comparePrerelease orders two pre-release identifiers: numeric ones by
// value, below alphanumeric ones, which compare in ASCII order.
func comparePrerelease(a, b string) int {
	aNumeric, bNumeric := digits(a), digits(b)
	switch {
	case aNumeric && bNumeric:
		a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
		return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
	case aNumeric:
		return -1
	case bNumeric:
		return 1
	}
	return strings.Compare(a, b)
}

// identifiers reports whether text is dot-separated, non-empty identifiers
// of ASCII letters, digits and hyphens.
func identifiers(text string) bool {
	for identifier := range strings.SplitSeq(text, ".") {
		if identifier == "" || strings.ContainsFunc(identifier, func(r rune) bool {
			return !(r == '-' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
		}) {
			return false
		}
	}
	return true
}

// digits reports whether text is one or more ASCII digits.
func digits(text string) bool {
	return text != "" && strings.Trim(text, "0123456789") == ""
}
