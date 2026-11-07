//go:build unix && !linux && !darwin

package procgroup

// onlyDead has no member listing here, so Kill relies on ESRCH and otherwise
// reports a survivor after the bound.
func onlyDead(int) bool { return false }
