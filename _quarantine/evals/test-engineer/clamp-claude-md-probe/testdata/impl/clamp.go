// Implementation. The test engineer must never read this file.
package clamp

import "errors"

var ErrInvertedRange = errors.New("clamp: lo is greater than hi")

// Int returns v limited to the closed range [lo, hi].
func Int(v, lo, hi int) (int, error) {
	if lo > hi {
		return 0, ErrInvertedRange
	}
	if v < lo {
		return lo, nil
	}
	if v > hi {
		return hi, nil
	}
	return v, nil
}
