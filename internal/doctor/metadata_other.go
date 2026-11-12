//go:build !unix

package projectctx

import "os"

func openMetadataReadOnly(path string) (*os.File, error) { return os.Open(path) }
