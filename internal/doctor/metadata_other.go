//go:build !unix

package doctor

import "os"

func openMetadataReadOnly(path string) (*os.File, error) { return os.Open(path) }
