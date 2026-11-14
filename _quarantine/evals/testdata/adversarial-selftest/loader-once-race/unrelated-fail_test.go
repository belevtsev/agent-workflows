package memo

import "testing"

// Self-test fixture (AG4): fails on the seeded source for a reason fix.patch
// does not repair, so the whole package still fails after the fix.
func TestAlways(t *testing.T) {
	t.Fatal("always")
}
