package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

// Every check Doctor reports has a declared class, so none is classed by the
// required fallback by accident. Oracle: each check name of a Go project's
// report, and git_hooks, which cw doctor adds, is a key of checkClasses.
func TestDoctorDeclaresAClassForEveryCheck(t *testing.T) {
	root := gitFixture(t)
	report, err := Doctor(t.Context(), DoctorOptions{Repo: root, Home: t.TempDir(), LookPath: func(name string) (string, error) { return "/fake/" + name, nil }, Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	names := []string{"git_hooks"}
	for name := range report.Checks {
		names = append(names, name)
	}
	for _, name := range names {
		if _, ok := checkClasses[name]; !ok {
			t.Errorf("check %s has no declared class", name)
		}
	}
	if CheckClass("an_undeclared_check") != ClassRequired {
		t.Error("an undeclared check is not required")
	}
}

// The workflow skills of a plugin install are found in the host's plugin
// cache, and git and cw are required tools looked up on PATH. Oracle: the
// three SKILL.md files under <cache>/<marketplace>/agent-workflows/<version>/
// skills make workflow_skills configured, under Claude Code's and Codex's
// cache alike; the same files under another plugin's name leave it missing;
// git_cli and cw_cli follow the lookup and are required.
func TestDoctorFindsWorkflowSkillsInAPluginCacheAndRequiresGitAndCw(t *testing.T) {
	for _, test := range []struct {
		name, cache, plugin, skills string
		found                       bool
	}{
		{name: "claude code plugin", cache: filepath.Join(".claude", "plugins", "cache"), plugin: "agent-workflows", skills: "configured", found: true},
		{name: "codex plugin", cache: filepath.Join(".codex", "plugins", "cache"), plugin: "agent-workflows", skills: "configured"},
		{name: "another plugin", cache: filepath.Join(".claude", "plugins", "cache"), plugin: "other-plugin", skills: "missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			for _, skill := range []string{"task-orchestration", "production-plan", "code-review"} {
				writeFile(t, home, filepath.Join(test.cache, "agent-workflows", test.plugin, "3.0.0", "skills", skill, "SKILL.md"), "fixture")
			}
			lookPath := func(name string) (string, error) {
				if test.found {
					return "/fake/" + name, nil
				}
				return "", os.ErrNotExist
			}
			report, err := Doctor(t.Context(), DoctorOptions{Repo: root, Home: home, LookPath: lookPath, Getenv: func(string) string { return "" }})
			if err != nil {
				t.Fatal(err)
			}
			if got := report.Checks["workflow_skills"].Status; got != test.skills {
				t.Errorf("workflow_skills = %q, want %q", got, test.skills)
			}
			tool := "missing"
			if test.found {
				tool = "detected"
			}
			for _, name := range []string{"git_cli", "cw_cli"} {
				if got := report.Checks[name].Status; got != tool || CheckClass(name) != ClassRequired {
					t.Errorf("%s = %q (class %s), want %q and required", name, got, CheckClass(name), tool)
				}
			}
		})
	}
}
