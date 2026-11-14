package repocheck

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cwtest"
)

// shellBlocks returns the commands of the `sh` code blocks of a document,
// one line each, in order.
func shellBlocks(t *testing.T, document string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(document)))
	if err != nil {
		t.Fatal(err)
	}
	var commands []string
	inside := false
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		switch {
		case line == "```sh":
			inside = true
		case line == "```":
			inside = false
		case inside && strings.TrimSpace(line) != "":
			commands = append(commands, line)
		}
	}
	return commands
}

// The quick start is executed as it is written: the commands of its two
// blocks, nothing added and nothing left out, in a new directory with a new
// home. The commit must go through the hooks, and no file is written by
// hand.
func TestQuickStart(t *testing.T) {
	commands := shellBlocks(t, "docs/quickstart.md")
	want := []string{"git init demo", "cd demo", "cw init", "cw add version -- git --version", "cw run", "git add -A", `git commit -m "first gated commit"`}
	if strings.Join(commands, "\n") != strings.Join(want, "\n") {
		t.Fatalf("docs/quickstart.md holds these commands:\n%s\nwant:\n%s", strings.Join(commands, "\n"), strings.Join(want, "\n"))
	}
	// cwtest.Binary is the directory that holds the cw under test.
	binary, home, dir := filepath.Join(cwtest.Binary(t), "cw"), t.TempDir(), t.TempDir()
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	env := append(os.Environ(),
		"HOME="+home, "USERPROFILE="+home, "XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(home, ".gitconfig"),
		"GIT_AUTHOR_NAME=Quick Start", "GIT_AUTHOR_EMAIL=quick@example.com",
		"GIT_COMMITTER_NAME=Quick Start", "GIT_COMMITTER_EMAIL=quick@example.com",
		"PATH="+filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, command := range commands {
		fields := strings.Fields(command)
		if fields[0] == "cd" {
			dir = filepath.Join(dir, fields[1])
			continue
		}
		// The one quoted argument of the page is the commit message.
		if before, message, quoted := strings.Cut(command, ` "`); quoted {
			fields = append(strings.Fields(before), strings.TrimSuffix(message, `"`))
		}
		// Go finds a program on the PATH of this process, not of the
		// command, so the cw under test is named by its file.
		if fields[0] == "cw" {
			fields[0] = binary
		}
		run := exec.CommandContext(t.Context(), fields[0], fields[1:]...)
		run.Dir, run.Env = dir, env
		if output, err := run.CombinedOutput(); err != nil {
			t.Fatalf("`%s` failed: %v\n%s", command, err, output)
		}
	}
	log := exec.CommandContext(t.Context(), "git", "log", "--format=%s")
	log.Dir, log.Env = dir, env
	output, err := log.Output()
	if err != nil || strings.TrimSpace(string(output)) != "first gated commit" {
		t.Fatalf("the commit was not recorded: %v\n%s", err, output)
	}
	status := exec.CommandContext(t.Context(), binary, "status")
	status.Dir, status.Env = dir, env
	if output, err := status.CombinedOutput(); err != nil || !strings.Contains(string(output), "verified") {
		t.Fatalf("`cw status` after the commit: %v\n%s", err, output)
	}
}
