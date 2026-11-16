package onboarding

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type skillRule struct {
	Path    string `toml:"path"`
	Name    string `toml:"name"`
	Enabled *bool  `toml:"enabled"`
}

type settings struct {
	host, home, hostDir, problem string
	enabled, managed             map[string]bool
	rules                        []skillRule
	disabled, managedOnly        bool
	pluginsDisabled              bool
}

type codexSettings struct {
	Plugins map[string]struct {
		Enabled *bool `toml:"enabled"`
	} `toml:"plugins"`
	Features struct {
		Plugins     *bool `toml:"plugins"`
		Hooks       *bool `toml:"hooks"`
		LegacyHooks *bool `toml:"codex_hooks"`
	} `toml:"features"`
	ManagedOnly *bool `toml:"allow_managed_hooks_only"`
	Skills      struct {
		Config []skillRule `toml:"config"`
	} `toml:"skills"`
}

type claudeSettings struct {
	EnabledPlugins map[string]bool `json:"enabledPlugins"`
	DisableHooks   *bool           `json:"disableAllHooks"`
	ManagedOnly    *bool           `json:"allowManagedHooksOnly"`
}

func readSettings(host, home, root, dir, system string) settings {
	s := settings{host: host, home: home, enabled: map[string]bool{}, managed: map[string]bool{}}
	env, fallback := "CODEX_HOME", ".codex"
	if host == "claude" {
		env, fallback = "CLAUDE_CONFIG_DIR", ".claude"
	}
	s.hostDir = filepath.Join(home, fallback)
	if value := os.Getenv(env); value != "" {
		s.hostDir = expand(value, home, dir)
	}
	for _, path := range s.paths(root, dir, system) {
		s.merge(path, false)
	}
	s.readManaged(system)
	return s
}

func (s settings) paths(root, dir, system string) []string {
	host, fallback := s.host, "."+s.host
	paths := []string{}
	if host == "codex" && system != "" {
		paths = append(paths, filepath.Join(system, "config.toml"))
	}
	file := "config.toml"
	if host == "claude" {
		file = "settings.json"
	}
	paths = append(paths, filepath.Join(s.hostDir, file))
	dirs := []string{dir}
	for current := dir; !samePath(current, root) && filepath.Dir(current) != current; {
		current = filepath.Dir(current)
		dirs = append(dirs, current)
	}
	if host == "claude" {
		dirs = []string{root}
	}
	for _, current := range slices.Backward(dirs) {
		paths = append(paths, filepath.Join(current, fallback, file))
		if host == "claude" {
			paths = append(paths, filepath.Join(current, fallback, "settings.local.json"))
		}
	}
	return paths
}

func (s *settings) readManaged(system string) {
	if system == "" {
		return
	}
	if s.host == "codex" {
		s.merge(filepath.Join(system, "requirements.toml"), true)
		return
	}
	s.merge(filepath.Join(system, "managed-settings.json"), true)
	files, err := readDirs(filepath.Join(system, "managed-settings.d"))
	if missing(err) {
		return
	}
	if err != nil {
		s.problem = "Cannot enumerate bounded managed settings fragments."
		return
	}
	slices.SortFunc(files, func(a, b os.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
	for _, file := range files {
		if !strings.HasPrefix(file.Name(), ".") && strings.HasSuffix(file.Name(), ".json") {
			s.merge(filepath.Join(system, "managed-settings.d", file.Name()), true)
		}
	}
}

func (s *settings) merge(path string, isManaged bool) {
	data, err := readFile(path)
	if missing(err) {
		return
	}
	if err != nil {
		s.problem = issue(path, "cannot read bounded regular settings file")
		return
	}
	if s.host == "claude" {
		s.mergeClaude(path, data, isManaged)
		return
	}
	var cfg codexSettings
	if toml.Unmarshal(data, &cfg) != nil {
		s.problem = issue(path, "malformed TOML host settings")
		return
	}
	for id, plugin := range cfg.Plugins {
		if plugin.Enabled != nil {
			s.enabled[id] = *plugin.Enabled
		}
	}
	if cfg.Features.Plugins != nil {
		s.pluginsDisabled = !*cfg.Features.Plugins
	}
	if cfg.Features.LegacyHooks != nil {
		s.disabled = !*cfg.Features.LegacyHooks
	}
	if cfg.Features.Hooks != nil {
		s.disabled = !*cfg.Features.Hooks
	}
	if cfg.ManagedOnly != nil {
		s.managedOnly = *cfg.ManagedOnly
	}
	for _, rule := range cfg.Skills.Config {
		if (rule.Path == "") == (rule.Name == "") || rule.Enabled == nil {
			s.problem = issue(path, "invalid skills.config policy")
			continue
		}
		if rule.Path != "" {
			base := filepath.Dir(filepath.Dir(path))
			if samePath(filepath.Dir(path), s.hostDir) {
				base = s.hostDir
			}
			rule.Path = expand(rule.Path, s.home, base)
			if filepath.Base(rule.Path) != "SKILL.md" {
				rule.Path = filepath.Join(rule.Path, "SKILL.md")
			}
		}
		s.rules = append(s.rules, rule)
	}
}

func (s *settings) mergeClaude(path string, data []byte, isManaged bool) {
	var cfg claudeSettings
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(data, &cfg) != nil {
		s.problem = issue(path, "malformed JSON host settings")
		return
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(data, &fields) // The typed decode above already checked JSON.
	for _, key := range []string{"enabledPlugins", "disableAllHooks", "allowManagedHooksOnly"} {
		if bytes.Equal(bytes.TrimSpace(fields[key]), []byte("null")) {
			s.problem = issue(path, "null hook or plugin policy cannot be verified")
			return
		}
	}
	for id, enabled := range cfg.EnabledPlugins {
		s.enabled[id] = enabled
		if isManaged {
			s.managed[id] = enabled
		}
	}
	if cfg.DisableHooks != nil {
		s.disabled = *cfg.DisableHooks
	}
	if cfg.ManagedOnly != nil {
		s.managedOnly = *cfg.ManagedOnly
	}
}

func kitsuID(id string) bool {
	name, market, ok := strings.Cut(id, "@")
	return ok && name == "kitsu" && market != "" && !strings.ContainsAny(market, "/\\:@\r\n\x00") && market != "." && market != ".."
}
