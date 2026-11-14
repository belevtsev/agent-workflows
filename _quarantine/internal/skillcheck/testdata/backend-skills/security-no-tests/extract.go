package unpack

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
)

// Extract is called by the upload worker for user-supplied zip archives.
func Extract(archive *zip.Reader, destination string) error {
	for _, entry := range archive.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		source, err := entry.Open()
		if err != nil {
			return err
		}
		target, err := os.Create(filepath.Join(destination, entry.Name))
		if err != nil {
			source.Close()
			return err
		}
		_, copyErr := io.Copy(target, source)
		source.Close()
		closeErr := target.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}
