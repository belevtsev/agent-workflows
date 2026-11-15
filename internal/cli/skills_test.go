package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/roles"
	"github.com/belevtsev/kitsu/v3/internal/skills"
)

func skillRepo(t *testing.T) (*kitsutest.Repo, string) {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	repo.Setenv("CODEX_HOME", filepath.Join(repo.Home, ".codex"))
	repo.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(repo.Home, ".claude"))
	configDir := filepath.Join(repo.Home, "config")
	repo.Setenv("XDG_CONFIG_HOME", configDir)
	repo.Setenv("APPDATA", configDir)
	if runtime.GOOS == "darwin" {
		configDir = filepath.Join(repo.Home, "Library", "Application Support")
	}
	return repo, filepath.Join(configDir, "kitsu", "roles.toml")
}

func writeSkillFixture(t *testing.T, path, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: "+name+"\ndescription: Review local code\n---\nRead local files.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func setSkills(t *testing.T, repo *kitsutest.Repo, role, refs string, extra ...string) {
	t.Helper()
	args := append([]string{"roles", "set", role, "--host", "codex", "--skills", refs}, extra...)
	if result := repo.Kitsu(args...); result.Code != 0 {
		t.Fatalf("%v: %s", args, result.Output())
	}
}

func readRoleConfig(t *testing.T, repo *kitsutest.Repo) *roles.Config {
	t.Helper()
	cfg, err := roles.Parse([]byte(repo.Read(roles.FileName)))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSkillCLISelectionsPreserveModelsAndScope(t *testing.T) {
	t.Parallel()
	repo, personal := skillRepo(t)
	writeSkillFixture(t, repo.Path(".agents/skills/local/SKILL.md"), "local")
	writeSkillFixture(t, filepath.Join(repo.Home, ".agents", "skills", "personal", "SKILL.md"), "personal")
	repo.Write(roles.FileName, "# Keep my scope\nversion = 1\n[paths]\ntests = ['tests/**'] # keep\n[codex.developer]\nmodel = 'custom'\neffort = 'max'\n")
	setSkills(t, repo, "developer", "personal", "--global")
	if _, err := os.Stat(personal); err != nil {
		t.Fatal(err)
	}
	setSkills(t, repo, "developer", "local")
	config := readRoleConfig(t, repo)
	if config.Models["codex"]["developer"] != (roles.Model{Model: "custom", Effort: "max"}) || len(config.Skills["codex"]["developer"]) != 1 {
		t.Fatalf("skill edit changed models: %#v", config)
	}
	beforeSkills := config.Skills["codex"]["developer"]
	if result := repo.Kitsu("roles", "set", "developer", "--host", "codex", "--reasoning", "high"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	config = readRoleConfig(t, repo)
	if !reflect.DeepEqual(config.Skills["codex"]["developer"], beforeSkills) || config.Models["codex"]["developer"].Model != "custom" {
		t.Fatal("model/reasoning edit lost skills or another model field")
	}
	setSkills(t, repo, "developer", "")
	config = readRoleConfig(t, repo)
	if refs, exists := config.Skills["codex"]["developer"]; !exists || refs == nil || len(refs) != 0 {
		t.Fatalf("explicit clear lost presence: %#v", config.Skills)
	}
	if result := repo.Kitsu("roles", "reset", "developer", "--host", "codex", "--json"); result.Code != 0 || !strings.Contains(result.Stdout, "codex:user:personal") {
		t.Fatalf("reset did not restore personal inheritance: %s", result.Output())
	}
	config = readRoleConfig(t, repo)
	if _, exists := config.Skills["codex"]["developer"]; exists || config.Models["codex"]["developer"] != (roles.Model{}) {
		t.Fatal("reset did not remove all repository overrides")
	}
	if !strings.Contains(repo.Read(roles.FileName), "tests = ['tests/**'] # keep") {
		t.Fatal("editing skills lost test policy or comments")
	}
	setSkills(t, repo, "coordinator", "personal", "--global")
	if result := repo.Kitsu("roles", "reset", "coordinator", "--host", "codex", "--global"); result.Code != 0 {
		t.Fatal(result.Output())
	}
	data, err := os.ReadFile(personal)
	if err != nil {
		t.Fatal(err)
	}
	global, err := roles.ParsePersonal(data)
	if err != nil || len(global.Skills["codex"]["coordinator"]) != 0 || len(global.Skills["codex"]["developer"]) != 1 {
		t.Fatalf("global reset changed another role: %s; %v", data, err)
	}
}

func TestSkillCLIErrorsLeaveRoleFilesUnchanged(t *testing.T) {
	t.Parallel()
	repo, personal := skillRepo(t)
	writeSkillFixture(t, repo.Path(".agents/skills/valid/SKILL.md"), "valid")
	writeSkillFixture(t, repo.Path(".agents/skills/duplicate-a/SKILL.md"), "duplicate")
	writeSkillFixture(t, repo.Path(".agents/skills/duplicate-b/SKILL.md"), "duplicate")
	writeSkillFixture(t, repo.Path(".agents/skills/manual/SKILL.md"), "manual")
	repo.Write(".agents/skills/manual/SKILL.md", "---\nname: manual\ndescription: Manual task\ndisable-model-invocation: true\n---\n")
	repo.Write(".codex/config.toml", "[[skills.config]]\npath = '.agents/skills/valid'\nenabled = false\n")
	repo.Write(roles.FileName, "version = 1\n[codex.developer]\nmodel = 'keep'\neffort = 'high'\n")
	before := repo.Read(roles.FileName)
	tests := []struct {
		name string
		args []string
		code int
	}{
		{name: "missing selection with model", args: []string{"--model", "changed", "--skills", "missing"}, code: 1},
		{name: "ambiguous name", args: []string{"--skills", "duplicate"}, code: 1},
		{name: "disabled", args: []string{"--skills", "valid"}, code: 1},
		{name: "manual only", args: []string{"--skills", "manual"}, code: 1},
		{name: "duplicate reference", args: []string{"--skills", "codex:repo:.agents/skills/duplicate-a,codex:repo:.agents/skills/duplicate-a"}, code: 1},
		{name: "empty scalar", args: []string{"--reasoning", "", "--skills", ""}, code: 2},
		{name: "global model", args: []string{"--global", "--model", "changed", "--skills", ""}, code: 2},
		{name: "root without skills", args: []string{"--root", repo.Dir, "--model", "changed"}, code: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"roles", "set", "developer", "--host", "codex"}, test.args...)
			if result := repo.Kitsu(args...); result.Code != test.code {
				t.Fatalf("exit %d, want %d: %s", result.Code, test.code, result.Output())
			}
			if repo.Read(roles.FileName) != before {
				t.Fatal("invalid selection partially changed role configuration")
			}
			if _, err := os.Stat(personal); !os.IsNotExist(err) {
				t.Fatalf("invalid edit created personal configuration: %v", err)
			}
		})
	}
}

