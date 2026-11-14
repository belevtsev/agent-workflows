package cli

import (
	"os"
	"testing"
	"time"
)

// The session-start deadlines cw ships, recorded before TestMain raises them.
var (
	shippedSessionInputTimeout = hookSessionInputTimeout
	shippedDoctorTimeout       = hookDoctorTimeout
	shippedGateNoteTimeout     = hookGateNoteTimeout
)

// sessionTestDeadline replaces the session-start deadlines in this package's
// tests. A test that asserts the doctor summary or the cw gate install line
// asserts what session start finds, not that it finds it within 5 seconds on
// a loaded machine, so the deadlines are far above any run. A test of what
// happens when a deadline passes sets that deadline itself
// (withSessionInputTimeout). It is the longest timeout cw doctor accepts.
const sessionTestDeadline = time.Minute

func TestMain(m *testing.M) {
	hookSessionInputTimeout = sessionTestDeadline
	hookDoctorTimeout = sessionTestDeadline
	hookGateNoteTimeout = sessionTestDeadline
	os.Exit(m.Run())
}

// withSessionInputTimeout sets the session-start stdin bound to d for the
// rest of t, for a test that needs the bound to pass.
func withSessionInputTimeout(t *testing.T, d time.Duration) {
	t.Helper()
	previous := hookSessionInputTimeout
	hookSessionInputTimeout = d
	t.Cleanup(func() { hookSessionInputTimeout = previous })
}

func TestSessionStartShipsItsDeadlines(t *testing.T) {
	// Oracle: hooks/hooks.json gives SessionStart 15 seconds; cw keeps the
	// stdin read at 2 s and the gate line and the doctor summary at 5 s
	// each, so together they stay below it. TestMain raises them only for
	// the tests.
	if shippedSessionInputTimeout != 2*time.Second || shippedGateNoteTimeout != 5*time.Second || shippedDoctorTimeout != 5*time.Second {
		t.Fatalf("session-start deadlines are %s (stdin), %s (gate line), %s (doctor); want 2s, 5s, 5s",
			shippedSessionInputTimeout, shippedGateNoteTimeout, shippedDoctorTimeout)
	}
}
