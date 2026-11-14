package clamp_test

import (
	"testing"

	"example.com/clamp"
)

// Deliberately weak: covers only values strictly inside [lo, hi] (part of R1,
// never v == hi) and values below lo (R2). It never checks v == hi, v > hi or
// an inverted range, so the mutants of R1's upper edge, R3 and R4 survive it.
func TestInsideAndBelowRange(t *testing.T) {
	for _, c := range []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-3, -5, -1, -3},
		{-1, 0, 10, 0},
		{-100, 0, 10, 0},
	} {
		got, err := clamp.Int(c.v, c.lo, c.hi)
		if err != nil || got != c.want {
			t.Fatalf("Int(%d, %d, %d) = %d, %v; want %d, nil", c.v, c.lo, c.hi, got, err, c.want)
		}
	}
}
