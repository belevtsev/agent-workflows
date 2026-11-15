package roles

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadSkillPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	personal, err := PersonalPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(personal), 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(path, data string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(personal, "version = 1\n[codex.developer]\nskills = ['personal']\n[codex.reviewer]\nskills = ['review']\n[codex.architect]\nskills = ['personal-design']\n[codex.coordinator]\nskills = ['coord']\n")
	root := t.TempDir()
	const repository = "version = 1\n[paths]\ntests = ['tests/**']\n[codex.developer]\nmodel = 'custom'\n[codex.reviewer]\nskills = []\n[codex.architect]\nskills = ['repository-design']\n"
	write(filepath.Join(root, FileName), repository)
	config, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	for role, want := range map[string][]string{
		"developer": {"personal"}, "reviewer": {}, "architect": {"repository-design"}, "coordinator": {"coord"},
	} {
		if got := config.Skills["codex"][role]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s skills = %#v, want %#v", role, got, want)
		}
	}
	if config.Models["codex"]["developer"].Model != "custom" || !reflect.DeepEqual(config.Tests, []string{"tests/**"}) {
		t.Fatalf("repository model or test scope changed: %#v", config)
	}
	parsed, err := Parse([]byte(repository))
	if err != nil {
		t.Fatal(err)
	}
	if _, inherited := parsed.Skills["codex"]["developer"]; inherited {
		t.Fatal("repository-only Parse inherited personal selections")
	}
	if err := os.Remove(personal); err != nil {
		t.Fatal(err)
	}
	defaults, err := Load(t.TempDir())
	if err != nil || len(defaults.Skills) != 0 || len(defaults.Models) != 0 || defaults.Tests != nil {
		t.Fatalf("defaults should not pin skills or change policy: %#v, %v", defaults, err)
	}
}

func TestParseSkillSelectionsStrict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, text string
		personal   bool
	}{
		{name: "scalar", text: "[codex.developer]\nskills = 'skill'"},
		{name: "wrong item type", text: "[codex.developer]\nskills = [1]"},
		{name: "duplicate", text: "[codex.developer]\nskills = ['same', 'same']"},
		{name: "empty", text: "[codex.developer]\nskills = ['']"},
		{name: "whitespace", text: "[codex.developer]\nskills = ['  ']"},
		{name: "control", text: "[codex.developer]\nskills = [\"bad\\nname\"]"},
		{name: "unknown host", text: "[other.developer]\nskills = []"},
		{name: "unknown role", text: "[codex.other]\nskills = []"},
		{name: "coordinator model", text: "[codex.coordinator]\nmodel = 'custom'"},
		{name: "coordinator effort", text: "[codex.coordinator]\neffort = 'high'"},
		{name: "personal models", text: "[codex.developer]\nmodel = 'custom'", personal: true},
		{name: "personal effort", text: "[codex.developer]\neffort = 'high'", personal: true},
		{name: "personal test paths", text: "[paths]\ntests = []", personal: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseConfig([]byte("version = 1\n"+test.text+"\n"), test.personal); err == nil {
				t.Fatal("invalid skill configuration accepted")
			}
		})
	}
	for _, parse := range []func([]byte) (*Config, error){Parse, ParsePersonal} {
		if cfg, err := parse([]byte("version = 1\n[codex.coordinator]\nskills = []\n")); err != nil || cfg.Skills["codex"]["coordinator"] == nil {
			t.Fatalf("explicit empty coordinator list lost: %#v, %v", cfg, err)
		}
	}
}

func TestEditSkillSelectionPreservesFields(t *testing.T) {
	t.Parallel()
	for _, section := range []string{
		"[codex.developer]\nmodel = 'custom'\neffort = 'max'\nskills = ['old']\n",
		"[codex]\ndeveloper.model = 'custom'\ndeveloper.effort = 'max'\ndeveloper.skills = ['old']\n",
		"codex = {developer = {model = 'custom', effort = 'max', skills = ['old']}}\n",
	} {
		t.Run(section, func(t *testing.T) {
			data := []byte("# role choices\nversion = 1\n" + section + "\n[paths]\ntests = ['tests/**'] # preserve\n")
			updated, err := EditSelection(data, "codex", "developer", Model{}, []string{"new", "second"}, false, false)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err := Parse(updated)
			if err != nil || cfg.Models["codex"]["developer"] != (Model{Model: "custom", Effort: "max"}) || !reflect.DeepEqual(cfg.Skills["codex"]["developer"], []string{"new", "second"}) {
				t.Fatalf("setting skills changed models: %s; %v", updated, err)
			}
			updated, err = Edit(updated, "codex", "developer", Model{Model: "next"}, false)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = Parse(updated)
			if err != nil || !reflect.DeepEqual(cfg.Skills["codex"]["developer"], []string{"new", "second"}) || cfg.Models["codex"]["developer"].Effort != "max" {
				t.Fatalf("model-only edit changed skills/effort: %s; %v", updated, err)
			}
			if !strings.HasPrefix(string(updated), "# role choices\n") || !strings.Contains(string(updated), "tests = ['tests/**'] # preserve") {
				t.Fatalf("lost unrelated comments: %s", updated)
			}
			updated, err = Edit(updated, "codex", "developer", Model{}, true)
			if err != nil {
				t.Fatal(err)
			}
			cfg, err = Parse(updated)
			_, hasSkills := cfg.Skills["codex"]["developer"]
			if err != nil || hasSkills || cfg.Models["codex"]["developer"] != (Model{}) {
				t.Fatalf("reset kept overrides: %s; %v", updated, err)
			}
		})
	}
}
