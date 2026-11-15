package skills_test

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillClaudeInstalledPlugins(t *testing.T) {
	f := newInventory(t)
	pluginRoot := filepath.Join(f.claude, "plugins", "cache", "market", "tools", "1.2.3")
	disabledRoot := filepath.Join(f.claude, "plugins", "cache", "market", "disabled", "1.0.0")
	f.write("claude/plugins/cache/market/tools/1.2.3/.claude-plugin/plugin.json", `{"name":"tools","skills":["./additional"]}`)
	f.write("claude/plugins/cache/market/tools/1.2.3/skills/review/SKILL.md", skillText("review", "Default plugin source."))
	f.write("claude/plugins/cache/market/tools/1.2.3/additional/SKILL.md", skillText("extra", "Additional plugin source."))
	blockedPath := f.write("claude/plugins/cache/market/disabled/1.0.0/skills/blocked/SKILL.md", skillText("blocked", "Disabled plugin source."))
	f.write("claude/plugins/cache/market/uninstalled/1.0.0/skills/ghost/SKILL.md", skillText("ghost", "Cached but uninstalled."))
	f.write("claude/skills/review/SKILL.md", skillText("review", "Hidden personal source."))
	f.write("claude/settings.json", `{"enabledPlugins":{"tools@market":true,"disabled@market":false},"skillOverrides":{"review":"off"}}`)
	f.installs(map[string]any{
		"tools@market":    []any{installRecord("user", pluginRoot, "")},
		"disabled@market": []any{installRecord("user", disabledRoot, "")},
	})
	catalog := f.discover("claude")
	if len(catalog.Skills) != 4 {
		t.Fatalf("installed plugin catalog = %+v", catalog)
	}
	for _, ref := range []string{"claude:plugin:tools@market:skills/review", "claude:plugin:tools@market:additional"} {
		entries, err := catalog.Resolve([]string{ref})
		if err != nil || len(entries) != 1 || entries[0].Disabled || entries[0].Scope != "plugin" {
			t.Fatalf("plugin metadata/visibility: %+v, %v", entries, err)
		}
	}
	if _, err := catalog.Resolve([]string{"review"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("plugin and personal duplicate picked silently: %v", err)
	}
	if !findSkill(t, catalog, "blocked").Disabled {
		t.Fatal("disabled plugin reported enabled")
	}
	if _, err := catalog.Resolve([]string{blockedPath}); err == nil {
		t.Fatal("disabled plugin bypassed by absolute path")
	}
	if _, err := catalog.Resolve([]string{"ghost"}); err == nil {
		t.Fatal("uninstalled cache skill was discovered")
	}
}

func TestSkillClaudePluginScopeAndVersionAmbiguity(t *testing.T) {
	f := newInventory(t)
	first := filepath.Join(f.claude, "plugins", "cache", "market", "tools", "1")
	second := filepath.Join(f.claude, "plugins", "cache", "market", "tools", "2")
	f.write("claude/plugins/cache/market/tools/1/skills/old/SKILL.md", skillText("old", "User installation."))
	f.write("claude/plugins/cache/market/tools/2/skills/new/SKILL.md", skillText("new", "Project installation."))
	f.write("claude/settings.json", `{"enabledPlugins":{"tools@market":true}}`)
	f.installs(map[string]any{"tools@market": []any{
		installRecord("user", first, ""), installRecord("project", second, f.repo),
	}})
	if catalog := f.discover("claude"); len(catalog.Skills) != 1 || catalog.Skills[0].Name != "new" {
		t.Fatalf("applicable project installation not selected: %+v", catalog)
	}
	f.installs(map[string]any{"tools@market": []any{
		installRecord("user", first, ""), installRecord("project", second, filepath.Join(f.base, "other-repo")),
	}})
	if catalog := f.discover("claude"); len(catalog.Skills) != 1 || catalog.Skills[0].Name != "old" {
		t.Fatalf("foreign project installation leaked: %+v", catalog)
	}
	f.installs(map[string]any{"tools@market": []any{
		installRecord("user", first, ""), installRecord("user", second, ""),
	}})
	if catalog := f.discover("claude"); len(catalog.Skills) != 0 || len(catalog.Warnings) == 0 {
		t.Fatalf("multiple installation versions silently selected: %+v", catalog)
	}
}

func TestSkillCodexConfiguredPluginCache(t *testing.T) {
	f := newInventory(t)
	f.write("codex/config.toml", "[plugins.\"tools@market\"]\nenabled = true\n[plugins.\"disabled@market\"]\nenabled = false\n")
	f.write("home/.agents/plugins/marketplace.json", `{"name":"market","plugins":[{"name":"tools","source":{"source":"local","path":"./editable"}},{"name":"disabled","source":"./disabled"},{"name":"uninstalled","source":"./uninstalled"}]}`)
	f.write("home/editable/skills/stale/SKILL.md", skillText("stale", "Not the installed copy."))
	f.write("codex/plugins/cache/market/tools/local/plugin.json", `{"name":"tools"}`)
	installed := f.write("codex/plugins/cache/market/tools/local/skills/installed/SKILL.md", skillText("installed", "Installed cache copy."))
	f.write("codex/plugins/cache/market/disabled/local/plugin.json", `{"name":"disabled"}`)
	blocked := f.write("codex/plugins/cache/market/disabled/local/skills/blocked/SKILL.md", skillText("blocked", "Disabled local plugin."))
	f.write("codex/plugins/cache/market/uninstalled/local/plugin.json", `{"name":"uninstalled"}`)
	f.write("codex/plugins/cache/market/uninstalled/local/skills/ghost/SKILL.md", skillText("ghost", "Uninstalled cache."))
	catalog := f.discover("codex")
	if len(catalog.Skills) != 2 || findSkill(t, catalog, "installed").Path != installed {
		t.Fatalf("configured installed cache catalog = %+v", catalog)
	}
	entries, err := catalog.Resolve([]string{"codex:plugin:tools@market:skills/installed"})
	if err != nil || len(entries) != 1 || entries[0].SHA256 != fileHash(t, installed) {
		t.Fatalf("installed source not resolved: %+v, %v", entries, err)
	}
	if _, err := catalog.Resolve([]string{blocked}); err == nil {
		t.Fatal("disabled Codex plugin selected through source path")
	}
	f.write("repo/.codex/config.toml", "[plugins.\"tools@market\"]\nenabled = false\n")
	if _, err := f.discover("codex").Resolve([]string{installed}); err == nil {
		t.Fatal("project plugin disable ignored")
	}
}

func TestSkillCodexPluginUnknownVersionRequiresExplicitRoot(t *testing.T) {
	f := newInventory(t)
	f.write("codex/config.toml", "[plugins.\"tools@market\"]\nenabled = true\n")
	f.write("codex/plugins/cache/market/tools/1.0.0/.codex-plugin/plugin.json", `{"name":"tools","skills":"./skills"}`)
	f.write("codex/plugins/cache/market/tools/1.0.0/skills/one/SKILL.md", skillText("one", "Only installed candidate."))
	if catalog := f.discover("codex"); len(catalog.Skills) != 1 {
		t.Fatalf("single configured installed candidate not discovered: %+v", catalog)
	}
	f.write("codex/plugins/cache/market/tools/2.0.0/plugin.json", `{"name":"tools"}`)
	f.write("codex/plugins/cache/market/tools/2.0.0/skills/two/SKILL.md", skillText("two", "Another cached version."))
	catalog := f.discover("codex")
	if len(catalog.Skills) != 0 || !strings.Contains(strings.Join(catalog.Warnings, " "), "ambiguous") {
		t.Fatalf("ambiguous cache selected a version: %+v", catalog)
	}
}

func TestSkillPluginMalformedMetadataAndEscapingPaths(t *testing.T) {
	f := newInventory(t)
	root := filepath.Join(f.claude, "plugins", "cache", "market", "tools", "1")
	f.write("claude/settings.json", `{"enabledPlugins":{"tools@market":true,"broken@market":true}}`)
	f.write("claude/plugins/cache/market/tools/1/.claude-plugin/plugin.json", `{"skills":42}`)
	f.write("claude/plugins/cache/market/tools/1/skills/found/SKILL.md", skillText("found", "Malformed manifest."))
	f.installs(map[string]any{
		"tools@market":  []any{installRecord("user", root, "")},
		"broken@market": "invalid installation record",
	})
	catalog := f.discover("claude")
	if !findSkill(t, catalog, "found").Disabled || len(catalog.Warnings) < 2 {
		t.Fatalf("malformed plugin state not diagnosed: %+v", catalog)
	}
	if _, err := catalog.Resolve([]string{"found"}); err == nil {
		t.Fatal("skill from malformed plugin selected")
	}
	f.write("claude/plugins/cache/market/tools/1/.claude-plugin/plugin.json", `{"skills":["./../../../../outside"]}`)
	f.write("claude/plugins/outside/SKILL.md", skillText("escape", "Outside plugin root."))
	catalog = f.discover("claude")
	if len(catalog.Skills) != 1 || strings.Contains(fmt.Sprint(catalog.Skills), "escape") || len(catalog.Warnings) == 0 {
		t.Fatalf("escaping plugin path was followed: %+v", catalog)
	}
}

func TestSkillClaudeLegacyPluginInventory(t *testing.T) {
	f := newInventory(t)
	root := filepath.Join(f.claude, "plugins", "cache", "market", "single", "1")
	f.write("claude/settings.json", `{"enabledPlugins":{"single@market":true}}`)
	f.write("claude/plugins/cache/market/single/1/SKILL.md", skillText("single", "Single-skill plugin root."))
	data, err := json.Marshal(map[string]any{
		"version": 1, "plugins": map[string]any{"single@market": installRecord("user", root, "")},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.write("claude/plugins/installed_plugins.json", string(data))
	catalog := f.discover("claude")
	entry := findSkill(t, catalog, "single")
	if entry.ID != "claude:plugin:single@market:." || entry.Disabled {
		t.Fatalf("legacy single-skill plugin: %+v", entry)
	}
}

func (f inventoryFixture) installs(plugins map[string]any) {
	f.t.Helper()
	data, err := json.Marshal(map[string]any{"version": 2, "plugins": plugins})
	if err != nil {
		f.t.Fatal(err)
	}
	f.write("claude/plugins/installed_plugins.json", string(data))
}

func installRecord(scope, path, project string) map[string]string {
	return map[string]string{"scope": scope, "installPath": path, "projectPath": project}
}
