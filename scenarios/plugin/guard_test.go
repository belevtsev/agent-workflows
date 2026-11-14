package plugin_test

import (
	"path/filepath"
	"strings"
	"testing"
)

// Requirement "Ask before skipping": the guard reads a shell command as words
// and asks before each spelling of a git call that skips git's hooks, also
// one that holds no --no-verify. Codex denies the same calls.
func TestAskBeforeEachSpellingThatSkipsTheHooks(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, command := range []string{
		"git commit -n -m change",
		"git commit -anm change",
		"git commit --no-verif -m change",
		"git -c core.hooksPath=/dev/null commit -m change",
		"git config core.hookspath .husky",
		"git push --no-verify",
		"cd . && git commit -n -m change",
		"sh -c 'git commit -n -m change'",
		`bash -lc "git commit --no-verify"`,
		"FOO=1 /usr/bin/git -C . commit -n",
	} {
		for _, h := range []struct{ file, want string }{{claudeHooks, "ask"}, {codexHooks, "deny"}} {
			t.Run(h.want+"/"+command, func(t *testing.T) {
				got := decisionOf(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)))
				if got.Permission != h.want {
					t.Errorf("permissionDecision is %q, want %s", got.Permission, h.want)
				}
				if !strings.Contains(strings.ToLower(got.Reason), "skips the commit gate") {
					t.Errorf("the reason does not say that the command skips the commit gate: %q", got.Reason)
				}
			})
		}
	}
}

// Requirement "Ask before skipping", scenario "An ordinary command": a
// command that only mentions --no-verify, or gives -n to a git call where it
// does not skip the hooks, is neither asked nor denied.
func TestCommandThatOnlyMentionsNoVerifyIsLetThrough(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, command := range []string{
		`git commit -m "--no-verify is blocked"`,
		`echo "--no-verify"`,
		"grep -- --no-verify docs/hooks.md",
		`git commit -m "fix -n handling"`,
		"git push -n",
		"git log -n 5",
		"git config --get core.hooksPath",
	} {
		for _, h := range hostFiles {
			t.Run(h.host+"/"+command, func(t *testing.T) {
				assertNoDecision(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)), command)
			})
		}
	}
}

// Requirement "Ask before skipping": a command the guard cannot split is
// judged by its text, as before.
func TestUnreadableCommandIsJudgedByItsText(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Bash", shellInput(`git commit --no-verify -m "open`)))
	if got.Permission != "ask" || !strings.Contains(got.Reason, "holds --no-verify") {
		t.Errorf("decision is %q with %q, want ask because the text holds --no-verify", got.Permission, got.Reason)
	}
	assertNoDecision(t, preToolUse(t, repo, claudeHooks, "Bash", shellInput(`echo "open`)), "an unclosed quote")
}

// Requirement "Ask before skipping": an edit of a git hook of the clone or of
// a record of cw is asked in Claude Code and denied in Codex.
func TestAskBeforeAnEditOfTheHooksOrTheRecords(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	targets := map[string]string{
		"git hook":      repo.Path(".git/hooks/pre-commit"),
		"records of cw": filepath.Join(repo.StoreDir(), "note"),
	}
	for want, path := range targets {
		t.Run("Write "+want, func(t *testing.T) {
			got := decisionOf(t, preToolUse(t, repo, claudeHooks, "Write", map[string]any{"file_path": path, "content": "x"}))
			if got.Permission != "ask" || !strings.Contains(got.Reason, want) {
				t.Errorf("decision is %q with %q, want ask naming the %s", got.Permission, got.Reason, want)
			}
		})
	}
	t.Run("apply_patch git hook", func(t *testing.T) {
		patch := "*** Begin Patch\n*** Update File: .git/hooks/pre-push\n@@\n-x\n+y\n*** End Patch"
		got := decisionOf(t, preToolUse(t, repo, codexHooks, "apply_patch", shellInput(patch)))
		if got.Permission != "deny" || !strings.Contains(got.Reason, "git hook") {
			t.Errorf("decision is %q with %q, want deny naming the git hook", got.Permission, got.Reason)
		}
	})
}
