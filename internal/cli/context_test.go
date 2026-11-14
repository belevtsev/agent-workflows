package cli

import (
	"testing"
	"unicode/utf8"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/store"
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
