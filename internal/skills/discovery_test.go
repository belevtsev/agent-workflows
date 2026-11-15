package skills_test

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func TestSkillRepositoryAndPersonalDiscovery(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.agents/skills/review/SKILL.md", skillText("review", "Repository review."))
	f.write("repo/module/.agents/skills/module-skill/SKILL.md", skillText("module-skill", "Nested module."))
	f.write("home/.agents/skills/review/SKILL.md", skillText("review", "Personal review."))
	f.write("codex/skills/legacy/SKILL.md", skillText("legacy", "Legacy Codex source."))
	f.write(".agents/skills/outside/SKILL.md", skillText("outside", "Above the repository."))
	f.write("repo/.claude/skills/other-host/SKILL.md", skillText("other-host", "Claude only."))
	f.t.Chdir(filepath.Join(f.repo, "module"))
	catalog := f.discover("codex")
	ids := skillIDs(catalog)
	want := []string{
		"codex:legacy:legacy", "codex:repo:.agents/skills/review",
		"codex:repo:module/.agents/skills/module-skill", "codex:user:review",
	}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("discovered %v, want %v (warnings %v)", ids, want, catalog.Warnings)
	}
	if _, err := catalog.Resolve([]string{"review"}); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("duplicate short name silently selected: %v", err)
	}
	resolved, err := catalog.Resolve([]string{"codex:user:review"})
	if err != nil || len(resolved) != 1 || resolved[0].Scope != "user" {
		t.Fatalf("qualified personal reference: %+v, %v", resolved, err)
	}
	if again := f.discover("codex"); !reflect.DeepEqual(catalog, again) {
		t.Fatalf("discovery order changed: %+v vs %+v", catalog, again)
	}
	personal, err := skills.Discover("", "codex")
	if err != nil || len(personal.Skills) != 2 {
		t.Fatalf("outside-repository inventory: %+v, %v", personal, err)
	}
}

