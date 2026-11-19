package roles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const workflowRoles = `version = 2

[paths]
tests = ['tests/**'] # preserve

[roles.lookup]
host = 'codex'

[roles.spec-writer]
host = 'claude'

[roles.architect]
host = 'codex'

[roles.quality-engineer]
host = 'claude'

[roles.developer]
host = 'claude'
model = 'custom-developer'
effort = 'high'
skills = []

[roles.reviewer]
host = 'codex'
`

func TestWorkflowRoleConfig(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, data, diagnostic string
	}{
		{name: "missing role", data: "version = 2\n", diagnostic: "[roles.lookup]"},
		{name: "unknown host", data: strings.Replace(workflowRoles, "'codex'", "'other'", 1), diagnostic: "`host` under [roles.lookup]"},
		{name: "legacy table", data: workflowRoles + "[codex.developer]\nmodel = 'old'\n", diagnostic: "unknown key `codex`"},
		{name: "coordinator", data: workflowRoles + "[roles.coordinator]\nhost = 'codex'\n", diagnostic: "unknown specialist `coordinator`"},
		{name: "unknown setting", data: workflowRoles + "fallback = 'claude'\n", diagnostic: "unknown key `fallback` under [roles.reviewer]"},
		{name: "optional git host", data: workflowRoles + "[roles.git]\nmodel = 'custom'\n", diagnostic: "`host` under [roles.git]"},
		{name: "duplicate role", data: workflowRoles + "[roles.lookup]\nhost = 'claude'\n", diagnostic: "not valid TOML"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Parse([]byte(test.data)); err == nil || !strings.Contains(err.Error(), test.diagnostic) {
				t.Fatalf("got %v, want diagnostic %q", err, test.diagnostic)
			}
		})
	}
	if _, err := ParsePersonal([]byte(workflowRoles)); err == nil {
		t.Fatal("personal file accepted repository-only configuration")
	}
	config, err := Parse([]byte(workflowRoles))
	if err != nil || config.Version != WorkflowConfigVersion || config.ConfiguredHost("developer") != "claude" || config.ConfiguredHost("git") != "" {
		t.Fatalf("unexpected role configuration: %#v, %v", config, err)
	}
}

func TestResolveWorkflow(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA"} {
		t.Setenv(name, home)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	write := func(path, contents string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	personal, err := PersonalPath()
	if err != nil {
		t.Fatal(err)
	}
	write(personal, `version = 1
[codex.reviewer]
skills = ['review-guidance']
[claude.reviewer]
skills = ['missing-inactive-selection']
[claude.developer]
skills = ['missing-suppressed-selection']
`)
	const skill = "---\nname: review-guidance\ndescription: Review this fixture.\n---\nRead the exact candidate.\n"
	skillPath := filepath.Join(root, ".agents", "skills", "review-guidance", "SKILL.md")
	write(skillPath, skill)
	write(filepath.Join(root, FileName), workflowRoles)
	settings, err := ResolveWorkflow(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(settings) != 6 || settings["developer"].Host != "claude" || settings["developer"].Model != (Model{Model: "custom-developer", Effort: "high"}) || len(settings["developer"].Skills) != 0 {
		t.Fatalf("unexpected configured workflow: %+v", settings)
	}
	review := settings["reviewer"]
	if review.Host != "codex" || review.Model.Model == "" || review.Model.Effort != "" || len(review.Skills) != 1 || review.Skills[0].Path != skillPath {
		t.Fatalf("reviewer did not inherit only the assigned host's skill and effort default: %+v", review)
	}
	if value, err := ResolveSpecialist(root, "", "reviewer"); err != nil || value.Host != "codex" || value.Model != review.Model {
		t.Fatalf("implicit reviewer host = %+v, %v", value, err)
	}
	if _, err := ResolveSpecialist(root, "claude", "reviewer"); err == nil || !strings.Contains(err.Error(), "assigned to codex") {
		t.Fatalf("wrong reviewer host was not rejected: %v", err)
	}
	write(filepath.Join(root, FileName), "version = 1\n[claude.developer]\nmodel = 'legacy'\nskills = []\n")
	if _, err := ResolveWorkflow(root); err == nil {
		t.Fatal("legacy configuration entered the configured workflow")
	}
	if value, err := ResolveSpecialist(root, "claude", "developer"); err != nil || value.Model != (Model{Model: "legacy", Effort: "high"}) {
		t.Fatalf("legacy role settings = %+v, %v", value, err)
	}
}

func TestEditWorkflowRole(t *testing.T) {
	t.Parallel()
	data, err := EditSelection([]byte(workflowRoles), "codex", "developer", Model{Model: "new"}, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	config, err := Parse(data)
	if err != nil || config.ConfiguredHost("developer") != "codex" || config.Models["codex"]["developer"] != (Model{Model: "new"}) {
		t.Fatalf("host switch kept old provider overrides: %s, %v", data, err)
	}
	if _, exists := config.Skills["codex"]["developer"]; exists {
		t.Fatal("host switch kept the old explicit skill selection")
	}
	data, err = EditSelection(data, "", "developer", Model{}, nil, true, false)
	if err != nil {
		t.Fatal(err)
	}
	data, err = EditSelection(data, "claude", "git", Model{}, nil, false, false)
	if err != nil {
		t.Fatal(err)
	}
	config, err = Parse(data)
	if err != nil || config.ConfiguredHost("developer") != "codex" || config.Models["codex"]["developer"] != (Model{}) || config.ConfiguredHost("git") != "claude" {
		t.Fatalf("reset lost the assignment or git was not added: %s, %v", data, err)
	}
	if !strings.Contains(string(data), "tests = ['tests/**'] # preserve") {
		t.Fatalf("role edits changed unrelated policy: %s", data)
	}
}
