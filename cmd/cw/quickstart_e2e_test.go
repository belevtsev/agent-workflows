package main_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// quickstartBlock matches a fenced block of docs/quickstart.md and its info
// string.
var quickstartBlock = regexp.MustCompile("(?ms)^```([^\\n]*)\\n(.*?)^```$")

// quickstartBlocks returns the bodies of the blocks of page whose info string
// is info, in order.
func quickstartBlocks(page, info string) []string {
	var bodies []string
	for _, match := range quickstartBlock.FindAllStringSubmatch(page, -1) {
		if strings.TrimSpace(match[1]) == info {
			bodies = append(bodies, match[2])
		}
	}
	return bodies
}

// The quick start reaches a verified commit as written. The test runs every
// `sh quickstart` block of docs/quickstart.md, in order, in one POSIX shell
// with -e, against the cw built from this tree, in a temporary HOME whose
// state, configuration, Claude Code and Codex directories are all temporary;
// the reviewer's result is the recorded fixture the page shows. Oracle: the
// script exits 0, so every command in it did, including the final
// cw task verify; verify printed "complete":true; and git log -1 in the module
// shows the quick start's commit.
func TestQuickstartReachesVerifiedCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end test builds the cw binary")
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skipf("git not found: %v", err)
	}
	shasumPath, err := exec.LookPath("shasum")
	if err != nil {
		t.Skipf("shasum not found: %v", err)
	}
	page, err := os.ReadFile(filepath.Join("..", "..", "docs", "quickstart.md"))
	if err != nil {
		t.Fatal(err)
	}
	blocks := quickstartBlocks(string(page), "sh quickstart")
	if len(blocks) < 8 {
		t.Fatalf("docs/quickstart.md has %d sh quickstart blocks, want the whole flow", len(blocks))
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "quickstart", "review-result.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The page shows the fixture verbatim, so a person saves what the test uses.
	if shown := quickstartBlocks(string(page), "json"); len(shown) != 1 || shown[0] != string(fixture) {
		t.Fatalf("docs/quickstart.md must show testdata/quickstart/review-result.json as its one json block; shows %q", shown)
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home, bin := filepath.Join(root, "home"), filepath.Join(root, "bin")
	work := filepath.Join(home, "cw-quickstart")
	for _, dir := range []string{home, bin, work} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Rename(buildPlainCW(t), filepath.Join(bin, "cw")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "review-result.json"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	gitConfig := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(gitConfig, []byte("[user]\n\tname = Quick Start\n\temail = quickstart@example.invalid\n[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var script strings.Builder
	script.WriteString("set -e\n")
	for i, block := range blocks {
		script.WriteString("echo '>>> quickstart block " + strconv.Itoa(i+1) + "' >&2\n" + block)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", script.String())
	cmd.Dir = home
	// Only these variables reach the script: nothing points at the real
	// home, ~/.claude, ~/.codex or the user's cw state and configuration.
	path := strings.Join([]string{bin, filepath.Dir(goTool(t)), filepath.Dir(gitPath), filepath.Dir(shasumPath), "/usr/bin", "/bin"}, string(os.PathListSeparator))
	cmd.Env = []string{
		"PATH=" + path,
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(root, "config"),
		"XDG_STATE_HOME=" + filepath.Join(root, "state"),
		"XDG_CACHE_HOME=" + filepath.Join(root, "cache"),
		"CLAUDE_CONFIG_DIR=" + filepath.Join(root, "claude"),
		"CODEX_HOME=" + filepath.Join(root, "codex"),
		"GIT_CONFIG_GLOBAL=" + gitConfig,
		"GIT_CONFIG_NOSYSTEM=1",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
	}
	// Reuse the build cache of the go that runs this test, so the race build
	// of the standard library is not repeated; it holds no user settings.
	if cache, err := exec.CommandContext(ctx, goTool(t), "env", "GOCACHE").Output(); err == nil && len(bytes.TrimSpace(cache)) > 0 {
		cmd.Env = append(cmd.Env, "GOCACHE="+string(bytes.TrimSpace(cache)))
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("the quick start failed: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"complete":true`) {
		t.Errorf("cw task verify did not print \"complete\":true\nstdout:\n%s", stdout.String())
	}

	module := filepath.Join(work, "greet")
	log := exec.CommandContext(ctx, gitPath, "log", "-1", "--format=%s")
	log.Dir = module
	log.Env = []string{"HOME=" + home, "GIT_CONFIG_GLOBAL=" + gitConfig, "GIT_CONFIG_NOSYSTEM=1"}
	subject, err := log.Output()
	if err != nil || strings.TrimSpace(string(subject)) != "Add Goodbye" {
		t.Fatalf("git log -1 in the module gives %q, %v; want the quick start's commit \"Add Goodbye\"", subject, err)
	}
	// The commit holds the change the task verified.
	show := exec.CommandContext(ctx, gitPath, "show", "HEAD:greet.go")
	show.Dir, show.Env = module, log.Env
	if body, err := show.Output(); err != nil || !bytes.Contains(body, []byte("func Goodbye(")) {
		t.Fatalf("the commit's greet.go has no Goodbye: %v\n%s", err, body)
	}
}
