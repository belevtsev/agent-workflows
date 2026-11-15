package skills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type installation struct {
	Scope       string `json:"scope"`
	ProjectPath string `json:"projectPath"`
	InstallPath string `json:"installPath"`
}

func (s *scanner) claudePlugins(root string, enabled map[string]bool) []source {
	for _, id := range sortedKeys(enabled) {
		if name, market, valid := pluginID(id); valid && !enabled[id] {
			path := filepath.Join(s.hostDir, "plugins", "cache", market, name)
			s.catalog.blocked = append(s.catalog.blocked, block{path: path, reason: "plugin is disabled in host settings"})
		}
	}
	path := filepath.Join(s.hostDir, "plugins", "installed_plugins.json")
	data, err := readFile(path)
	if missing(err) {
		return nil
	}
	var inventory struct {
		Version int                        `json:"version"`
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err != nil || json.Unmarshal(data, &inventory) != nil || (inventory.Version != 1 && inventory.Version != 2) {
		s.warn(path, "cannot read supported installed plugin inventory (version 1 or 2)")
		return nil
	}
	sources := []source{}
	for _, id := range sortedKeys(inventory.Plugins) {
		if _, _, valid := pluginID(id); !valid {
			s.warn(path, "invalid installed plugin identifier")
			continue
		}
		var installs []installation
		raw := inventory.Plugins[id]
		if inventory.Version == 1 {
			var install installation
			err = json.Unmarshal(raw, &install)
			installs = []installation{install}
		} else {
			err = json.Unmarshal(raw, &installs)
		}
		if err != nil {
			s.warn(path, "invalid installation metadata for "+id)
			continue
		}
		if !enabled[id] {
			for _, install := range installs {
				if install.InstallPath != "" {
					pluginRoot := s.expandPath(install.InstallPath, s.hostDir)
					s.catalog.blocked = append(s.catalog.blocked, block{path: pluginRoot, reason: "plugin is not enabled in host settings"})
				}
			}
		}
		install, ok := chooseInstallation(installs, root)
		if !ok || install.InstallPath == "" {
			s.warn(path, "no unambiguous installation for "+id+" in this scope")
			continue
		}
		pluginRoot := s.expandPath(install.InstallPath, s.hostDir)
		sources = append(sources, s.pluginSources(pluginRoot, id, enabled[id])...)
	}
	return sources
}

func chooseInstallation(installs []installation, root string) (installation, bool) {
	best, rank, count := installation{}, 0, 0
	for _, install := range installs {
		priority := 0
		switch install.Scope {
		case "", "user", "managed":
			priority = 1
		case "project", "local":
			if root != "" && realPath(install.ProjectPath) == realPath(root) {
				priority = 2
				if install.Scope == "local" {
					priority = 3
				}
			}
		}
		if priority > rank {
			best, rank, count = install, priority, 1
		} else if priority == rank && priority > 0 && install.InstallPath != best.InstallPath {
			count++
		}
	}
	return best, rank > 0 && count == 1
}

func (s *scanner) codexPlugins(root string, enabled map[string]bool) []source {
	locals := s.localCodexPlugins(root)
	sources := []source{}
	for _, id := range sortedKeys(enabled) {
		name, market, valid := pluginID(id)
		if !valid {
			s.warn(s.hostDir, "invalid configured plugin identifier")
			continue
		}
		base := filepath.Join(s.hostDir, "plugins", "cache", market, name)
		if !enabled[id] {
			s.catalog.blocked = append(s.catalog.blocked, block{path: base, reason: "plugin is disabled in host settings"})
		}
		pluginRoot := filepath.Join(base, "local")
		if !locals[id] {
			pluginRoot = cachedVersion(base)
		}
		if pluginRoot == "" {
			s.warn(base, "installed plugin version is unknown or ambiguous; use an explicit skill root")
			continue
		}
		sources = append(sources, s.pluginSources(pluginRoot, id, enabled[id])...)
	}
	cache := filepath.Join(s.hostDir, "plugins", "cache")
	if info, err := os.Stat(cache); err == nil && info.IsDir() {
		s.warn(cache, "desktop or workspace-managed plugin state is not enumerated; use explicit roots for those skills")
	}
	return sources
}

func (s *scanner) localCodexPlugins(root string) map[string]bool {
	paths := []string{filepath.Join(s.home, ".agents", "plugins", "marketplace.json")}
	if root != "" {
		paths = append(paths,
			filepath.Join(root, ".agents", "plugins", "marketplace.json"),
			filepath.Join(root, ".claude-plugin", "marketplace.json"),
		)
	}
	locals := map[string]bool{}
	for _, path := range paths {
		data, err := readFile(path)
		if missing(err) {
			continue
		}
		var market struct {
			Name    string `json:"name"`
			Plugins []struct {
				Name   string          `json:"name"`
				Source json.RawMessage `json:"source"`
			} `json:"plugins"`
		}
		if err != nil || json.Unmarshal(data, &market) != nil || !component(market.Name) {
			s.warn(path, "invalid local marketplace metadata")
			continue
		}
		for _, plugin := range market.Plugins {
			var plain string
			var src struct {
				Source string `json:"source"`
			}
			local := json.Unmarshal(plugin.Source, &plain) == nil && strings.HasPrefix(plain, "./")
			if json.Unmarshal(plugin.Source, &src) == nil && src.Source == "local" {
				local = true
			}
			if component(plugin.Name) && local {
				locals[plugin.Name+"@"+market.Name] = true
			}
		}
	}
	return locals
}

func cachedVersion(base string) string {
	entries, err := readDirs(base)
	if err != nil {
		return ""
	}
	paths := []string{}
	for _, entry := range entries {
		path := filepath.Join(base, entry.Name())
		if entry.IsDir() && component(entry.Name()) && manifestPath(path, "codex") != "" {
			paths = append(paths, path)
		}
	}
	if len(paths) == 1 {
		return paths[0]
	}
	return ""
}

func (s *scanner) pluginSources(root, id string, enabled bool) []source {
	reason := ""
	if !enabled {
		reason = "plugin is not enabled in host settings"
		s.catalog.blocked = append(s.catalog.blocked, block{path: root, reason: reason})
	}
	paths := []string{"./skills"}
	manifest := manifestPath(root, s.catalog.host)
	if manifest != "" {
		extra, err := manifestSkills(manifest)
		if err != nil {
			s.warn(manifest, err.Error())
			reason = "plugin manifest is malformed"
			s.catalog.blocked = append(s.catalog.blocked, block{path: root, reason: reason})
		} else {
			paths = append(paths, extra...)
		}
	}
	if s.catalog.host == "claude" && len(paths) == 1 {
		if _, err := os.Stat(filepath.Join(root, "skills")); missing(err) {
			if _, err := os.Stat(filepath.Join(root, "SKILL.md")); err == nil {
				paths = []string{"."}
			}
		}
	}
	sources := []source{}
	for _, rel := range paths {
		path := filepath.Join(root, rel)
		if !within(root, path) || filepath.IsAbs(rel) || !validRelative(rel) {
			s.warn(manifest, "plugin skill path escapes its installation or has an invalid form")
			continue
		}
		sources = append(sources, source{path: path, base: root, scope: "plugin", namespace: id, reason: reason})
	}
	return sources
}

func manifestPath(root, host string) string {
	paths := []string{"plugin.json", filepath.Join(".codex-plugin", "plugin.json"), filepath.Join(".claude-plugin", "plugin.json")}
	if host == "claude" {
		paths[0], paths[2] = paths[2], paths[0]
	}
	for _, path := range paths {
		path = filepath.Join(root, path)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func manifestSkills(path string) ([]string, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read plugin manifest")
	}
	var manifest struct {
		Skills json.RawMessage `json:"skills"`
	}
	if json.Unmarshal(data, &manifest) != nil {
		return nil, fmt.Errorf("invalid plugin manifest JSON")
	}
	if len(manifest.Skills) == 0 {
		return nil, nil
	}
	var pathValue string
	if json.Unmarshal(manifest.Skills, &pathValue) == nil {
		return []string{pathValue}, nil
	}
	var paths []string
	if json.Unmarshal(manifest.Skills, &paths) != nil {
		return nil, fmt.Errorf("plugin skills must be a path or array of paths")
	}
	return paths, nil
}

func validRelative(path string) bool {
	if path != "." && !strings.HasPrefix(path, "./") {
		return false
	}
	for _, part := range strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}

func pluginID(id string) (string, string, bool) {
	name, market, ok := strings.Cut(id, "@")
	return name, market, ok && component(name) && component(market)
}

func component(text string) bool {
	return text != "" && text != "." && text != ".." && !strings.ContainsAny(text, "/\\:@\r\n\x00")
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
