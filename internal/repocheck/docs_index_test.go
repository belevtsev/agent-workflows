package repocheck

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The index of the documents names every document, so that none is written
// and then never found.
func TestTheIndexNamesEveryDocument(t *testing.T) {
	t.Parallel()
	const index = "docs/README.md"
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(index)))
	if err != nil {
		t.Fatal(err)
	}
	targets := markdownTargets(data)
	documents := 0
	for _, file := range linkCheckedDocuments(t) {
		name, inDocs := strings.CutPrefix(file, "docs/")
		if !inDocs || file == index {
			continue
		}
		documents++
		if !slices.Contains(targets, name) {
			t.Errorf("%s has no link to %s", index, file)
		}
	}
	if documents == 0 {
		t.Fatal("no document was found under docs/")
	}
}
