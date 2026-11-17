package onboarding_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
	"github.com/belevtsev/kitsu/v3/internal/onboarding"
)

// Diagnostics must never execute this local stand-in for a host CLI.
func TestMain(m *testing.M) {
	if os.Getenv("KITSU_SCENARIO_ONBOARDING_HOST") == "1" {
		data, _ := json.Marshal(os.Args[1:])
		_ = os.WriteFile(os.Getenv("KITSU_SCENARIO_ONBOARDING_CAPTURE"), data, 0o600)
		os.Exit(97)
	}
	os.Exit(m.Run())
}

func onboardingRepo(t *testing.T) (*kitsutest.Repo, string, string) {
	t.Helper()
	repo := kitsutest.NewRepo(t)
	hostHome := filepath.Join(repo.Home, ".codex")
	repo.Setenv("CODEX_HOME", hostHome)
	repo.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(repo.Home, ".claude"))
	repo.Setenv("KITSU_HOST", "")
	repo.Setenv("KITSU_SCENARIO_ONBOARDING_HOST", "1")
	capture := filepath.Join(repo.Home, "host-was-executed.json")
	repo.Setenv("KITSU_SCENARIO_ONBOARDING_CAPTURE", capture)
	bin := filepath.Join(repo.Home, "native host directory")
	name := "codex"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	writeOnboardingFile(t, filepath.Join(bin, name), data, 0o755)
	repo.Setenv("PATH", strings.Join([]string{kitsutest.Binary(t), bin, os.Getenv("PATH")}, string(os.PathListSeparator)))
	plugin := filepath.Join(hostHome, "plugins", "cache", "kitsu", "kitsu", "test")
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(source), "..", "..")
	for _, file := range []string{".codex-plugin/plugin.json", "skills/task/SKILL.md", "hooks/codex-hooks.json"} {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		writeOnboardingFile(t, filepath.Join(plugin, filepath.FromSlash(file)), data, 0o600)
	}
	writeOnboardingFile(t, filepath.Join(hostHome, "config.toml"), []byte("[plugins.\"kitsu@kitsu\"]\nenabled = true\n[features]\nhooks = true\n"), 0o600)
	return repo, filepath.Join(bin, name), capture
}

func writeOnboardingFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}

func TestInstalledBundleCheckAndHandoffStayReadOnly(t *testing.T) {
	t.Parallel()
	repo, host, capture := onboardingRepo(t)
	request := "Add 'literal quotes'; $(touch marker) $HOME C:\\project\\file\nnext line"
	check := repo.Kitsu("agent", "check", "--host", "codex", "--json")
	var report onboarding.Report
	if err := json.Unmarshal([]byte(check.Stdout), &report); err != nil || check.Code != 0 || !report.Ready || report.HostExecutable != host || check.Stderr != "" {
		t.Fatalf("installed bundle not recognized: %s; %v", check.Output(), err)
	}
	warnings := false
	for _, check := range report.Checks {
		warnings = warnings || check.ID == "hook_runtime" && check.Status == "warn"
	}
	if !warnings {
		t.Fatalf("local check claimed runtime verification: %+v", report)
	}
	result := repo.Kitsu("task", "start", request, "--host", "codex", "--json")
	var handoff struct {
		onboarding.Report
		Prompt string
		Argv   []string
	}
	if err := json.Unmarshal([]byte(result.Stdout), &handoff); err != nil || result.Code != 0 || !handoff.Ready || !slices.Equal(handoff.Argv, []string{host, handoff.Prompt}) || !strings.HasSuffix(handoff.Prompt, request) || result.Stderr != "" {
		t.Fatalf("invalid handoff: %s; %v", result.Output(), err)
	}
	for _, args := range [][]string{{"task", "start", request, "--host", "codex", "--print"}, {"agent", "check", "--help"}, {"task", "start", "--help"}, {"help", "task", "start"}, {"__complete", "--", "task", "start", "--host", "co"}} {
		if got := repo.Kitsu(args...); got.Code != 0 || got.Stdout == "" {
			t.Fatalf("read-only command %q: %s", args, got.Output())
		}
	}
	if got := repo.Kitsu("task", "start", request, "--host", "codex"); got.Code != 2 || !strings.Contains(got.Stderr, "--print or --json") {
		t.Fatalf("nonterminal invocation launched a host: %s", got.Output())
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("diagnostics or handoff executed the host: %v", err)
	}
	if status := repo.Git("status", "--porcelain"); status != "" || repo.Exists("openspec") || repo.Exists(".git/kitsu") || repo.Exists(".git/kitsu-cli.toml") {
		t.Fatalf("onboarding wrote repository state: %s", status)
	}
}

func TestMissingHostStillProducesAnActionableHandoff(t *testing.T) {
	t.Parallel()
	repo, host, capture := onboardingRepo(t)
	if err := os.Remove(host); err != nil {
		t.Fatal(err)
	}
	// An empty absolute directory hides any real host installed on the machine.
	repo.Setenv("PATH", kitsutest.Binary(t))
	check := repo.Kitsu("agent", "check", "--host", "codex", "--json")
	var report onboarding.Report
	if err := json.Unmarshal([]byte(check.Stdout), &report); err != nil || check.Code != 5 || report.Ready {
		t.Fatalf("missing host was not diagnosed: %s; %v", check.Output(), err)
	}
	for _, option := range []string{"--print", "--json"} {
		got := repo.Kitsu("task", "start", "Investigate the bug", "--host", "codex", option)
		if got.Code != 0 || !strings.Contains(got.Stdout, "Investigate the bug") || !strings.Contains(got.Stdout, "host_cli") {
			t.Fatalf("%s cannot prepare a missing-host handoff: %s", option, got.Output())
		}
	}
	if got := repo.Kitsu("task", "start", "Investigate the bug", "--host", "codex"); got.Code != 5 || !strings.Contains(got.Stderr, "Install the codex CLI") {
		t.Fatalf("default start ignored missing host: %s", got.Output())
	}
	if _, err := os.Stat(capture); !os.IsNotExist(err) {
		t.Fatalf("missing-host flow ran a host: %v", err)
	}
}
