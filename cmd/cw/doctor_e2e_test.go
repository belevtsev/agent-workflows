package main_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The rc.4 exit check: in a fresh Go module, with git and cw on PATH and the
// plugin installed, cw doctor --summary reports no workflow blocker. Oracle:
// with HOME holding only the plugin's skills in Claude Code's plugin cache (no
// Codex config, Claude settings, MCP server, credential or optional tool), the
// built cw prints status ok and an empty problems object, and exits 0.
func TestDoctorSummaryIsOkInAFreshGoModule(t *testing.T) {
	t.Parallel()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git is needed for the fixture: %v", err)
	}
	cw := buildPlainCW(t)
	module, home := t.TempDir(), t.TempDir()
	for _, skill := range []string{"task-orchestration", "production-plan", "code-review"} {
		path := filepath.Join(home, ".claude", "plugins", "cache", "agent-workflows", "agent-workflows", "3.0.0", "skills", skill, "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(module, "go.mod"), []byte("module example.invalid/fresh\n\ngo 1.27.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	environment := []string{
		"HOME=" + home,
		"PATH=" + filepath.Dir(cw) + string(os.PathListSeparator) + filepath.Dir(git),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
	}
	initialize := exec.CommandContext(t.Context(), git, "init", "-q", "-b", "main")
	initialize.Dir, initialize.Env = module, environment
	if output, err := initialize.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}

	doctor := exec.CommandContext(t.Context(), cw, "doctor", "--summary")
	doctor.Dir, doctor.Env = module, environment
	output, err := doctor.Output()
	if err != nil {
		t.Fatalf("cw doctor --summary: %v: %s", err, output)
	}
	var summary struct {
		Status   string                     `json:"status"`
		Problems map[string]json.RawMessage `json:"problems"`
	}
	if err := json.Unmarshal(output, &summary); err != nil {
		t.Fatalf("cw doctor --summary printed %q: %v", output, err)
	}
	if summary.Status != "ok" || summary.Problems == nil || len(summary.Problems) != 0 || !strings.Contains(string(output), `"status":"ok"`) {
		t.Fatalf("cw doctor --summary = %s, want status ok and no problems", output)
	}
}
