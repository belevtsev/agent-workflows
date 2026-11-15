package plugin_test

import (
	"os"
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// agentRuns runs a command line the way an agent's shell tool does, with
// sh -c in the repository.
func agentRuns(repo *gatecubtest.Repo, command string) gatecubtest.Result {
	return repo.Run("", "sh", "-c", command)
}

// assertPluginStaysOut fails the test when the PreToolUse command of either
// host asks or denies for a command line.
func assertPluginStaysOut(t *testing.T, repo *gatecubtest.Repo, command string) {
	t.Helper()
	for _, h := range hostFiles {
		assertNoDecision(t, preToolUse(t, repo, h.file, "Bash", shellInput(command)), h.host+" and "+command)
	}
}

// Requirement "The plugin never judges a commit or push", scenario "A piped
// commit".
func TestPipedCommitIsDenied(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	repo.Write("src/app.txt", "broken\n")
	before := head(repo)
	const command = "git commit -q -a -m x 2>&1 | tail -5"

	result := agentRuns(repo, command)
	if after := head(repo); after != before {
		t.Errorf("the commit was recorded (%s), want it denied:\n%s", after, result.Output())
	}
	assertPluginStaysOut(t, repo, command)
}

// Requirement "The plugin never judges a commit or push", scenario "A commit
// inside another shell".
func TestCommitInsideAnotherShellIsDenied(t *testing.T) {
	t.Parallel()
	needShell(t)
	for _, c := range []struct{ program, command string }{
		{"/bin/bash", "/bin/bash -c 'git commit -a -m x'"},
		{"/usr/bin/env", "/usr/bin/env git commit -a -m x"},
	} {
		t.Run(c.command, func(t *testing.T) {
			t.Parallel()
			if _, err := os.Stat(c.program); err != nil {
				t.Skipf("%s is not on this machine: %v", c.program, err)
			}
			repo := gatedDemo(t)
			repo.Write("src/app.txt", "broken\n")
			before := head(repo)

			result := agentRuns(repo, c.command)
			if result.Code == 0 || head(repo) != before {
				t.Errorf("the commit was recorded (exit %d), want it denied:\n%s", result.Code, result.Output())
			}
			assertPluginStaysOut(t, repo, c.command)
		})
	}
}

// Requirement "The plugin never judges a commit or push", scenario "A commit
// message that mentions a push".
func TestCommitMessageThatMentionsAPush(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	repo.Write("README.md", "# demo\n\nRelease notes.\n")
	if result := repo.Gatecub("run"); result.Code != 0 {
		t.Fatalf("gatecub run exited %d:\n%s", result.Code, result.Output())
	}
	before := head(repo)
	const command = "git add -A && git commit -F - <<'EOF'\n" +
		"Add release notes\n\n" +
		"Run `git push` after the review.\n" +
		"EOF\n"

	assertPluginStaysOut(t, repo, command)
	result := agentRuns(repo, command)
	if result.Code != 0 || head(repo) == before {
		t.Fatalf("the commit was not recorded (exit %d):\n%s", result.Code, result.Output())
	}
	if message := repo.Git("log", "-1", "--format=%B"); !strings.Contains(message, "`git push`") {
		t.Errorf("the recorded message does not hold `git push`:\n%s", message)
	}
}
