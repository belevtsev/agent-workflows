package cli

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/kitsu/v3/internal/gate"
	"github.com/belevtsev/kitsu/v3/internal/store"
)

// The text a session starts with has a limit, in every state of the gate.
func TestContextTextStaysWithinItsLimit(t *testing.T) {
	t.Parallel()
	for _, hooks := range []string{gate.Installed, gate.Missing} {
		for _, mode := range []string{store.Enforce, store.Observe} {
			for _, configured := range []bool{false, true} {
				text := contextText(hooks, mode, configured)
				if n := utf8.RuneCountInString(text); n == 0 || n > MaxContext {
					t.Errorf("hooks %s, mode %s, core.hooksPath %v: %d characters, want 1 to %d", hooks, mode, configured, n, MaxContext)
				}
			}
		}
	}
}

// Where the hooks do not run, the text says so first, before what the gate
// does; where they run, it starts with what the gate does and ends with its
// state (U6).
func TestContextTextSaysFirstWhetherTheHooksRun(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		mode       string
		hooks      string
		configured bool
		start      string // what the text starts with
		holds      string // what it says further on
	}{
		{"installed", store.Enforce, gate.Installed, false, "This repository is gated by kitsu.", "The hooks are installed.\n"},
		{"observing", store.Observe, gate.Installed, false, "This repository is gated by kitsu.", "The gate only observes here"},
		{"not installed", store.Enforce, gate.Missing, false, "This repository declares checks for kitsu, but the hooks are not installed in this clone, so nothing is gated here yet; `kitsu init` installs them.", "Once they are installed, git allows a commit"},
		{"core.hooksPath", store.Enforce, gate.Missing, true, "This repository declares checks for kitsu, but the hooks git runs in this clone (core.hooksPath) do not all call kitsu, so nothing is gated here yet;", "Once they call kitsu, git allows a commit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			text := contextText(c.hooks, c.mode, c.configured)
			if !strings.HasPrefix(text, c.start) {
				t.Errorf("the text does not start with %q:\n%s", c.start, text)
			}
			if !strings.Contains(text, c.holds) {
				t.Errorf("the text does not hold %q:\n%s", c.holds, text)
			}
			if c.hooks != gate.Installed && strings.Contains(text, "The hooks are installed.") {
				t.Errorf("the text says that the hooks are installed:\n%s", text)
			}
		})
	}
}