func TestSkillSymlinkedCollectionsAndCycles(t *testing.T) {
	f := newInventory(t)
	f.write("installed-package/skills/tool/SKILL.md", skillText("tool", "Symlinked installed package."))
	collection := filepath.Join(f.home, ".agents", "skills")
	if err := os.MkdirAll(collection, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(collection, "installed-package")
	if err := os.Symlink(filepath.Join(f.base, "installed-package"), link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	if err := os.Symlink(collection, filepath.Join(f.base, "installed-package", "cycle")); err != nil {
		t.Fatal(err)
	}
	catalog := f.discover("codex")
	if len(catalog.Skills) != 1 {
		t.Fatalf("cycle or symlink duplicate inventory: %+v", catalog)
	}
	entry := catalog.Skills[0]
	wantPath := filepath.Join(link, "skills", "tool", "SKILL.md")
	if entry.Path != wantPath || entry.ID != "codex:user:installed-package/skills/tool" {
		t.Fatalf("original symlink directory lost: %+v", entry)
	}
	resolved, err := catalog.Resolve([]string{filepath.Join(f.base, "installed-package", "skills", "tool")})
	if err != nil || len(resolved) != 1 || resolved[0].ID != entry.ID {
		t.Fatalf("real source did not match discovered identity: %+v, %v", resolved, err)
	}
}

func TestSkillHostDisabledPathsCannotBypassPolicy(t *testing.T) {
	f := newInventory(t)
	path := f.write("home/.agents/skills/disabled/SKILL.md", skillText("disabled", "Disabled by host settings."))
	f.write("codex/config.toml", fmt.Sprintf("[[skills.config]]\npath = %q\nenabled = false\n", path))
	catalog := f.discover("codex")
	entry := findSkill(t, catalog, "disabled")
	if !entry.Disabled || !strings.Contains(entry.Reason, "skills.config") {
		t.Fatalf("host disable state lost: %+v", entry)
	}
	for _, ref := range []string{entry.ID, "disabled", path, filepath.Dir(path)} {
		if _, err := catalog.Resolve([]string{ref}); err == nil {
			t.Errorf("disabled skill selected through %q", ref)
		}
	}
	alias := filepath.Join(f.base, "alias")
	if err := os.Symlink(filepath.Dir(path), alias); err == nil {
		if _, err := catalog.Resolve([]string{alias}); err == nil {
			t.Fatal("disabled path bypassed through symlink")
		}
	}
	f.write("repo/.codex/config.toml", fmt.Sprintf("[[skills.config]]\npath = %q\nenabled = true\n", path))
	if _, err := f.discover("codex").Resolve([]string{path}); err != nil {
		t.Fatalf("project setting did not override personal setting: %v", err)
	}
	f.write("repo/.codex/config.toml", "[[skills.config]]\npath = '.agents/skills/local'\nenabled = false\n")
	local := f.write("repo/.agents/skills/local/SKILL.md", skillText("local", "Directory-selector compatibility."))
	if _, err := f.discover("codex").Resolve([]string{local}); err == nil {
		t.Fatal("relative skill-directory disable ignored")
	}
}

func TestSkillCodexNameRulesAndBundledPolicy(t *testing.T) {
	f := newInventory(t)
	repoPath := f.write("repo/.agents/skills/shared/SKILL.md", skillText("shared", "Repository source."))
	userPath := f.write("home/.agents/skills/shared/SKILL.md", skillText("shared", "Personal source."))
	bundledPath := f.write("codex/skills/.system/bundled/SKILL.md", skillText("bundled", "Bundled source."))
	f.write("codex/config.toml", fmt.Sprintf("[[skills.config]]\nname = \"shared\"\nenabled = false\n[[skills.config]]\npath = %q\nenabled = true\n[skills.bundled]\nenabled = false\n", repoPath))
	catalog := f.discover("codex")
	if _, err := catalog.Resolve([]string{repoPath}); err != nil {
		t.Fatalf("last matching path rule did not enable repository source: %v", err)
	}
	for _, ref := range []string{userPath, bundledPath} {
		if _, err := catalog.Resolve([]string{ref}); err == nil {
			t.Fatalf("disabled name/bundled source selected through %q", ref)
		}
	}
	f.write("repo/.codex/config.toml", fmt.Sprintf("[[skills.config]]\nname = \"shared\"\nenabled = true\n[[skills.config]]\npath = %q\nenabled = false\n[skills.bundled]\nenabled = true\n", repoPath))
	catalog = f.discover("codex")
	if _, err := catalog.Resolve([]string{userPath, bundledPath}); err != nil {
		t.Fatalf("project name/bundled overrides ignored: %v", err)
	}
	if _, err := catalog.Resolve([]string{repoPath}); err == nil {
		t.Fatal("last matching repository path rule ignored")
	}
}

func TestSkillCodexInvocationSidecarPolicy(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.agents/skills/controlled/SKILL.md", skillText("controlled", "Policy sidecar."))
	f.write("repo/.agents/skills/controlled/agents/openai.yaml", "interface:\n  display_name: Controlled\npolicy:\n  allow_implicit_invocation: false\n")
	catalog := f.discover("codex")
	entry := findSkill(t, catalog, "controlled")
	if !entry.ManualOnly || entry.Disabled {
		t.Fatalf("manual Codex policy = %+v", entry)
	}
	if _, err := catalog.Resolve([]string{entry.ID}); err == nil {
		t.Fatal("Codex manual-only source preloaded")
	}
	f.write("repo/.agents/skills/controlled/agents/openai.yaml", "policy:\n  allow_implicit_invocation: true\n")
	if _, err := catalog.Resolve([]string{entry.ID}); err != nil {
		t.Fatalf("current Codex invocation policy not re-read: %v", err)
	}
	f.write("repo/.agents/skills/controlled/agents/openai.yaml", "policy: [broken\n")
	catalog = f.discover("codex")
	if !findSkill(t, catalog, "controlled").Disabled || len(catalog.Warnings) == 0 {
		t.Fatalf("malformed sidecar policy silently accepted: %+v", catalog)
	}
	if _, err := catalog.Resolve([]string{entry.Path}); err == nil {
		t.Fatal("malformed policy bypassed by source path")
	}
}

func TestSkillClaudeVisibilitySettings(t *testing.T) {
	f := newInventory(t)
	for _, name := range []string{"off", "manual", "name-only", "background"} {
		f.write("claude/skills/"+name+"/SKILL.md", skillText(name, "Visibility test."))
	}
	f.write("claude/settings.json", `{"skillOverrides":{"off":"off","manual":"user-invocable-only","name-only":"name-only"}}`)
	catalog := f.discover("claude")
	if !findSkill(t, catalog, "off").Disabled || !findSkill(t, catalog, "manual").ManualOnly {
		t.Fatalf("Claude visibility settings ignored: %+v", catalog)
	}
	if _, err := catalog.Resolve([]string{"off"}); err == nil {
		t.Fatal("hidden skill selected")
	}
	if _, err := catalog.Resolve([]string{"manual"}); err == nil {
		t.Fatal("user-only skill preloaded")
	}
	if _, err := catalog.Resolve([]string{"name-only", "background"}); err != nil {
		t.Fatal(err)
	}
	f.write("repo/.claude/settings.local.json", `{"skillOverrides":{"off":"on"}}`)
	if _, err := f.discover("claude").Resolve([]string{"off"}); err != nil {
		t.Fatalf("local visibility override ignored: %v", err)
	}
}

func TestSkillMalformedSettingsFailSelectionWithoutLeakingContents(t *testing.T) {
	for _, host := range []string{"codex", "claude"} {
		t.Run(host, func(t *testing.T) {
			f := newInventory(t)
			path := f.write("outside/SKILL.md", skillText("outside", "Explicit source."))
			if host == "codex" {
				f.write("codex/config.toml", "secret-token-do-not-log = [broken")
			} else {
				f.write("claude/settings.json", `{"secret-token-do-not-log":broken}`)
			}
			catalog := f.discover(host)
			if len(catalog.Warnings) == 0 || strings.Contains(strings.Join(catalog.Warnings, " "), "secret-token-do-not-log") {
				t.Fatalf("missing or unsafe diagnostic: %+v", catalog)
			}
			if _, err := catalog.Resolve([]string{path}); err == nil {
				t.Fatal("unknown enablement accepted after malformed settings")
			}
		})
	}
}

func TestSkillExactIDPrecedesConflictingShortName(t *testing.T) {
	f := newInventory(t)
	f.write("repo/.agents/skills/target/SKILL.md", skillText("normal", "Canonical target."))
	f.write("home/.agents/skills/conflict/SKILL.md", skillText("codex:repo:.agents/skills/target", "Conflicting name."))
	catalog := f.discover("codex")
	resolved, err := catalog.Resolve([]string{"codex:repo:.agents/skills/target"})
	if err != nil || len(resolved) != 1 || resolved[0].Name != "normal" {
		t.Fatalf("short name overrode canonical ID: %+v, %v", resolved, err)
	}
}

func skillIDs(catalog skills.Catalog) []string {
	ids := make([]string, 0, len(catalog.Skills))
	for _, entry := range catalog.Skills {
		ids = append(ids, entry.ID)
	}
	if !slices.IsSorted(ids) {
		panic("catalog is not sorted")
	}
	return ids
}
