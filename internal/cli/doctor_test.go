package cli

import (
	"bytes"
	"context"
	json "encoding/json/v2"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/doctor"
	"github.com/spf13/cobra"
)

func doctorFixture(t *testing.T, config Config) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	config = defaults(config)
	root := &cobra.Command{Use: "cw", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("home", "", "home")
	root.PersistentFlags().String("codex-home", "", "codex home")
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(out)
	addDoctorCommand(root, config)
	return root, out
}

func doctorWrite(t *testing.T, root, path, content string) {
	t.Helper()
	full := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDoctorTargetsCwdIndependentlyOfInstallerSource(t *testing.T) {
	target, source, home := t.TempDir(), t.TempDir(), t.TempDir()
	doctorWrite(t, target, "package.json", "{}")
	doctorWrite(t, source, "go.mod", "module wrong.example/source\n")
	doctorWrite(t, home, ".codex/config.toml", "[shell_environment_policy.set]\nOPENAI_API_KEY = 'secret-value'\n")
	root, out := doctorFixture(t, Config{WorkingDir: func() (string, error) { return target, nil }, HomeDir: func() (string, error) { return "", errors.New("--home was ignored") }, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"doctor", "--home", home})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report doctor.DoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	canonical, _ := filepath.EvalSymlinks(target)
	if report.Profile.Snapshot.Root != canonical || len(report.Profile.Kinds) != 1 || report.Profile.Kinds[0] != "node" || report.Live {
		t.Fatalf("doctor selected the wrong project or mode: %+v", report)
	}
	if !report.Credentials.CodexShell["OPENAI_API_KEY"] || strings.Contains(out.String(), "secret-value") {
		t.Fatal("--home was ignored or configured credential leaked")
	}
}

func TestDoctorExplicitPathsAndTimeoutBounds(t *testing.T) {
	target, codex, skills := t.TempDir(), t.TempDir(), t.TempDir()
	doctorWrite(t, target, "nested/go.mod", "module example.invalid/nested\n")
	doctorWrite(t, codex, "config.toml", "[mcp_servers.gopls]\nenabled = true\n")
	for _, name := range []string{"task-orchestration", "production-plan", "code-review"} {
		doctorWrite(t, skills, name+"/SKILL.md", "fixture")
	}
	root, out := doctorFixture(t, Config{WorkingDir: func() (string, error) { return "", errors.New("--repo was ignored") }, HomeDir: func() (string, error) { return t.TempDir(), nil }, Getenv: func(string) string { return "" }})
	root.SetArgs([]string{"doctor", "--repo", target, "--codex-home", codex, "--skills-home", skills, "--timeout", "2s"})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	var report doctor.DoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Checks["mcp_gopls"].Status != "configured" || report.Checks["workflow_skills"].Status != "configured" {
		t.Fatal("explicit doctor directories were ignored")
	}
	for _, timeout := range []string{"1ms", "2m"} {
		root, _ := doctorFixture(t, Config{})
		root.SetArgs([]string{"doctor", "--timeout", timeout})
		if err := root.ExecuteContext(context.Background()); ExitCode(err) != 2 {
			t.Fatalf("invalid timeout %s was not a usage error: %v", timeout, err)
		}
	}
}

func TestDoctorHelpDoesNotInspectEnvironment(t *testing.T) {
	root, out := doctorFixture(t, Config{WorkingDir: func() (string, error) { t.Fatal("help inspected cwd"); return "", nil }, HomeDir: func() (string, error) { t.Fatal("help inspected home"); return "", nil }})
	root.SetArgs([]string{"doctor", "--help"})
	if err := root.ExecuteContext(context.Background()); err != nil || !strings.Contains(out.String(), "--repo") || !strings.Contains(out.String(), "--live") {
		t.Fatalf("doctor help failed: %v", err)
	}
}

func TestDoctorHonorsCodexHomeEnvironmentAndExplicitFlagPrecedence(t *testing.T) {
	target, home, environmentCodex, explicitCodex := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	doctorWrite(t, environmentCodex, "config.toml", "[mcp_servers.jbcontext]\nenabled = true\n")
	doctorWrite(t, explicitCodex, "config.toml", "[mcp_servers.gopls]\nenabled = true\n")
	for _, test := range []struct {
		name, explicit, configured string
	}{{"environment", "", "mcp_jbcontext"}, {"explicit", explicitCodex, "mcp_gopls"}} {
		t.Run(test.name, func(t *testing.T) {
			root, out := doctorFixture(t, Config{WorkingDir: func() (string, error) { return target, nil }, HomeDir: func() (string, error) { return home, nil }, Getenv: func(key string) string {
				if key == "CODEX_HOME" {
					return environmentCodex
				}
				return ""
			}})
			args := []string{"doctor"}
			if test.explicit != "" {
				args = append(args, "--codex-home", test.explicit)
			}
			root.SetArgs(args)
			if err := root.ExecuteContext(context.Background()); err != nil {
				t.Fatal(err)
			}
			var report doctor.DoctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Checks[test.configured].Status != "configured" {
				t.Fatalf("Codex home precedence was ignored: %+v %v", report.Checks, err)
			}
		})
	}
}

func TestDoctorGitHooksCheckNeedsAttentionOnlyForAnOpenTaskWithoutGitHooks(t *testing.T) {
	// Oracle: cw doctor's git_hooks check has the status each case names,
	// with a detail that names cw gate install wherever git runs and the
	// gate is on, and cw doctor --summary, which exits 0, lists it among its
	// problems (which make the summary's status attention) only when a git
	// hook is absent from the repository's own hooks directory, or is cw's
	// script without an execute bit, which git skips, while a substantial
	// task is open there and the gate is on. A repository that only lacks
	// the hooks gets no problem. Where some hooks are installed, the detail
	// names the absent ones and says what skips the gate without them, only
	// a merge commit for pre-merge-commit (F4 review finding 3).
	// cw gate and cw doctor read git's configuration from this process's
	// environment.
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tests := []struct {
		name    string
		setup   func(t *testing.T, h *hkHarness, repo string) string
		status  string
		problem bool
		// says is part of the detail, when not empty.
		says string
	}{
		{"no hooks and no task", func(_ *testing.T, _ *hkHarness, repo string) string { return repo }, "not_applicable", false, ""},
		{"no hooks and a small task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSmallPlan)
			return repo
		}, "not_applicable", false, ""},
		{"no hooks and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			return repo
		}, "missing", true, "but git's hooks are absent, so a commit or push from a script, an alias or a terminal, or a merge commit made without conflicts skips the commit gate; cw gate install adds them"},
		{"hooks installed and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			if err := Execute(t.Context(), []string{"gate", "install", "--repo", repo}, h.config); err != nil {
				t.Fatalf("cw gate install: %v", err)
			}
			return repo
		}, "configured", false, ""},
		// An install by a cw that wrote no pre-merge-commit: git makes a merge
		// commit without the gate, so cw gate install needs to run again.
		{"only the pre-commit and pre-push hooks and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			for _, name := range []string{"pre-commit", "pre-push"} {
				hkWriteFile(t, repo, ".git/hooks/"+name, gtScript(name))
				if err := os.Chmod(filepath.Join(repo, ".git", "hooks", name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			return repo
		}, "missing", true, "but git's pre-merge-commit hook is absent, so a merge commit made without conflicts skips the commit gate; cw gate install adds it"},
		{"only the pre-commit and pre-push hooks and no task", func(t *testing.T, h *hkHarness, repo string) string {
			for _, name := range []string{"pre-commit", "pre-push"} {
				hkWriteFile(t, repo, ".git/hooks/"+name, gtScript(name))
				if err := os.Chmod(filepath.Join(repo, ".git", "hooks", name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			return repo
		}, "not_applicable", false, "Git's pre-merge-commit hook is absent, so a merge commit made without conflicts skips the commit gate, and no open substantial task governs this repository; cw gate install adds it"},
		{"a hook cw gate install did not write and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			hkWriteFile(t, repo, ".git/hooks/pre-push", "#!/bin/sh\nexit 0\n")
			return repo
		}, "detected", false, ""},
		{"a hooks directory other repositories use and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			global := hkCanonicalTempDir(t)
			hkWriteFile(t, global, "gitconfig", "[core]\n\thooksPath = "+hkCanonicalTempDir(t)+"\n")
			t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(global, "gitconfig"))
			return repo
		}, "detected", false, ""},
		{"cw's hook without an execute bit and an open substantial task", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			if err := Execute(t.Context(), []string{"gate", "install", "--repo", repo}, h.config); err != nil {
				t.Fatalf("cw gate install: %v", err)
			}
			// Git skips a hook it cannot execute.
			if err := os.Chmod(filepath.Join(repo, ".git", "hooks", "pre-commit"), 0o644); err != nil {
				t.Fatal(err)
			}
			return repo
		}, "missing", true, "but git's pre-commit hook is absent, so a commit from a script, an alias or a terminal skips the commit gate; cw gate install adds it"},
		{"no hooks and an open substantial task with the gate off", func(t *testing.T, h *hkHarness, repo string) string {
			h.start(t, repo, hkSubstantialPlan)
			h.env[enforceVerifyOption] = "false"
			return repo
		}, "not_applicable", false, ""},
		{"not a repository", func(t *testing.T, _ *hkHarness, _ string) string { return hkCanonicalTempDir(t) }, "unchecked", false, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHKHarness(t)
			dir := test.setup(t, h, hkRepo(t))
			h.out.Reset()
			if err := Execute(t.Context(), []string{"doctor", "--repo", dir}, h.config); err != nil {
				t.Fatalf("cw doctor: %v", err)
			}
			var report struct {
				Checks map[string]doctor.Check `json:"checks"`
			}
			if err := json.Unmarshal(h.out.Bytes(), &report); err != nil {
				t.Fatalf("cw doctor printed %q: %v", h.out.String(), err)
			}
			check, ok := report.Checks["git_hooks"]
			if !ok || check.Status != test.status {
				t.Fatalf("git_hooks check %+v (present %v), want status %s", check, ok, test.status)
			}
			if test.status != "unchecked" && h.env[enforceVerifyOption] != "false" && !strings.Contains(check.Detail, "cw gate install") {
				t.Fatalf("git_hooks detail %q does not name cw gate install", check.Detail)
			}
			if !strings.Contains(check.Detail, test.says) {
				t.Fatalf("git_hooks detail %q, want it to say %q", check.Detail, test.says)
			}
			h.out.Reset()
			if err := Execute(t.Context(), []string{"doctor", "--repo", dir, "--summary"}, h.config); err != nil {
				t.Fatalf("cw doctor --summary: %v", err)
			}
			var summary struct {
				Status   string                  `json:"status"`
				Problems map[string]doctor.Check `json:"problems"`
			}
			if err := json.Unmarshal(h.out.Bytes(), &summary); err != nil {
				t.Fatalf("cw doctor --summary printed %q: %v", h.out.String(), err)
			}
			problem, listed := summary.Problems["git_hooks"]
			if listed != test.problem {
				t.Fatalf("cw doctor --summary lists git_hooks: %v, want %v; problems %+v", listed, test.problem, summary.Problems)
			}
			if listed && (problem.Status != test.status || problem.Detail != check.Detail || summary.Status != "attention") {
				t.Fatalf("cw doctor --summary has status %s and git_hooks problem %+v, want attention and %+v", summary.Status, problem, check)
			}
		})
	}

	t.Run("session-start doctor summary leaves it out", func(t *testing.T) {
		// The doctor summary that session-start appends, after the cw gate
		// install line, leaves the check out, since that line covers it.
		const instructions = "# Rules\n\nWork carefully."
		repo := hkRepo(t)
		h := newHKHarness(t)
		h.start(t, repo, hkSubstantialPlan)
		h.env[sessionDoctorOption] = "true"
		stdin, err := json.Marshal(map[string]any{"session_id": "s", "hook_event_name": "SessionStart", "source": "startup", "cwd": repo})
		if err != nil {
			t.Fatal(err)
		}
		got, err := h.runSessionStart(t, string(stdin), "--instructions", hkWriteInstructions(t, instructions), "--cache", hkCanonicalTempDir(t))
		if err != nil {
			t.Fatalf("session-start: %v", err)
		}
		line := hkDoctorSummary(t, hkSessionContext(t, got), instructions+"\n\n"+hkGateLine)
		var summary struct {
			Problems map[string]doctor.Check `json:"problems"`
		}
		if err := json.Unmarshal([]byte(line), &summary); err != nil {
			t.Fatal(err)
		}
		if problem, listed := summary.Problems["git_hooks"]; listed {
			t.Fatalf("session-start doctor summary lists git_hooks %+v; the line covers it: %s", problem, line)
		}
	})
}
