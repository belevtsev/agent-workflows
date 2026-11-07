//go:build !unix

package procgroup

import "errors"

// Kill is unavailable without Unix process groups.
func Kill(int) error { return errors.ErrUnsupported }
