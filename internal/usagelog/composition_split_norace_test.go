//go:build !race

package usagelog

// splitTestRaceEnabled reports whether the race detector is enabled; R4d
// (TestSplitShellDeepNestingAllocations) skips when it is.
const splitTestRaceEnabled = false