func TestSkillCLIResolveIsReadOnlyAndRecordsSelections(t *testing.T) {
	t.Parallel()
	repo, _ := skillRepo(t)
	custom := filepath.Join(t.TempDir(), "custom", "SKILL.md")
	writeSkillFixture(t, custom, "custom")
	setSkills(t, repo, "coordinator", "custom", "--root", filepath.Dir(custom))
	if refs := readRoleConfig(t, repo).Skills["codex"]["coordinator"]; !reflect.DeepEqual(refs, []string{custom}) {
		t.Fatalf("explicit-root choice is not reusable without --root: %q", refs)
	}
	var resolved struct {
		Host, Role   string
		SelectedRefs []string       `json:"selected_refs"`
		Skills       []skills.Entry `json:"skills"`
		SelectionID  string         `json:"selection_id"`
		LoadStatus   string         `json:"load_status"`
	}
	result := repo.Kitsu("skills", "resolve", "coordinator", "--host", "codex", "--json")
	if err := json.Unmarshal([]byte(result.Stdout), &resolved); result.Code != 0 || err != nil {
		t.Fatalf("resolve: %s; %v", result.Output(), err)
	}
	if resolved.Host != "codex" || resolved.Role != "coordinator" || resolved.LoadStatus != "unverified" || resolved.SelectionID != "" || len(resolved.Skills) != 1 || resolved.Skills[0].Path != custom || len(resolved.Skills[0].SHA256) != 64 {
		t.Fatalf("incomplete or misleading resolution: %+v", resolved)
	}
	if repo.Exists("openspec") {
		t.Fatal("read-only resolution created task records")
	}
	for _, workflow := range []string{"small", "full"} {
		t.Run(workflow, func(t *testing.T) {
			if workflow == "full" {
				repo.Write("openspec/changes/skill-task/proposal.md", "# Proposal\n")
			}
			result := repo.Kitsu("skills", "resolve", "coordinator", "--host", "codex", "--task", "skill-task", "--workflow", workflow, "--json")
			if err := json.Unmarshal([]byte(result.Stdout), &resolved); result.Code != 0 || err != nil || resolved.SelectionID == "" {
				t.Fatalf("task resolve: %s; %v", result.Output(), err)
			}
			dir := "openspec/changes/.skills/skill-task/"
			if workflow == "full" {
				dir = "openspec/changes/skill-task/skills/"
			}
			data := repo.Read(dir + resolved.SelectionID + ".json")
			if strings.Contains(data, custom) || !strings.Contains(data, `"load_status": "unverified"`) {
				t.Fatal("receipt exposed a local path or claimed verified loading")
			}
		})
	}
	if result := repo.Kitsu("skills", "resolve", "coordinator", "--host", "codex", "--workflow", "full"); result.Code != 2 {
		t.Fatalf("accepted workflow without task: %s", result.Output())
	}
	if result := repo.Kitsu("roles", "set", "coordinator", "--host", "codex", "--reasoning", "max"); result.Code != 2 {
		t.Fatalf("accepted coordinator reasoning: %s", result.Output())
	}
}

