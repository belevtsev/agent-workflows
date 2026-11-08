package clamp_test

import (
	"errors"
	"math"
	"math/rand"
	"testing"

	"example.com/clamp"
)

// Known-good tests for clamp.Int, derived from the interface contract R1 to R5.
// Every expected value is a hand-checked literal or follows directly from the
// contract's rule; no case depends on the implementation.

// wantClamped fails unless Int(v, lo, hi) returns want and a nil error.
func wantClamped(t *testing.T, req string, v, lo, hi, want int) {
	t.Helper()
	got, err := clamp.Int(v, lo, hi)
	if err != nil {
		t.Errorf("%s: Int(%d, %d, %d) error = %v, want nil", req, v, lo, hi, err)
	}
	if got != want {
		t.Errorf("%s: Int(%d, %d, %d) = %d, want %d", req, v, lo, hi, got, want)
	}
}

// wantInverted fails unless Int(v, lo, hi) returns 0 and an error for which
// errors.Is(err, ErrInvertedRange) holds.
func wantInverted(t *testing.T, v, lo, hi int) {
	t.Helper()
	got, err := clamp.Int(v, lo, hi)
	if !errors.Is(err, clamp.ErrInvertedRange) {
		t.Errorf("R4: Int(%d, %d, %d) error = %v, want ErrInvertedRange", v, lo, hi, err)
	}
	if got != 0 {
		t.Errorf("R4: Int(%d, %d, %d) = %d, want 0", v, lo, hi, got)
	}
}

// R1 fails when a value inside [lo, hi], including either end, does not come
// back unchanged with a nil error.
func TestR1InRangeIsUnchanged(t *testing.T) {
	for _, c := range []struct{ v, lo, hi int }{
		{5, 0, 10},
		{0, 0, 10},   // v == lo
		{10, 0, 10},  // v == hi
		{-3, -5, -1}, // negative range
		{-1, -5, -1}, // v == hi, negative range
		{-5, -5, -1}, // v == lo, negative range
		{7, 7, 7},    // lo == hi == v
		{0, -1, 1},
		{math.MaxInt, 0, math.MaxInt}, // v == hi at the top of int
		{math.MinInt, math.MinInt, 0}, // v == lo at the bottom of int
		{math.MaxInt, math.MinInt, math.MaxInt},
		{math.MinInt, math.MinInt, math.MaxInt},
		{0, math.MinInt, math.MaxInt},
		{math.MaxInt - 1, math.MaxInt - 1, math.MaxInt},
		{math.MaxInt, math.MaxInt - 1, math.MaxInt},
	} {
		wantClamped(t, "R1", c.v, c.lo, c.hi, c.v)
	}
}

// R2 fails when a value below lo does not come back as lo with a nil error.
func TestR2BelowLoReturnsLo(t *testing.T) {
	for _, c := range []struct{ v, lo, hi int }{
		{-1, 0, 10},
		{-100, 0, 10},
		{4, 5, 5}, // lo == hi
		{-6, -5, -1},
		{math.MinInt, 0, 10},
		{math.MinInt, math.MinInt + 1, math.MaxInt},
		{math.MaxInt - 1, math.MaxInt, math.MaxInt},
	} {
		wantClamped(t, "R2", c.v, c.lo, c.hi, c.lo)
	}
}

// R3 fails when a value above hi does not come back as hi with a nil error.
func TestR3AboveHiReturnsHi(t *testing.T) {
	for _, c := range []struct{ v, lo, hi int }{
		{11, 0, 10},
		{1000, 0, 10},
		{6, 5, 5}, // lo == hi
		{0, -5, -1},
		{math.MaxInt, 0, 10},
		{math.MaxInt, math.MinInt, math.MaxInt - 1},
		{math.MinInt + 1, math.MinInt, math.MinInt},
	} {
		wantClamped(t, "R3", c.v, c.lo, c.hi, c.hi)
	}
}

// R4 fails when lo > hi does not give 0 and ErrInvertedRange, for any v.
func TestR4InvertedRangeIsAnError(t *testing.T) {
	for _, r := range []struct{ lo, hi int }{
		{1, 0},
		{10, 0},
		{0, -1},
		{-1, -5},
		{math.MaxInt, math.MinInt},
		{math.MinInt + 1, math.MinInt},
		{math.MaxInt, math.MaxInt - 1},
	} {
		for _, v := range []int{math.MinInt, -1000, -1, 0, 1, 5, 1000, math.MaxInt, r.lo, r.hi} {
			wantInverted(t, v, r.lo, r.hi)
		}
	}
}

// R1 to R5, exhaustive on a small domain: fails when any v in [-8, 8] and any
// lo, hi in [-6, 6] give a result other than the contract's (v inside, lo
// below, hi above, an R4 error when lo > hi), or break R5's bounds.
func TestR5ExhaustiveSmallDomain(t *testing.T) {
	const span = 6
	for lo := -span; lo <= span; lo++ {
		for hi := -span; hi <= span; hi++ {
			for v := -span - 2; v <= span+2; v++ {
				if lo > hi {
					wantInverted(t, v, lo, hi)
				} else {
					want := v
					if v < lo {
						want = lo
					} else if v > hi {
						want = hi
					}
					wantClamped(t, "R1-R3", v, lo, hi, want)
					checkR5(t, v, lo, hi)
				}
				if t.Failed() {
					return // the first failing triple is enough
				}
			}
		}
	}
}

// R5, seeded property over the whole int domain with the extremes included:
// fails when the result leaves [lo, hi], or when r == v does not hold exactly
// when lo <= v <= hi.
func TestR5PropertyFullDomain(t *testing.T) {
	rng := rand.New(rand.NewSource(20261108)) // fixed seed: deterministic
	extremes := []int{math.MinInt, math.MinInt + 1, -1, 0, 1, math.MaxInt - 1, math.MaxInt}
	pick := func() int {
		if rng.Intn(4) == 0 {
			return extremes[rng.Intn(len(extremes))]
		}
		return int(rng.Uint64())
	}
	for i := 0; i < 20000; i++ {
		v, lo, hi := pick(), pick(), pick()
		if lo > hi {
			lo, hi = hi, lo
		}
		checkR5(t, v, lo, hi)
	}
	for _, v := range extremes {
		for _, lo := range extremes {
			for _, hi := range extremes {
				if lo <= hi {
					checkR5(t, v, lo, hi)
				}
			}
		}
	}
}

// checkR5 asserts R5 for one triple with lo <= hi.
func checkR5(t *testing.T, v, lo, hi int) {
	t.Helper()
	r, err := clamp.Int(v, lo, hi)
	if err != nil {
		t.Fatalf("R5: Int(%d, %d, %d) error = %v, want nil for lo <= hi", v, lo, hi, err)
	}
	if r < lo || r > hi {
		t.Fatalf("R5: Int(%d, %d, %d) = %d, outside [lo, hi]", v, lo, hi, r)
	}
	inside := lo <= v && v <= hi
	if (r == v) != inside {
		t.Fatalf("R5: Int(%d, %d, %d) = %d; r == v is %v, want %v (v inside [lo, hi])", v, lo, hi, r, r == v, inside)
	}
}
