package plugin_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/gatecub/v3/internal/gatecubtest"
)

// Requirement "Session context", scenario "A gated repository".
func TestSessionContextInAGatedRepository(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, repo, repo.Env(), h.file)
			if result.Code != 0 {
				t.Errorf("the SessionStart command exited %d, want 0:\n%s", result.Code, result.Output())
			}
			for _, want := range []string{"gatecub run", "gatecub status"} {
				if !strings.Contains(result.Stdout, want) {
					t.Errorf("the context does not hold %q:\n%s", want, result.Stdout)
				}
			}
			if n := len([]rune(result.Stdout)); n > 1000 {
				t.Errorf("the context is %d characters, want at most 1,000:\n%s", n, result.Stdout)
			}
		})
	}
}

// Requirement "Session context", scenario "Hooks are not installed in this
// clone".
func TestSessionContextWhenHooksAreNotInstalled(t *testing.T) {
	t.Parallel()
	needShell(t)
	origin := gatedDemo(t)
	clone := origin.In(t.TempDir() + "/clone")
	origin.Git("clone", "-q", origin.Dir, clone.Dir)
	for _, h := range hostFiles {
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, clone, clone.Env(), h.file)
			if result.Code != 0 {
				t.Errorf("the SessionStart command exited %d, want 0:\n%s", result.Code, result.Output())
			}
			lower := strings.ToLower(result.Stdout)
			if !strings.Contains(lower, "hooks are not installed") {
				t.Errorf("the context does not say that the hooks are not installed:\n%s", result.Stdout)
			}
			if !strings.Contains(result.Stdout, "gatecub init") {
				t.Errorf("the context does not say that gatecub init installs them:\n%s", result.Stdout)
			}
			// It says so first, before what the gate does (U6).
			missing, gated := strings.Index(lower, "nothing is gated"), strings.Index(lower, "allows a commit")
			if missing < 0 || gated < 0 || missing > gated || !strings.Contains(strings.SplitN(lower, ".", 2)[0], "hooks are not installed") {
				t.Errorf("the context does not start by saying that the hooks are not installed and nothing is gated yet:\n%s", result.Stdout)
			}
		})
	}
}

// Requirement "Session context", scenario "A repository without a
// declaration". The requirement says "in any other directory", so a
// directory outside any repository is checked as well.
func TestSessionContextWithoutADeclaration(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatecubtest.NewRepo(t)
	repo.Write("README.md", "# plain\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "first")
	outside := repo.In(t.TempDir())
	for _, h := range hostFiles {
		for _, dir := range []struct {
			name string
			repo *gatecubtest.Repo
		}{{"repository without gatecub.toml", repo}, {"directory outside git", outside}} {
			t.Run(h.host+"/"+dir.name, func(t *testing.T) {
				result := sessionStart(t, dir.repo, dir.repo.Env(), h.file)
				if result.Code != 0 || result.Stdout != "" {
					t.Errorf("the SessionStart command exited %d and printed, want exit 0 and nothing:\n%s", result.Code, result.Output())
				}
			})
		}
	}
}
