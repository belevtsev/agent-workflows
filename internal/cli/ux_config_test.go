package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func uxTestConfig(t *testing.T) (Config, string, string) {
	t.Helper()
	_, root := guardRepo(t, "")
	t.Chdir(root)
	dir := t.TempDir()
	for name, value := range map[string]string{"HOME": dir, "USERPROFILE": dir, "APPDATA": dir, "XDG_CONFIG_HOME": dir, "KITSU_HOST": "", "CODEX_HOME": filepath.Join(dir, ".codex"), "CLAUDE_CONFIG_DIR": filepath.Join(dir, ".claude")} {
		t.Setenv(name, value)
	}
	checkout, personal, err := uxConfigPaths(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return Config{In: &bytes.Buffer{}, Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}, checkout, personal
}

func uxTestWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestUXHostPreferencePrecedenceAndOrigins(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	ctx := t.Context()
	assert := func(want, origin string) {
		t.Helper()
		host, source, err := uxHostPreference(ctx)
		if err != nil || host != want || source != origin {
			t.Fatalf("host=%q, source=%q, err=%v; want %q from %q", host, source, err, want, origin)
		}
	}
	assert("", "unset")
	if _, err := uxHostArgs(ctx, []string{"developer"}, true); ExitCode(err) != 2 {
		t.Fatalf("missing host did not produce a deterministic usage error: %v", err)
	}
	if err := runUXConfig(ctx, cfg, []string{"set", "host", "claude", "--global"}); err != nil {
		t.Fatal(err)
	}
	assert("claude", personal)
	if err := runUXConfig(ctx, cfg, []string{"set", "host", "codex"}); err != nil {
		t.Fatal(err)
	}
	assert("codex", checkout)
	t.Setenv("KITSU_HOST", "claude")
	assert("claude", "KITSU_HOST")
	t.Setenv("KITSU_HOST", "invalid")
	explicit := []string{"developer", "--host=codex"}
	if got, err := uxHostArgs(ctx, explicit, true); err != nil || !reflect.DeepEqual(got, explicit) {
		t.Fatalf("explicit host did not bypass invalid lower preferences: %q, %v", got, err)
	}
	if _, _, err := uxHostPreference(ctx); ExitCode(err) != 2 {
		t.Fatalf("invalid environment host accepted: %v", err)
	}
	t.Setenv("KITSU_HOST", "")
	got, err := uxHostArgs(ctx, []string{"developer", "--", "unchanged"}, true)
	if err != nil || !reflect.DeepEqual(got, []string{"developer", "--host", "codex", "--", "unchanged"}) {
		t.Fatalf("host inserted after argument boundary: %q, %v", got, err)
	}
	got, err = uxHostArgs(ctx, []string{"developer", "--model", "--host=custom-model"}, true)
	if err != nil || !reflect.DeepEqual(got, []string{"developer", "--model", "--host=custom-model", "--host", "codex"}) {
		t.Fatalf("a model value was mistaken for an explicit host: %q, %v", got, err)
	}
	if err := runUXConfig(ctx, cfg, []string{"reset", "host"}); err != nil {
		t.Fatal(err)
	}
	assert("claude", personal)
	cfg.Out.(*bytes.Buffer).Reset()
	if err := runUXConfig(ctx, cfg, []string{"show", "--json"}); err != nil {
		t.Fatal(err)
	}
	var shown struct{ Host, Source, Checkout, Personal string }
	if err := json.Unmarshal(cfg.Out.(*bytes.Buffer).Bytes(), &shown); err != nil || shown.Source != personal || shown.Checkout != checkout {
		t.Fatalf("show did not expose preference origins: %+v, %v", shown, err)
	}
	if err := runUXConfig(ctx, cfg, []string{"reset", "host", "--global"}); err != nil {
		t.Fatal(err)
	}
	assert("", "unset")
}

func TestUXHostPreferenceIsPrivateToEachWorktree(t *testing.T) {
	cfg, mainPath, _ := uxTestConfig(t)
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, ".git", uxCheckoutConfigFile); mainPath != want {
		t.Fatalf("checkout preference is inside gate evidence storage: %q, want %q", mainPath, want)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	for _, args := range [][]string{
		{"-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-qm", "first"},
		{"worktree", "add", "-qb", "linked", linked},
	} {
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
	if err := runUXConfig(t.Context(), cfg, []string{"set", "host", "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(linked); err != nil {
		t.Fatal(err)
	}
	linkedPath, _, err := uxConfigPaths(t.Context())
	if err != nil || linkedPath == mainPath || !strings.Contains(filepath.ToSlash(linkedPath), "/worktrees/") {
		t.Fatalf("linked worktree preference uses common config: %q, %v", linkedPath, err)
	}
	if host, _, err := uxHostPreference(t.Context()); err != nil || host != "" {
		t.Fatalf("main preference leaked into linked worktree: %q, %v", host, err)
	}
	if err := runUXConfig(t.Context(), cfg, []string{"set", "host", "claude"}); err != nil {
		t.Fatal(err)
	}
	if host, _, err := uxReadPreference(mainPath); err != nil || host != "codex" {
		t.Fatalf("linked edit changed main preference: %q, %v", host, err)
	}
}

func TestUXConfigStrictParsingModeAndConcurrentChange(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, data string }{
		{"future version", "version = 2\nhost = 'codex'\n"},
		{"wrong version type", "version = '1'\n"},
		{"unknown key", "version = 1\n[models]\n"},
		{"wrong host type", "version = 1\nhost = 1\n"},
		{"invalid host", "version = 1\nhost = 'other'\n"},
		{"empty host", "version = 1\nhost = ''\n"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			uxTestWrite(t, path, test.data)
			if _, _, err := uxReadPreference(path); err == nil {
				t.Fatal("invalid private preference accepted")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "config.toml")
	uxTestWrite(t, path, "version = 1\nhost = 'claude'\n")
	_, before, err := uxReadPreference(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := uxSavePreference(t.Context(), before, "codex"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("preference mode changed: %v, %v", info, err)
	}
	if err := uxSavePreference(t.Context(), before, "claude"); err == nil || !strings.Contains(err.Error(), "changed while editing") {
		t.Fatalf("stale edit overwrote a concurrent host change: %v", err)
	}
	if host, _, err := uxReadPreference(path); err != nil || host != "codex" {
		t.Fatalf("concurrent preference was overwritten: %q, %v", host, err)
	}
	link := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, _, err := uxReadPreference(link); err == nil {
		t.Fatal("private preference followed a symlink")
	}
}

func TestUXConfigShowAndInvalidArgumentsDoNotWrite(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	cfg.Editor = func(context.Context, *UXEditor) error {
		t.Fatal("read-only config command launched editor")
		return nil
	}
	for _, args := range [][]string{{"show"}, {"get", "host"}} {
		if err := runUXConfig(t.Context(), cfg, args); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"set", "host", "invalid"}, {"reset"}, {"reset", "other"}, {"show", "--global"}} {
		if err := runUXConfig(t.Context(), cfg, args); ExitCode(err) != 2 {
			t.Fatalf("invalid config %v: %v", args, err)
		}
	}
	for _, path := range []string{checkout, personal, checkout + ".lock", personal + ".lock"} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("read-only or invalid command created %s: %v", path, err)
		}
	}
}

func TestUXConfigIfUnsetUsesPersonalPreference(t *testing.T) {
	cfg, checkout, personal := uxTestConfig(t)
	uxTestWrite(t, checkout, "version = 1\nhost = \"codex\"\n")
	t.Setenv("KITSU_HOST", "codex")
	for _, requested := range []string{"claude", "codex"} {
		if err := Execute(t.Context(), []string{"config", "set", "host", requested, "--global", "--if-unset"}, cfg); err != nil {
			t.Fatal(err)
		}
		got, _, err := uxReadPreference(personal)
		if err != nil || got != "claude" {
			t.Fatalf("personal host = %q, %v", got, err)
		}
	}
}
