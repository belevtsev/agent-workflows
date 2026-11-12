//go:build unix

package projectctx

import (
	"os"
	"syscall"
)

func openMetadataReadOnly(path string) (*os.File, error) {
	// O_NONBLOCK closes the stat/open race for FIFOs. The caller checks the
	// opened descriptor is the same regular file before reading any bytes.
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
