package roles

import (
	"os"
	"path/filepath"
	"testing"
)

// The review of a daily task starts cheaper than the review of a design. A
// model or an effort that the repository sets still decides, and the role's
// own default, which the managed workflow uses, stays as it is.
func TestDailyReviewerStartsCheaperAndYieldsToTheRepository(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	for _, name := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "APPDATA"} {
		t.Setenv(name, home)
	}
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	resolve := func(host, id string) Model {
		t.Helper()
		got, err := ResolveSpecialist(root, host, id)
		if err != nil {
			t.Fatal(err)
		}
		return got.Model
	}
	for host, want := range map[string]Model{"claude": {"sonnet", "high"}, "codex": {"gpt-6.1-sol", "high"}} {
		if got := resolve(host, "reviewer"); got != want {
			t.Errorf("daily reviewer on %s = %+v, want %+v", host, got, want)
		}
	}
	for _, role := range All(nil) {
		managed := map[string]Model{"claude": {"opus", "max"}, "codex": {"gpt-6.1-sol", "max"}}
		if role.ID == "reviewer" && (role.Models["claude"] != managed["claude"] || role.Models["codex"] != managed["codex"]) {
			t.Errorf("the managed reviewer changed: %+v", role.Models)
		}
		if role.ID == "developer" && resolve("claude", "developer") != role.Models["claude"] {
			t.Errorf("a daily developer no longer uses the role's default")
		}
	}

	config := "version = 1\n[claude.reviewer]\nmodel = 'opus'\n[codex.reviewer]\neffort = 'max'\n"
	if err := os.WriteFile(filepath.Join(root, FileName), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	// Each field is set on its own: the other keeps the daily default.
	if got, want := resolve("claude", "reviewer"), (Model{"opus", "high"}); got != want {
		t.Errorf("a configured model = %+v, want %+v", got, want)
	}
	if got, want := resolve("codex", "reviewer"), (Model{"gpt-6.1-sol", "max"}); got != want {
		t.Errorf("a configured effort = %+v, want %+v", got, want)
	}
}
