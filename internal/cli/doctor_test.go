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

	"github.com/belevtsev/agent-workflows/v3/internal/projectctx"
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
	var report projectctx.DoctorReport
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
	var report projectctx.DoctorReport
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
			var report projectctx.DoctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil || report.Checks[test.configured].Status != "configured" {
				t.Fatalf("Codex home precedence was ignored: %+v %v", report.Checks, err)
			}
		})
	}
}
