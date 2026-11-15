package plugin_test

import (
	"strings"
	"testing"
)

// Requirement "Ask before skipping", scenario "A commit that skips the
// hook".
func TestAskBeforeACommitThatSkipsTheHook(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Bash", shellInput("git commit --no-verify -m change")))
	if got.Permission != "ask" {
		t.Errorf("permissionDecision is %q, want ask", got.Permission)
	}
	if lower := strings.ToLower(got.Reason); !strings.Contains(lower, "skips the commit gate") {
		t.Errorf("the question does not say that the command skips the commit gate: %q", got.Reason)
	}
}

// Requirement "Ask before skipping", scenario "A change to the declaration".
func TestAskBeforeAChangeToTheDeclaration(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	inputs := map[string]map[string]any{
		"Edit":  {"file_path": repo.Path("kitsu.toml"), "old_string": "version = 1", "new_string": "version = 1\n"},
		"Write": {"file_path": repo.Path("kitsu.toml"), "content": "version = 1\n"},
	}
	for tool, input := range inputs {
		t.Run(tool, func(t *testing.T) {
			got := decisionOf(t, preToolUse(t, repo, claudeHooks, tool, input))
			if got.Permission != "ask" {
				t.Errorf("permissionDecision is %q, want ask", got.Permission)
			}
			lower := strings.ToLower(got.Reason)
			if !strings.Contains(lower, "change") || !strings.Contains(lower, "check") {
				t.Errorf("the question does not say that the edit changes the required checks: %q", got.Reason)
			}
		})
	}
}

// Requirement "Ask before skipping", scenario "An ordinary command". Both
// hosts are checked, and so is an edit of a file other than kitsu.toml (the
// plugin SHALL NOT act on any other tool call).
func TestOrdinaryCommandIsNeitherAskedNorDenied(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, h := range hostFiles {
		for _, command := range []string{"go test ./...", "git commit -m change"} {
			t.Run(h.host+"/"+command, func(t *testing.T) {
				assertNoDecision(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)), command)
			})
		}
	}
	t.Run("Claude Code/Edit README.md", func(t *testing.T) {
		input := map[string]any{"file_path": repo.Path("README.md"), "old_string": "# demo", "new_string": "# Demo"}
		assertNoDecision(t, preToolUse(t, repo, claudeHooks, "Edit", input), "an edit of README.md")
	})
	t.Run("Codex/apply_patch README.md", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Update File: README.md\n@@\n-# demo\n+# Demo\n*** End Patch"
		assertNoDecision(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)), "a patch of README.md")
	})
}

// Requirement "Ask before skipping": where the host cannot ask (Codex), the
// plugin SHALL deny with the reason.
func TestCodexDeniesWithTheReason(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	t.Run("--no-verify", func(t *testing.T) {
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "Bash", shellInput("git commit --no-verify -m change")))
		if got.Permission != "deny" {
			t.Errorf("permissionDecision is %q, want deny", got.Permission)
		}
		if !strings.Contains(strings.ToLower(got.Reason), "skips the commit gate") {
			t.Errorf("the reason does not say that the command skips the commit gate: %q", got.Reason)
		}
	})
	t.Run("apply_patch kitsu.toml", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Update File: kitsu.toml\n@@\n-version = 1\n+version = 1\n*** End Patch"
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)))
		if got.Permission != "deny" {
			t.Errorf("permissionDecision is %q, want deny", got.Permission)
		}
		lower := strings.ToLower(got.Reason)
		if !strings.Contains(lower, "change") || !strings.Contains(lower, "check") {
			t.Errorf("the reason does not say that the edit changes the required checks: %q", got.Reason)
		}
	})
}
