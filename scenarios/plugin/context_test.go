package plugin_test

import (
	"strings"
	"testing"

	"github.com/belevtsev/kitsu/v3/internal/kitsutest"
)

// Requirement "Session context", scenario "A gated repository".
func TestSessionContextInAGatedRepository(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := gatedDemo(t)
	for _, h := range hostFiles {
		host := "claude"
		if h.file == codexHooks {
			host = "codex"
		}
		t.Run(h.host, func(t *testing.T) {
			result := sessionStart(t, repo, repo.Env(), h.file)
			if result.Code != 0 {
				t.Errorf("the SessionStart command exited %d, want 0:\n%s", result.Code, result.Output())
			}
			for _, want := range []string{"Kitsu verifies coding tasks", "kitsu agent check --host " + host, "kitsu run", "kitsu status"} {
				if !strings.Contains(result.Stdout, want) {
					t.Errorf("the context does not hold %q:\n%s", want, result.Stdout)
				}
			}
			if n := len([]rune(result.Stdout)); n > 620 {
				t.Errorf("the context is %d characters, want at most 620:\n%s", n, result.Stdout)
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
			if !strings.Contains(result.Stdout, "kitsu init") {
				t.Errorf("the context does not say that kitsu init installs them:\n%s", result.Stdout)
			}
			// The workflow introduction precedes the gate paragraph; that paragraph
			// must still say that hooks are missing before describing enforcement.
			missing, gated := strings.Index(lower, "nothing is gated"), strings.Index(lower, "allows a commit")
			_, gateParagraph, _ := strings.Cut(lower, "\n\n")
			if missing < 0 || gated < 0 || missing > gated || !strings.Contains(strings.SplitN(gateParagraph, ".", 2)[0], "hooks are not installed") {
				t.Errorf("the context does not start by saying that the hooks are not installed and nothing is gated yet:\n%s", result.Stdout)
			}
		})
	}
}

// Requirement "Session context", scenario "A repository without a
// declaration". Onboarding introduces coordination before a gate is set up;
// a directory outside any repository remains quiet.
func TestSessionContextWithoutADeclaration(t *testing.T) {
	t.Parallel()
	needShell(t)
	repo := kitsutest.NewRepo(t)
	repo.Write("README.md", "# plain\n")
	repo.Git("add", "-A")
	repo.Git("commit", "-q", "-m", "first")
	outside := repo.In(t.TempDir())
	for _, h := range hostFiles {
		host := "claude"
		if h.file == codexHooks {
			host = "codex"
		}
		for _, dir := range []struct {
			name     string
			repo     *kitsutest.Repo
			workflow bool
		}{{"repository without kitsu.toml", repo, true}, {"directory outside git", outside, false}} {
			t.Run(h.host+"/"+dir.name, func(t *testing.T) {
				result := sessionStart(t, dir.repo, dir.repo.Env(), h.file)
				if result.Code != 0 {
					t.Fatalf("SessionStart exited %d:\n%s", result.Code, result.Output())
				}
				if !dir.workflow {
					if result.Stdout != "" {
						t.Errorf("context outside Git must remain quiet:\n%s", result.Output())
					}
					return
				}
				for _, want := range []string{"Kitsu verifies coding tasks", "kitsu agent check --host " + host, "no kitsu.toml declaration", "Ask before declaring checks"} {
					if !strings.Contains(result.Stdout, want) {
						t.Errorf("missing onboarding guidance %q:\n%s", want, result.Stdout)
					}
				}
				if strings.Contains(result.Stdout, "allows a commit") || repo.Exists("kitsu.toml") || repo.Exists(".git/kitsu") {
					t.Errorf("onboarding claimed gate enforcement or wrote configuration/records:\n%s", result.Output())
				}
			})
		}
	}
}