func TestSkillCLIInventoryAndRoleDisplays(t *testing.T) {
	t.Parallel()
	repo, _ := skillRepo(t)
	writeSkillFixture(t, repo.Path(".agents/skills/local/SKILL.md"), "local")
	writeSkillFixture(t, filepath.Join(repo.Home, ".agents", "skills", "personal", "SKILL.md"), "personal")
	var catalog struct {
		Host   string         `json:"host"`
		Skills []skills.Entry `json:"skills"`
	}
	outside := repo.In(repo.Home)
	result := outside.Kitsu("skills", "list", "--host", "codex", "--json")
	if err := json.Unmarshal([]byte(result.Stdout), &catalog); result.Code != 0 || err != nil || len(catalog.Skills) != 1 || catalog.Skills[0].Name != "personal" {
		t.Fatalf("personal inventory outside git: %s; %v", result.Output(), err)
	}
	setSkills(t, outside, "developer", "personal", "--global")
	setSkills(t, repo, "developer", "local")
	setSkills(t, repo, "coordinator", "local")
	for _, args := range [][]string{{"roles", "--json"}, {"roles", "--host", "codex", "--json"}} {
		result := repo.Kitsu(args...)
		if result.Code != 0 || !strings.Contains(result.Stdout, `"skills"`) || !strings.Contains(result.Stdout, "codex:repo:.agents/skills/local") {
			t.Fatalf("role JSON lost selections: %s", result.Output())
		}
	}
	// Listing remains available when a selected skill disappears.
	repo.Remove(".agents/skills/local")
	longRef := "codex:plugin:" + strings.Repeat("long-plugin-name-", 15)
	repo.Write(roles.FileName, "version = 1\n[codex.developer]\nmodel = '"+strings.Repeat("model", 50)+"'\nskills = ['"+longRef+"']\n")
	for _, args := range [][]string{{"roles"}, {"roles", "--host", "codex"}} {
		result := repo.Kitsu(args...)
		if result.Code != 0 || !strings.Contains(result.Stdout, "Skills") {
			t.Fatalf("plain listing validated installed skill: %s", result.Output())
		}
		for _, line := range strings.Split(result.Stdout, "\n") {
			if utf8.RuneCountInString(line) > 120 {
				t.Fatalf("role line exceeds terminal: %s", line)
			}
		}
	}
	if result := repo.Kitsu("skills", "resolve", "developer", "--host", "codex"); result.Code != 1 {
		t.Fatalf("preflight accepted missing selection: %s", result.Output())
	}
}
