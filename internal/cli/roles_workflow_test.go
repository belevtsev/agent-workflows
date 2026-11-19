package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/roles"
)

const workflowRoleFixture = `version = 2
roles.lookup.host = 'codex'
roles.spec-writer.host = 'claude'
roles.architect.host = 'codex'
roles.quality-engineer.host = 'claude'
roles.reviewer.host = 'codex'

[roles.developer]
host = 'claude'
model = 'selected-developer'
effort = 'high'
skills = []
`

func TestInitializeRolesFromInstalledHost(t *testing.T) {
	cfg, checkout, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"config", "set", "host", "claude", "--global"},
		{"config", "set", "host", "codex"},
		{"role", "init", "--json"},
		{"role", "set", "architect", "--host", "codex", "--model", "gpt-6.1-sol", "--reasoning", "max"},
	} {
		if err := Execute(t.Context(), args, cfg); err != nil {
			t.Fatalf("kitsu %v: %v", args, err)
		}
	}
	settings, err := roles.ResolveWorkflow(root)
	if err != nil || len(settings) != 6 || settings["developer"].Host != "claude" || settings["developer"].Model.Effort != "" ||
		settings["architect"].Host != "codex" || settings["architect"].Model.Effort != "max" {
		t.Fatalf("settings: %+v, %v", settings, err)
	}
	path := filepath.Join(root, roles.FileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := Execute(t.Context(), []string{"role", "init", "--host", "codex"}, cfg); err == nil {
		t.Fatal("role init accepted an existing policy")
	}
	if after, err := os.ReadFile(path); err != nil || !bytes.Equal(before, after) {
		t.Fatalf("role init changed an existing policy: %v", err)
	}
	uxTestWrite(t, path, "version = 1\n")
	t.Setenv("KITSU_HOST", "codex")
	config, err := dailyRoleConfig(root)
	if err != nil || config.Version != 1 {
		t.Fatalf("installed host replaced version 1 policy: %v, %v", config, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	config, err = dailyRoleConfig(root)
	if err != nil || config.ConfiguredHost("architect") != "claude" {
		t.Fatalf("the daily workflow used the checkout host from %s: %v, %v", checkout, config, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("resolving defaults wrote a repository policy: %v", err)
	}
}

func TestConfiguredRoleCommands(t *testing.T) {
	config, _, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	uxTestWrite(t, filepath.Join(root, roles.FileName), workflowRoleFixture)
	uxTestWrite(t, filepath.Join(root, ".agents", "skills", "guidance", "SKILL.md"),
		"---\nname: guidance\ndescription: Guide implementation.\n---\nFollow the task assignment.\n")
	t.Setenv("KITSU_HOST", "invalid")

	call := func(args ...string) []byte {
		t.Helper()
		out := config.Out.(*bytes.Buffer)
		out.Reset()
		if err := Execute(t.Context(), args, config); err != nil {
			t.Fatalf("kitsu %v: %v", args, err)
		}
		return out.Bytes()
	}
	var list struct {
		Version int                  `json:"version"`
		Roles   []configuredRoleView `json:"roles"`
	}
	if err := json.Unmarshal(call("role", "list", "--json"), &list); err != nil || list.Version != 2 || len(list.Roles) != 6 {
		t.Fatalf("unexpected role route: %+v, %v", list, err)
	}
	if got := list.Roles[4]; got.Role != "developer" || got.Host != "claude" || got.Effort != "high" {
		t.Fatalf("developer assignment: %+v", got)
	}
	if err := Execute(t.Context(), []string{"role", "show", "developer", "--host", "codex"}, config); ExitCode(err) != ExitUsage {
		t.Fatalf("wrong explicit host was accepted: %v", err)
	}
	call("role", "set", "developer", "--host", "codex")
	call("role", "set", "developer", "--model", "chosen-without-host", "--reasoning", "high")
	call("role", "set", "developer", "--reasoning", "")
	call("skill", "attach", "guidance", "--role", "developer")
	var resolved struct {
		Host     string   `json:"host"`
		Selected []string `json:"selected_refs"`
	}
	if err := json.Unmarshal(call("skill", "resolve", "developer", "--json"), &resolved); err != nil || resolved.Host != "codex" || len(resolved.Selected) != 1 {
		t.Fatalf("configured skill resolution: %+v, %v", resolved, err)
	}
	var developer configuredRoleView
	if err := json.Unmarshal(call("role", "show", "developer", "--json"), &developer); err != nil || developer.Host != "codex" || developer.Model.Model != "chosen-without-host" || developer.Effort != "" {
		t.Fatalf("role overrides were not applied: %+v, %v", developer, err)
	}
	call("skill", "reset", "--role", "developer")
	call("role", "reset", "developer")
	call("role", "set", "git", "--host", "claude")
	settings, err := roles.ResolveWorkflow(root)
	if err != nil || settings["developer"].Host != "codex" || settings["developer"].Model.Model == "chosen-without-host" || len(settings["developer"].Skills) != 0 || settings["git"].Host != "claude" {
		t.Fatalf("reset or optional Git assignment failed: %+v, %v", settings, err)
	}
}

func TestConfiguredRoleEditorUsesAssignedHost(t *testing.T) {
	config, _, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	uxTestWrite(t, filepath.Join(root, roles.FileName), workflowRoleFixture)
	t.Setenv("KITSU_HOST", "invalid")
	config.Editor = func(_ context.Context, draft *UXEditor) error {
		if draft.Host != "claude" || draft.Role != "developer" || draft.Reasoning != "high" || !draft.NativeEffort {
			t.Fatalf("unexpected editor assignment: %+v", draft)
		}
		draft.Reasoning, draft.Save = "", true
		return nil
	}
	if err := Execute(t.Context(), []string{"role", "edit", "developer"}, config); err != nil {
		t.Fatal(err)
	}
	settings, err := roles.ResolveSpecialist(root, "", "developer")
	if err != nil || settings.Model.Effort != "" || settings.Model.Model != "selected-developer" {
		t.Fatalf("editor did not restore native effort: %+v, %v", settings, err)
	}
}
