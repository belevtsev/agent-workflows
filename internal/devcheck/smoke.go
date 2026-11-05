package devcheck

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ExtractBinary obtains the already-built native cw from its verified release
// archive. Only the single regular executable is materialized.
func ExtractBinary(archive, destination string) (err error) {
	file, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer file.Close()
	zipped, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer zipped.Close()
	reader := tar.NewReader(zipped)
	var executable []byte
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if header.Name != "cw" {
			continue
		}
		if executable != nil || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 128<<20 {
			return errors.New("release archive has an invalid cw executable")
		}
		executable, err = io.ReadAll(io.LimitReader(reader, header.Size+1))
		if err != nil || int64(len(executable)) != header.Size {
			return errors.Join(errors.New("release executable size differs"), err)
		}
	}
	if executable == nil {
		return errors.New("release archive is missing cw")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, executable, 0o755)
}
