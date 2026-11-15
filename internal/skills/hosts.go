package skills

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type codexConfig struct {
	Plugins map[string]struct {
		Enabled *bool `toml:"enabled"`
	} `toml:"plugins"`
	Skills struct {
		Config []struct {
			Path    string `toml:"path"`
			Name    string `toml:"name"`
			Enabled *bool  `toml:"enabled"`
		} `toml:"config"`
		Bundled struct {
			Enabled *bool `toml:"enabled"`
		} `toml:"bundled"`
	} `toml:"skills"`
}

type claudeSettings struct {
	EnabledPlugins map[string]bool   `json:"enabledPlugins"`
	SkillOverrides map[string]string `json:"skillOverrides"`
}

func (s *scanner) hostSources(root, host string) []source {
	dirs := repoDirectories(root)
	sources := make([]source, 0, len(dirs)+2)
	if host == "codex" {
		s.hostDir = s.environmentDir("CODEX_HOME", ".codex")
		for _, dir := range dirs {
			sources = append(sources, source{path: filepath.Join(dir, ".agents", "skills"), base: root, scope: "repo"})
		}
		personal := filepath.Join(s.home, ".agents", "skills")
		legacy := filepath.Join(s.hostDir, "skills")
		sources = append(sources, source{path: personal, base: personal, scope: "user"})
		sources = append(sources, source{path: legacy, base: legacy, scope: "legacy"})
		plugins := s.codexSettings(dirs)
		return append(sources, s.codexPlugins(root, plugins)...)
	}
	s.hostDir = s.environmentDir("CLAUDE_CONFIG_DIR", ".claude")
	for _, dir := range dirs {
		sources = append(sources, source{path: filepath.Join(dir, ".claude", "skills"), base: root, scope: "repo"})
	}
	personal := filepath.Join(s.hostDir, "skills")
	sources = append(sources, source{path: personal, base: personal, scope: "user"})
	plugins := s.claudeSettings(root)
	return append(sources, s.claudePlugins(root, plugins)...)
}

func (s *scanner) codexSettings(dirs []string) map[string]bool {
	paths := []string{filepath.Join(s.hostDir, "config.toml")}
	for _, dir := range slices.Backward(dirs) {
		paths = append(paths, filepath.Join(dir, ".codex", "config.toml"))
	}
	plugins, bundled := map[string]bool{}, true
	for _, path := range paths {
		data, err := readFile(path)
		if missing(err) {
			continue
		}
		var cfg codexConfig
		if err != nil || toml.Unmarshal(data, &cfg) != nil {
			s.invalidSettings(path)
			continue
		}
		for name, plugin := range cfg.Plugins {
			if plugin.Enabled != nil {
				plugins[name] = *plugin.Enabled
			}
		}
		if cfg.Skills.Bundled.Enabled != nil {
			bundled = *cfg.Skills.Bundled.Enabled
		}
		for _, setting := range cfg.Skills.Config {
			if (setting.Path == "") == (setting.Name == "") || setting.Enabled == nil {
				s.invalidSettings(path)
				s.warn(path, "skill setting requires exactly one path or name selector and enabled")
				continue
			}
			base := s.hostDir
			if path != paths[0] {
				base = filepath.Dir(filepath.Dir(path))
			}
			rule := codexRule{name: setting.Name, enabled: *setting.Enabled}
			if setting.Path != "" {
				rule.path = skillPath(s.expandPath(setting.Path, base))
			}
			s.catalog.codexRules = append(s.catalog.codexRules, rule)
		}
	}
	if !bundled {
		path := filepath.Join(s.hostDir, "skills", ".system")
		s.catalog.blocked = append(s.catalog.blocked, block{path: path, reason: "bundled Codex skills are disabled"})
	}
	return plugins
}

func (s *scanner) claudeSettings(root string) map[string]bool {
	paths := []string{filepath.Join(s.hostDir, "settings.json")}
	if root != "" {
		paths = append(paths,
			filepath.Join(root, ".claude", "settings.json"),
			filepath.Join(root, ".claude", "settings.local.json"),
		)
	}
	plugins := map[string]bool{}
	for _, path := range paths {
		data, err := readFile(path)
		if missing(err) {
			continue
		}
		var cfg claudeSettings
		if err != nil || json.Unmarshal(data, &cfg) != nil {
			s.invalidSettings(path)
			continue
		}
		for name, enabled := range cfg.EnabledPlugins {
			plugins[name] = enabled
		}
		for name, state := range cfg.SkillOverrides {
			switch state {
			case "on", "name-only", "user-invocable-only", "off":
				s.catalog.overrides[name] = state
			default:
				s.warn(path, "unknown skillOverrides state")
			}
		}
	}
	return plugins
}

func (s *scanner) invalidSettings(path string) {
	s.warn(path, "cannot read valid host settings; skill enablement cannot be verified")
	s.catalog.policyError = "host settings are unreadable or malformed; repair them before selecting skills"
}

func (s *scanner) environmentDir(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return s.expandPath(value, "")
	}
	return filepath.Join(s.home, fallback)
}

func (s *scanner) expandPath(path, base string) string {
	if path == "~" {
		return s.home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(s.home, path[2:])
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	absolute, err := filepath.Abs(path)
	if err == nil {
		return absolute
	}
	return path
}

func repoDirectories(root string) []string {
	if root == "" {
		return nil
	}
	cwd, err := os.Getwd()
	if err != nil || !within(root, cwd) {
		cwd = root
	}
	dirs := []string{}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if realPath(dir) == realPath(root) || filepath.Dir(dir) == dir {
			return dirs
		}
	}
}
