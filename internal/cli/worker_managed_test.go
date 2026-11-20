package cli

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

func TestFormerConfiguredLaunchRequiresSupervisor(t *testing.T) {
	r := kitsutest.NewRepo(t)
	got := r.Kitsu("task", "dispatch", strings.Repeat("a", 64), "--run")
	if got.Code == 0 || !strings.Contains(got.Output(), "supervised workers and checks") {
		t.Fatal(got.Output())
	}
}

const managedFixtureConfig = `version = 2
[paths]
tests = ["tests/**"]
[roles.lookup]
host = "codex"
[roles.spec-writer]
host = "claude"
[roles.architect]
host = "codex"
[roles.quality-engineer]
host = "claude"
[roles.developer]
host = "claude"
[roles.reviewer]
host = "codex"
model = "configured-reviewer"
effort = "high"
[roles.git]
host = "codex"
model = "configured-git"
effort = "high"
`

func TestConfiguredManagedFullStageRequiresOpenSpecApprovals(t *testing.T) {
	t.Parallel()
	r := compactChecksFixture(t, "pass")
	r.Write("kitsu-roles.toml", managedFixtureConfig)
	r.Write("openspec/changes/fix-login/proposal.md", "# Fix login\n")
	result := r.Run("Follow the saved design assignment.\n", filepath.Join(kitsutest.Binary(t), compactBinaryName()),
		"task", "prepare", "fix-login", "--role", "architect", "--stage", "design", "--round", "1",
		"--workflow", "full", "--assignment", "-", "--dispatch")
	if result.Code == 0 || !strings.Contains(result.Stderr, "outstanding work and approvals") ||
		r.Exists(".git/kitsu-stages/active.json") {
		t.Fatal("design stage started without a specification approval", result.Output())
	}
}
