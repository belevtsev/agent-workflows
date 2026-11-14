// Declaration extract for package clamp (bodies stripped).
package clamp

import "errors"

var ErrInvertedRange = errors.New("clamp: lo is greater than hi")

func Int(v, lo, hi int) (int, error)
