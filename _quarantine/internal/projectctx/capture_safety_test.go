package projectctx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestTrackedSourceUnderHeuristicDirectoryNamesIsIncluded(t *testing.T) {
	root := gitFixture(t)
	paths := []string{"internal/log/logger.go", "build/source.go", "target/source.go", "logs/source.go", "vendor/owned.go"}
	for _, path := range paths {
		writeFile(t, root, path, "package owned\nconst Value = 1\n")
	}
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "tracked source under directory heuristics")
	before, err := Capture(t.Context(), root)
	if err != nil || before.Dirty || before.FileCount != 2+len(paths) {
		t.Fatalf("tracked source was silently excluded: %+v %v", before, err)
	}
	for _, path := range paths {
		writeFile(t, root, path, "package owned\nconst Value = 2\n")
		after, err := Capture(t.Context(), root)
		if err != nil || !after.Dirty || before.Fingerprint == after.Fingerprint {
			t.Fatalf("tracked %s change was not bound: %+v %v", path, after, err)
		}
		writeFile(t, root, path, "package owned\nconst Value = 1\n")
	}
}

func TestMetadataInspectionNeverExecutesGitContentFilters(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("filter fixture uses the POSIX shell supplied with Unix Git")
	}
	root := gitFixture(t)
	marker := filepath.Join(t.TempDir(), "filter-executed")
	writeFile(t, root, ".gitattributes", "tracked.txt filter=review\n")
	writeFile(t, root, "tracked.txt", "original\n")
	gitCommand(t, root, "config", "filter.review.clean", fmt.Sprintf("touch %s; cat", strconv.Quote(marker)))
	gitCommand(t, root, "add", ".")
	gitCommand(t, root, "commit", "-qm", "configured local content filter")
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "tracked.txt", "modified\n")
	profile, err := Inspect(t.Context(), root)
	if err != nil || !profile.Snapshot.Dirty {
		t.Fatalf("raw changed bytes were not captured: %+v %v", profile, err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("metadata inspection executed a repository-configured content filter")
	}
}

func TestUntrackedFirstPartySourceAndNestedMarkersAreIncluded(t *testing.T) {
	root := gitFixture(t)
	before, err := Capture(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "internal/log/new.go", "package log\nconst Value = 1\n")
	after, err := Capture(t.Context(), root)
	if err != nil || !after.Dirty || before.Fingerprint == after.Fingerprint || after.FileCount != before.FileCount+1 {
		t.Fatalf("new first-party source was silently excluded: %+v %v", after, err)
	}
	for path, content := range map[string]string{"build/nested/go.mod": "module example.invalid/nested\n", "target/web/package.json": "{}", "logs/tools/pyproject.toml": "[project]\n"} {
		writeFile(t, root, path, content)
	}
	profile, err := Inspect(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"build/nested/go.mod", "target/web/package.json", "logs/tools/pyproject.toml"} {
		found := false
		for _, marker := range profile.Markers {
			found = found || marker.Path == path
		}
		if !found {
			t.Fatalf("first-party nested marker under a generic directory was hidden: %s", path)
		}
	}
}
