package roles_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestSetAndResetRoleSelection(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	repo.Write("kitsu-roles.toml", rolesTOML+"\n[claude.developer]\nmodel = 'opus'\n")
	if err := os.MkdirAll(repo.Path("sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	commands := [][]string{
		{"set", "developer", "--host", "codex", "--model", "custom-model", "--reasoning", "max"},
		{"set", "developer", "--host", "codex", "--reasoning", "medium"},
		{"set", "--host", "codex", "developer", "--model", "second-model"},
	}
	for _, args := range commands {
		result := repo.In("sub").Kitsu(append([]string{"roles"}, args...)...)
		if result.Code != 0 || !strings.Contains(result.Stdout, "developer") || !strings.Contains(result.Stdout, "REASONING") {
			t.Fatalf("roles %v: %s", args, result.Output())
		}
	}
	selected := rolesJSON(t, repo)["developer"].Models
	if selected["codex"] != (hostModel{"second-model", "medium"}) || selected["claude"] != (hostModel{"opus", "high"}) {
		t.Fatalf("wrong selections: %+v", selected)
	}
	if !strings.Contains(repo.Read("kitsu-roles.toml"), rolesTOML) {
		t.Fatal("changed the test paths")
	}
	result := repo.Kitsu("roles", "reset", "developer", "--host", "codex", "--json")
	var selection struct{ Role, Host, Model, Effort string }
	if err := json.Unmarshal([]byte(result.Stdout), &selection); result.Code != 0 || err != nil {
		t.Fatalf("reset --json: %s; %v", result.Output(), err)
	}
	if selection.Role != "developer" || selection.Host != "codex" || selection.Model != "gpt-6.1-terra" || selection.Effort != "high" {
		t.Fatalf("reset did not show defaults: %+v", selection)
	}
	if rolesJSON(t, repo)["developer"].Models["claude"] != selected["claude"] {
		t.Fatal("reset changed the other host")
	}
	// A model-setting command cannot bypass the role's existing write scope.
	if got := repo.Kitsu("roles", "check", "developer", "--from", "HEAD"); got.Code != 5 {
		t.Fatalf("developer's model-policy edit was accepted: %s", got.Output())
	}
}

func TestRoleSettingsCreateAndValidate(t *testing.T) {
	t.Parallel()
	repo := kitsutest.Demo(t)
	if got := repo.Kitsu("roles", "reset", "architect", "--host", "codex"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	if _, err := os.Stat(repo.Path("kitsu-roles.toml")); !os.IsNotExist(err) {
		t.Fatalf("reset created a missing configuration: %v", err)
	}
	if got := repo.Kitsu("roles", "set", "architect", "--host", "claude", "--model", "sonnet", "--reasoning", "high", "--json"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	before := repo.Read("kitsu-roles.toml")
	for _, args := range [][]string{
		{"set", "developer", "--model", "m"}, {"set", "developer", "--host", "other", "--model", "m"},
		{"set", "unknown", "--host", "codex", "--model", "m"}, {"set", "coordinator", "--host", "codex", "--model", "m"},
		{"set", "developer", "--host", "codex"}, {"set", "developer", "--host", "codex", "--model="},
		{"set", "developer", "--host", "codex", "--model", "m", "--reasoning", "  "},
		{"reset", "developer", "--host", "codex", "--model", "m"}, {"reset", "developer", "--host", "codex", "--"},
	} {
		if got := repo.Kitsu(append([]string{"roles"}, args...)...); got.Code != 2 {
			t.Fatalf("roles %v: exit %d, want 2: %s", args, got.Code, got.Output())
		}
		if repo.Read("kitsu-roles.toml") != before {
			t.Fatalf("invalid command %v changed settings", args)
		}
	}
	repo.Write("kitsu-roles.toml", "version = 1\n[unknown]\n")
	before = repo.Read("kitsu-roles.toml")
	if got := repo.Kitsu("roles", "set", "developer", "--host", "codex", "--model", "m"); got.Code != 1 || repo.Read("kitsu-roles.toml") != before {
		t.Fatalf("invalid configuration was not preserved: %s", got.Output())
	}
}

func TestRoleSettingsPreserveModeAndRefuseLinks(t *testing.T) {
	t.Parallel()
	repo := demo(t)
	path := repo.Path("kitsu-roles.toml")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := repo.Kitsu("roles", "set", "git", "--host", "codex", "--reasoning", "medium"); got.Code != 0 {
		t.Fatal(got.Output())
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
		t.Fatalf("file permissions changed: %v; %v", info, err)
	}
	target := filepath.Join(t.TempDir(), "external.toml")
	if err := os.WriteFile(target, []byte(rolesTOML), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if got := repo.Kitsu("roles", "reset", "git", "--host", "codex"); got.Code != 1 {
		t.Fatalf("edited a link: %s", got.Output())
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != rolesTOML {
		t.Fatalf("link target changed: %q; %v", data, err)
	}
}
