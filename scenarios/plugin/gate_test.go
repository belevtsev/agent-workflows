package plugin_test

import (
	"testing"
)

// Requirement "The gate needs no plugin", scenario "A commit from a script".
//
// No plugin is installed anywhere: the home directory is empty and no host
// runs. The commit of a shell script is compared with the same commit run
// the way an agent's shell tool runs it, with the variables Claude Code and
// Codex set for their shell commands.
func TestCommitFromAScript(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	repo.Write("src/app.txt", "broken\n")
	repo.Write("../commit.sh", "#!/bin/sh\ngit commit -a -m change\n")
	before := head(repo)

	script := repo.Run("", "sh", repo.Path("../commit.sh"))
	if script.Code == 0 || head(repo) != before {
		t.Fatalf("the script's commit was recorded (exit %d), want it denied:\n%s", script.Code, script.Output())
	}

	agent := repo.Command("sh", "-c", "git commit -a -m change")
	agent.Env = append(agent.Env, "CLAUDECODE=1", "CLAUDE_CODE_ENTRYPOINT=cli", "CODEX_SANDBOX=seatbelt", "CODEX_MANAGED_BY_NPM=1")
	output, err := agent.CombinedOutput()
	if err == nil || head(repo) != before {
		t.Fatalf("the agent's commit was recorded, want it denied:\n%s", output)
	}
	if script.Output() != string(output) {
		t.Errorf("the script's commit is denied with another reason than the agent's.\nscript:\n%s\nagent:\n%s", script.Output(), output)
	}
}
