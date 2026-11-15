package repocheck

import (
	"bytes"
	"go/format"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CI refuses a Go file that gofmt would change. This test holds the same
// rule inside `go test`, with the formatter of the Go that runs the test, so
// that the checks of kitsu.toml find such a file before a push does: a gofmt of
// another Go release, first on PATH, can format differently.
func TestGoFilesAreFormatted(t *testing.T) {
	for _, file := range repoFiles(t) {
		if !strings.HasSuffix(file, ".go") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(repoRoot, file))
		if err != nil {
			t.Fatal(err)
		}
		formatted, err := format.Source(data)
		if err != nil {
			t.Errorf("%s: %v", file, err)
			continue
		}
		if !bytes.Equal(formatted, data) {
			t.Errorf("%s is not formatted as the gofmt of this Go formats it; run `go fmt` on its package", file)
		}
	}
}
