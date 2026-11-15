package plugin_test

import (
	"os"
	"strings"
	"testing"
)

// hostDecisions are the decision each host's file gives where the guard
// speaks up: Claude Code asks, Codex denies.
var hostDecisions = []struct{ host, file, want string }{
	{"Claude Code", claudeHooks, "ask"},
	{"Codex", codexHooks, "deny"},
}

// assertDecision fails the test unless the hook command gave the decision
// want with a reason that holds phrase.
func assertDecision(t *testing.T, got decision, want, phrase string) {
	t.Helper()
	if got.Permission != want {
		t.Errorf("permissionDecision is %q, want %s", got.Permission, want)
	}
	if !strings.Contains(strings.ToLower(got.Reason), strings.ToLower(phrase)) {
		t.Errorf("the reason does not hold %q: %q", phrase, got.Reason)
	}
}

// Requirement "Ask before skipping", scenario "The shell tool of Windows".
func TestAskBeforeSkippingInPowerShell(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	got := decisionOf(t, preToolUse(t, repo, claudeHooks, "PowerShell", shellInput("git commit --no-verify -m change")))
	assertDecision(t, got, "ask", "skips the commit gate")
}

// Requirement "Ask before skipping", scenario "A call that turns the gate
// off".
func TestAskBeforeACallThatTurnsTheGateOff(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, command := range []string{"kitsu uninstall", "kitsu init --observe"} {
		for _, h := range hostDecisions {
			t.Run(h.host+"/"+command, func(t *testing.T) {
				got := decisionOf(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)))
				assertDecision(t, got, h.want, "skips the commit gate")
			})
		}
	}
}

// Requirement "Ask before skipping", scenario "An edit of the git
// configuration".
func TestAskBeforeAnEditOfTheGitConfiguration(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	t.Run("Claude Code/Write", func(t *testing.T) {
		got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Write", map[string]any{"file_path": repo.Path(".git/config"), "content": "[core]\n"}))
		assertDecision(t, got, "ask", "git configuration")
	})
	t.Run("Codex/apply_patch", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Update File: .git/config\n@@\n-x\n+y\n*** End Patch"
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)))
		assertDecision(t, got, "deny", "git configuration")
	})
}

// Requirement "Ask before skipping", scenario "A hook in the folder of
// core.hooksPath".
func TestAskBeforeAnEditOfAHookOfHooksPath(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	repo.Write(".githooks/pre-commit", "#!/bin/sh\n")
	repo.Git("config", "core.hooksPath", ".githooks")
	t.Run("Claude Code/Edit", func(t *testing.T) {
		input := map[string]any{"file_path": repo.Path(".githooks/pre-commit"), "old_string": "#!/bin/sh", "new_string": "#!/bin/bash"}
		got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Edit", input))
		assertDecision(t, got, "ask", "git hook")
	})
	t.Run("Codex/apply_patch", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Add File: .githooks/pre-push\n+exit 0\n*** End Patch"
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)))
		assertDecision(t, got, "deny", "git hook")
	})
}

// Requirement "Ask before skipping", scenario "An edit through a link".
func TestAskBeforeAnEditThroughALink(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	if err := os.Symlink("kitsu.toml", repo.Path("alias.toml")); err != nil {
		t.Skipf("no symbolic link can be made here: %v", err)
	}
	t.Run("Claude Code/Write", func(t *testing.T) {
		got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Write", map[string]any{"file_path": repo.Path("alias.toml"), "content": "version = 1\n"}))
		assertDecision(t, got, "ask", "kitsu.toml")
	})
	t.Run("Codex/apply_patch", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Update File: alias.toml\n@@\n-version = 1\n+version = 1\n*** End Patch"
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)))
		assertDecision(t, got, "deny", "kitsu.toml")
	})
}

// Requirement "Ask before skipping", scenario "A command given as a list".
func TestAskBeforeACommandGivenAsAList(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, words := range [][]any{
		{"bash", "-lc", "git commit --no-verify"},
		{"git", "commit", "--no-verify"},
	} {
		for _, h := range hostDecisions {
			t.Run(h.host+"/"+strings.Join([]string{words[0].(string), words[1].(string)}, " "), func(t *testing.T) {
				got := decisionOf(t, preToolUse(t, repo, h.file, "Bash", map[string]any{"command": words}))
				assertDecision(t, got, h.want, "skips the commit gate")
			})
		}
	}
	t.Run("a list that skips nothing", func(t *testing.T) {
		input := map[string]any{"command": []any{"bash", "-lc", "git commit -m change"}}
		assertNoDecision(t, preToolUse(t, repo, codexHooks, "Bash", input), "a list without --no-verify")
	})
}

// Requirement "Ask before skipping", scenario "A notice larger than 16 MiB".
func TestAskBeforeAWriteWithALargeNotice(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	content := "version = 1\n" + strings.Repeat("# filler\n", (17<<20)/9)
	got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Write", map[string]any{"file_path": repo.Path("kitsu.toml"), "content": content}))
	assertDecision(t, got, "ask", "kitsu.toml")
}

// Requirement "Ask before skipping", scenario "A patch given to the shell".
func TestAskBeforeAPatchGivenToTheShell(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	command := "apply_patch <<'EOF'\n*** Begin Patch\n*** Update File: kitsu.toml\n@@\n-version = 1\n+version = 2\n*** End Patch\nEOF\n"
	for _, h := range hostDecisions {
		t.Run(h.host, func(t *testing.T) {
			got := decisionOf(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)))
			assertDecision(t, got, h.want, "kitsu.toml")
		})
	}
	t.Run("a patch of README.md", func(t *testing.T) {
		readme := strings.ReplaceAll(command, "kitsu.toml", "README.md")
		assertNoDecision(t, preToolUse(t, repo, codexHooks, "Bash", shellInput(readme)), "a patch of README.md given to the shell")
	})
}

// Requirement "Ask before skipping", scenario "An edit of another file".
func TestWriteOfAnotherFileIsNeitherAskedNorDenied(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	input := map[string]any{"file_path": repo.Path("README.md"), "content": "# Demo\n"}
	assertNoDecision(t, preToolUse(t, repo, claudeHooks, "Write", input), "a Write of README.md")
}
