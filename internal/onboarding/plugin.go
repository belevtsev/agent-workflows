package onboarding

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type manifest struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Skills  json.RawMessage `json:"skills"`
	Hooks   json.RawMessage `json:"hooks"`
}

type plugin struct {
	id, root string
	manifest manifest
}

type installation struct{ Scope, InstallPath, ProjectPath string }

func findPlugin(s settings, root string) (plugin, Check) {
	check := Check{ID: "plugin", Status: "fail", Summary: "Kitsu plugin is not installed and enabled for this scope", Actions: []string{"Install and enable kitsu@kitsu through the host's plugin manager in this project or user scope; a checkout or marketplace cache alone is insufficient."}}
	if s.problem != "" {
		check.Detail = "Repair host settings before checking plugin enablement."
		return plugin{}, check
	}
	if s.pluginsDisabled {
		check.Summary = "Codex plugins are disabled by effective settings"
		check.Detail = "The plugins feature is off even though individual plugin entries may be enabled."
		check.Actions = []string{"Review [features].plugins in the applicable Codex config.toml and managed requirements; enable plugins if allowed by your policy."}
		return plugin{}, check
	}
	ids := []string{}
	for id, enabled := range s.enabled {
		if kitsuID(id) {
			if enabled {
				ids = append(ids, id)
			} else {
				check.Summary = "Kitsu plugin is disabled in host settings"
			}
		}
	}
	if len(ids) != 1 {
		if len(ids) > 1 {
			check.Summary = "Multiple Kitsu plugins are enabled; installation is ambiguous"
		}
		return plugin{}, check
	}
	p := plugin{id: ids[0]}
	if s.host == "claude" {
		p.root = claudeInstallation(s, p.id, root)
	} else {
		p.root = codexInstallation(s, p.id)
	}
	var valid bool
	p.manifest, valid = readManifest(p.root, s.host)
	if p.root == "" || !valid {
		check.Detail = "Enabled settings do not identify one readable installed Kitsu manifest in this scope; inventory may be absent, malformed, stale or ambiguous."
		return plugin{}, check
	}
	check.Status, check.Summary = "pass", "Kitsu plugin installation and enablement are configured"
	check.Detail, check.Actions = p.id+" at "+p.root+"; host loading is not verified.", []string{}
	return p, check
}

func codexInstallation(s settings, id string) string {
	_, market, _ := strings.Cut(id, "@")
	base := filepath.Join(s.hostDir, "plugins", "cache", market, "kitsu")
	entries, err := readDirs(base)
	if err != nil {
		return ""
	}
	root := ""
	for _, entry := range entries {
		candidate := filepath.Join(base, entry.Name())
		if info, err := os.Stat(candidate); err != nil || !info.IsDir() {
			continue
		}
		if _, ok := readManifest(candidate, s.host); !ok {
			continue
		}
		if root != "" {
			return ""
		}
		root = candidate
	}
	return root
}

func readManifest(root, host string) (manifest, bool) {
	if root == "" {
		return manifest{}, false
	}
	paths := []string{filepath.Join(".codex-plugin", "plugin.json"), "plugin.json", filepath.Join(".claude-plugin", "plugin.json")}
	if host == "claude" {
		paths[0], paths[2] = paths[2], paths[0]
	}
	for _, path := range paths {
		data, err := readFile(filepath.Join(root, path))
		if missing(err) {
			continue
		}
		var m manifest
		valid := err == nil && json.Unmarshal(data, &m) == nil && m.Name == "kitsu"
		return m, valid
	}
	return manifest{}, false
}

func claudeInstallation(s settings, id, root string) string {
	data, err := readFile(filepath.Join(s.hostDir, "plugins", "installed_plugins.json"))
	var inventory struct {
		Version int                        `json:"version"`
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if err != nil || json.Unmarshal(data, &inventory) != nil || (inventory.Version != 1 && inventory.Version != 2) {
		return ""
	}
	var installs []installation
	if inventory.Version == 1 {
		var one installation
		err = json.Unmarshal(inventory.Plugins[id], &one)
		installs = []installation{one}
	} else {
		err = json.Unmarshal(inventory.Plugins[id], &installs)
	}
	if err != nil {
		return ""
	}
	best, rank, ambiguous := "", 0, false
	for _, install := range installs {
		priority := install.rank(root, inventory.Version)
		if priority == 0 || install.InstallPath == "" {
			continue
		}
		path := expand(install.InstallPath, s.home, s.hostDir)
		if priority > rank {
			best, rank, ambiguous = path, priority, false
		} else if priority == rank && !samePath(best, path) {
			ambiguous = true
		}
	}
	if ambiguous {
		return ""
	}
	return best
}

func (i installation) rank(root string, version int) int {
	switch i.Scope {
	case "":
		if version == 1 {
			return 1
		}
	case "user", "managed":
		return 1
	case "project", "local":
		if filepath.IsAbs(i.ProjectPath) && samePath(i.ProjectPath, root) {
			if i.Scope == "local" {
				return 3
			}
			return 2
		}
	}
	return 0
}

func readDirs(path string) ([]os.DirEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	entries, err := file.ReadDir(65)
	if len(entries) > 64 {
		return nil, io.ErrShortBuffer
	}
	if err == io.EOF {
		err = nil
	}
	return entries, err
}
