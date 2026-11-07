package langpack

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// GoTests applies to a Go module (go.mod at the selected path). Each changed
// package (see changedPackages) needs a passed run whose argv is a direct
// `go test` invocation with a package pattern covering it (see patterns).
type GoTests struct{}

func (GoTests) Applies(source Source) bool { return slices.Contains(source.Current, "go.mod") }

func (GoTests) Problems(source Source, passed [][]string) []string {
	var problems []string
	for _, pkg := range changedPackages(source) {
		covered := slices.ContainsFunc(passed, func(argv []string) bool {
			return slices.ContainsFunc(patterns(argv), func(pattern string) bool { return covers(pattern, pkg) })
		})
		if !covered {
			problems = append(problems, fmt.Sprintf("changed package %s has no passed go test run at the current source", pkg))
		}
	}
	return problems
}

// changedPackages lists, sorted, the directories of changed or deleted
// non-test .go files that still hold a .go file. Directories that ./... skips
// (testdata, vendor, and names starting with _ or .) and directories of nested
// modules are omitted.
func changedPackages(source Source) []string {
	modules, sources := map[string]bool{}, map[string]bool{}
	for _, name := range source.Current {
		if path.Base(name) == "go.mod" {
			modules[path.Dir(name)] = true
		}
		if strings.HasSuffix(name, ".go") {
			sources[path.Dir(name)] = true
		}
	}
	packages := map[string]bool{}
	for _, name := range source.Changed {
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if dir := path.Dir(name); sources[dir] && inRootModule(dir, modules) {
			packages[dir] = true
		}
	}
	return slices.Sorted(maps.Keys(packages))
}

// inRootModule reports whether ./... at the module root matches dir.
func inRootModule(dir string, modules map[string]bool) bool {
	for _, part := range strings.Split(dir, "/") {
		if part == "testdata" || part == "vendor" || part != "." && (strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".")) {
			return false
		}
	}
	for ; dir != "."; dir = path.Dir(dir) {
		if modules[dir] {
			return false
		}
	}
	return true
}

// patterns returns the relative package patterns of a direct `go test`
// invocation (argv[0] named go, argv[1] test): each later argument before
// -args that is "." or starts with "./", cleaned. Other arguments, such as
// flags, flag values and import-path patterns, are ignored, so they never
// cover a package. Shell text is never parsed. A run that changes directory
// with -C (or --C, -C=dir) covers nothing: its patterns would be relative to
// another directory, which may lie outside the module or in a nested one.
// `go -C dir test` covers nothing either, since argv[1] is then not test.
func patterns(argv []string) []string {
	if len(argv) < 2 || strings.TrimSuffix(filepath.Base(argv[0]), ".exe") != "go" || argv[1] != "test" {
		return nil
	}
	found := []string{}
	for _, arg := range argv[2:] {
		if arg == "-args" || arg == "--args" {
			break
		}
		if name, _, _ := strings.Cut(arg, "="); name == "-C" || name == "--C" {
			return nil
		}
		if arg == "." || strings.HasPrefix(arg, "./") {
			found = append(found, path.Clean(arg))
		}
	}
	return found
}

// covers reports whether a cleaned relative pattern ("...", ".", "a/b" or
// "a/b/...") matches the package directory pkg.
func covers(pattern, pkg string) bool {
	if pattern == "..." {
		return true
	}
	if prefix, ok := strings.CutSuffix(pattern, "/..."); ok {
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	}
	return pattern == pkg
}
