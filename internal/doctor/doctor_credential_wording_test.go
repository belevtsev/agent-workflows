package projectctx

// Contract: verify-adviser-claim, interface contract revision 4, R7 (cw doctor
// credential wording). Every credential value below is a fake fixture value;
// Getenv is fully controlled and the Codex, Claude and skills homes are
// temporary directories.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The known provider credential variables (<m> = 3): TYPESAFE_API_KEY,
// SUBQ_API_KEY and OPENAI_API_KEY, as in the contract background ("0 of 3").
const acDoctorKnown = "3"

func acDoctorEnvironmentDetail(n string) string {
	return n + " of " + acDoctorKnown + " provider credential variables present in this process's environment (saved keys are not counted); cw keys status reports the keys cw can use; authentication was not attempted"
}

func acDoctorLauncherDetail(n string) string {
	return n + " of " + acDoctorKnown + " provider credential variables present in the Codex MCP launcher environment, not cw's own; cw keys status reports the keys cw can use; authentication was not attempted"
}

func acDoctorShellDetail(n string) string {
	return n + " of " + acDoctorKnown + " provider credential variables present in the Codex shell environment policy, not cw's own; cw keys status reports the keys cw can use; authentication was not attempted"
}

// acDoctorStatus is the unchanged status rule: missing when n is 0, else configured.
func acDoctorStatus(n string) string {
	if n == "0" {
		return "missing"
	}
	return "configured"
}

// acDoctorReport runs metadata-only Doctor with a controlled environment and
// an optional Codex config.toml.
func acDoctorReport(t *testing.T, environment map[string]string, codexConfig string) DoctorReport {
	t.Helper()
	home, codex := t.TempDir(), t.TempDir()
	if codexConfig != "" {
		if err := os.WriteFile(filepath.Join(codex, "config.toml"), []byte(codexConfig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Doctor(context.Background(), DoctorOptions{
		Repo: t.TempDir(), Home: home, CodexHome: codex, ClaudeHome: t.TempDir(), SkillsHome: t.TempDir(),
		Getenv:   func(name string) string { return environment[name] },
		LookPath: func(string) (string, error) { return "", os.ErrNotExist },
		Run: func(context.Context, Command) (CommandResult, error) {
			t.Error("metadata inspection ran a command")
			return CommandResult{}, errors.New("not allowed")
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

// R7: the detail of each credential presence check names the environment it
// counts, the launcher and Codex shell checks say they are not cw's own, and
// the status rule is unchanged. Fails on any other wording, a wrong count, or
// a status change.
func TestDoctorCredentialChecksNameTheirEnvironment(t *testing.T) {
	for _, c := range []struct {
		name                  string
		environment           map[string]string
		codexConfig           string
		environmentN, launchN string
		shellN                string
	}{
		{"nothing_present", nil, "", "0", "0", "0"},
		{"one_in_process_environment", map[string]string{"SUBQ_API_KEY": "fake-environment-value"}, "", "1", "0", "0"},
		{"all_in_process_environment", map[string]string{
			"TYPESAFE_API_KEY": "fake-environment-value", "SUBQ_API_KEY": "fake-environment-value", "OPENAI_API_KEY": "fake-environment-value",
		}, "", "3", "0", "0"},
		{"one_in_launcher", nil, "[mcp_servers.gopls]\nenabled = false\ncommand = 'gopls'\n[mcp_servers.gopls.env]\nTYPESAFE_API_KEY = 'fake-launcher-value'\n", "0", "1", "0"},
		{"two_in_codex_shell", nil, "[shell_environment_policy.set]\nOPENAI_API_KEY = 'fake-shell-value'\nSUBQ_API_KEY = 'fake-shell-value'\n", "0", "0", "2"},
		{"each_source_separately", map[string]string{"OPENAI_API_KEY": "fake-environment-value"},
			"[mcp_servers.gopls]\nenabled = false\ncommand = 'gopls'\n[mcp_servers.gopls.env]\nSUBQ_API_KEY = 'fake-launcher-value'\nOPENAI_API_KEY = 'fake-launcher-value'\n[shell_environment_policy.set]\nTYPESAFE_API_KEY = 'fake-shell-value'\n",
			"1", "2", "1"},
	} {
		t.Run(c.name, func(t *testing.T) {
			report := acDoctorReport(t, c.environment, c.codexConfig)
			for _, want := range []struct {
				check, detail, status string
			}{
				{"credentials_environment", acDoctorEnvironmentDetail(c.environmentN), acDoctorStatus(c.environmentN)},
				{"credentials_launcher", acDoctorLauncherDetail(c.launchN), acDoctorStatus(c.launchN)},
				{"credentials_codex_shell", acDoctorShellDetail(c.shellN), acDoctorStatus(c.shellN)},
			} {
				got, ok := report.Checks[want.check]
				if !ok {
					t.Fatalf("R7: doctor report has no %s check", want.check)
				}
				if got.Detail != want.detail {
					t.Errorf("R7: %s detail\n got %q\nwant %q", want.check, got.Detail, want.detail)
				}
				if got.Status != want.status {
					t.Errorf("R7: %s status = %q, want %q (status rule unchanged)", want.check, got.Status, want.status)
				}
			}
			encoded, err := json.Marshal(report)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(encoded), "fake-") {
				t.Fatal("R7: doctor report contains a credential value")
			}
		})
	}
}
