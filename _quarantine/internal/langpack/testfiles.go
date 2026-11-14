package langpack

import (
	"path"
	"slices"
	"strings"
)

// GoTestFile reports whether file is a file go test ./... at the module root
// compiles as a test or reads as test data (R10). files and file are relative to
// the module root.
func GoTestFile(files []string, file string) bool {
	if !slices.Contains(files, "go.mod") {
		return false
	}
	if file == "" || path.IsAbs(file) || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") {
		return false
	}
	modules := map[string]bool{}
	for _, name := range files {
		if path.Base(name) == "go.mod" {
			modules[path.Dir(name)] = true
		}
	}
	dir := owner(file)
	if !inRootModule(dir, modules) {
		return false
	}
	if !slices.Contains(strings.Split(path.Dir(file), "/"), "testdata") {
		base := path.Base(file)
		return strings.HasSuffix(base, "_test.go") && !strings.HasPrefix(base, "_") && !strings.HasPrefix(base, ".")
	}
	return slices.ContainsFunc(files, func(name string) bool {
		return strings.HasSuffix(name, ".go") && path.Dir(name) == dir
	})
}
