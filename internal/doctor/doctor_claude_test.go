package doctor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDoctorReportsClaudeSettingsWithoutValues(t *testing.T) {
	for _, test := range []struct{ name, contents, status string }{
		{name: "missing", status: "missing"},
		{name: "valid", contents: `{"apiKeyHelper":"secret-helper"}`, status: "detected"},
		{name: "malformed", contents: `{"apiKeyHelper":`, status: "degraded"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, claude := t.TempDir(), t.TempDir()
			if test.contents != "" {
				if err := os.WriteFile(filepath.Join(claude, "settings.json"), []byte(test.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			report, err := Doctor(context.Background(), DoctorOptions{Repo: t.TempDir(), Home: home, ClaudeHome: claude, CodexHome: t.TempDir(), Getenv: func(string) string { return "" }})
			if err != nil {
				t.Fatal(err)
			}
			if got := report.Checks["claude_config"]; got.Status != test.status || got.Source != "selected_claude_settings" {
				t.Fatalf("wrong check: %+v", got)
			}
		})
	}
}

func TestDoctorFindsSkillsInEitherHostRoot(t *testing.T) {
	home, claude := t.TempDir(), t.TempDir()
	for _, skill := range []string{"task-orchestration", "production-plan", "code-review"} {
		directory := filepath.Join(claude, "skills", skill)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(directory, "SKILL.md"), []byte("---\nname: x\n---\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Doctor(context.Background(), DoctorOptions{Repo: t.TempDir(), Home: home, ClaudeHome: claude, CodexHome: t.TempDir(), Getenv: func(string) string { return "" }})
	if err != nil {
		t.Fatal(err)
	}
	if got := report.Checks["workflow_skills"].Status; got != "configured" {
		t.Fatalf("Claude skills not found: %s", got)
	}
}
