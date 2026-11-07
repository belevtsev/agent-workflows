package langpack

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// GoTests applies to a Go module (go.mod at the selected path). It maps each
// changed file to the work that must have been tested, and requires a passed
// run that covers it (see credited):
//
//   - go.mod, go.sum, go.work or go.work.sum at the module root needs a run
//     with the pattern ./... ;
//   - any other file belongs to the package of its directory or, under a
//     testdata directory, to the package holding that testdata. This includes
//     _test.go files and non-Go files such as embedded assets, .s and .c. It
//     counts when that package still holds a .go file and ./... at the module
//     root matches it: vendor, nested modules and directories named with a
//     leading _ or . are skipped, as are deleted packages.
type GoTests struct{}

func (GoTests) Applies(source Source) bool { return slices.Contains(source.Current, "go.mod") }

func (GoTests) Problems(source Source, passed []Run) []string {
	proven := [][]string{}
	for _, run := range passed {
		if patterns := credited(run); patterns != nil {
			proven = append(proven, patterns)
		}
	}
	covered := func(pkg string) bool {
		return slices.ContainsFunc(proven, func(patterns []string) bool {
			return slices.ContainsFunc(patterns, func(pattern string) bool { return covers(pattern, pkg) })
		})
	}
	packages, moduleFiles := changedWork(source)
	var problems []string
	for _, name := range moduleFiles {
		if !slices.ContainsFunc(proven, func(patterns []string) bool { return slices.Contains(patterns, "...") }) {
			problems = append(problems, fmt.Sprintf("changed %s has no passed go test ./... run at the current source", name))
		}
	}
	for _, pkg := range packages {
		if !covered(pkg) {
			problems = append(problems, fmt.Sprintf("changed package %s has no passed go test run at the current source", pkg))
		}
	}
	return problems
}

var moduleFileNames = []string{"go.mod", "go.sum", "go.work", "go.work.sum"}

// changedWork returns, sorted, the changed packages and the changed module
// files at the module root, as GoTests describes.
func changedWork(source Source) (packages, moduleFiles []string) {
	modules, sources := map[string]bool{}, map[string]bool{}
	for _, name := range source.Current {
		if path.Base(name) == "go.mod" {
			modules[path.Dir(name)] = true
		}
		if strings.HasSuffix(name, ".go") {
			sources[path.Dir(name)] = true
		}
	}
	found := map[string]bool{}
	for _, name := range source.Changed {
		if slices.Contains(moduleFileNames, name) {
			moduleFiles = append(moduleFiles, name)
			continue
		}
		if pkg := owner(name); sources[pkg] && inRootModule(pkg, modules) {
			found[pkg] = true
		}
	}
	slices.Sort(moduleFiles)
	return slices.Sorted(maps.Keys(found)), moduleFiles
}

// owner returns the package directory a file belongs to: its own directory,
// or the directory holding the first testdata directory on its path.
func owner(name string) string {
	parts := strings.Split(path.Dir(name), "/")
	if index := slices.Index(parts, "testdata"); index >= 0 {
		parts = parts[:index]
	}
	if len(parts) == 0 {
		return "."
	}
	return strings.Join(parts, "/")
}

// inRootModule reports whether ./... at the module root matches dir.
func inRootModule(dir string, modules map[string]bool) bool {
	for _, part := range strings.Split(dir, "/") {
		if part == "vendor" || part != "." && (strings.HasPrefix(part, "_") || strings.HasPrefix(part, ".")) {
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

// Flags of go test (go help testflag) and of the build (go help build), by
// whether they take a value. The test. prefix go test also accepts is removed
// before lookup.
var (
	// uncreditedFlags skip, filter or relocate the tests, so a run using any
	// of them proves nothing about a package: -c and -n build or print without
	// testing, -list lists, -exec runs another program, -run and -skip filter,
	// and -C changes the directory its patterns are relative to.
	uncreditedFlags = []string{"c", "n", "list", "exec", "run", "skip", "C"}
	valueFlags      = []string{
		"bench", "benchtime", "blockprofile", "blockprofilerate", "count", "coverpkg", "covermode", "coverprofile",
		"cpu", "cpuprofile", "fuzz", "fuzzminimizetime", "fuzztime", "memprofile", "memprofilerate", "mutexprofile",
		"mutexprofilefraction", "o", "outputdir", "parallel", "shuffle", "timeout", "trace", "vet",
		"asmflags", "buildmode", "compiler", "debug-actiongraph", "debug-runtime-trace", "debug-trace", "gccgoflags",
		"gcflags", "installsuffix", "ldflags", "mod", "modfile", "overlay", "p", "pgo", "pkgdir", "tags", "toolexec",
	}
	boolFlags = []string{
		"a", "asan", "benchmem", "buildvcs", "cover", "failfast", "fullpath", "json", "linkshared", "modcacherw",
		"msan", "race", "short", "trimpath", "v", "work", "x",
	}
)

// flagName returns the name of a flag argument ("-name", "--name",
// "-name=value") without any test. prefix, and whether it carries its value.
func flagName(arg string) (string, bool) {
	name, _, inline := strings.Cut(strings.TrimLeft(arg, "-"), "=")
	return strings.TrimPrefix(name, "test."), inline
}

// credited returns the package patterns a passed run proves, or nil when it
// proves none. Shell text is never parsed. A run counts only when:
//
//   - argv[0] is named go or go.exe (not GO.EXE) and argv[1] is test, so
//     wrappers such as make or scripts are not credited;
//   - neither argv nor the run's GOFLAGS uses an uncredited flag; and
//   - every flag in argv is known, so its value is never read as a pattern.
//
// Patterns are the other arguments before -args that are "." or start with
// "./", cleaned. Import paths and Windows-style .\pkg patterns are not
// credited. GOFLAGS set through `go env -w` rather than the environment is
// not seen.
func credited(run Run) []string {
	argv := run.Argv
	if len(argv) < 2 || strings.TrimSuffix(filepath.Base(argv[0]), ".exe") != "go" || argv[1] != "test" {
		return nil
	}
	for _, field := range strings.Fields(run.GOFLAGS) {
		if name, _ := flagName(field); strings.HasPrefix(field, "-") && slices.Contains(uncreditedFlags, name) {
			return nil
		}
	}
	found := []string{}
	for i := 2; i < len(argv); i++ {
		arg := argv[i]
		if arg == "-args" || arg == "--args" {
			break
		}
		if !strings.HasPrefix(arg, "-") {
			if arg == "." || strings.HasPrefix(arg, "./") {
				found = append(found, path.Clean(arg))
			}
			continue
		}
		name, inline := flagName(arg)
		switch {
		case slices.Contains(valueFlags, name):
			if !inline {
				i++
			}
		case slices.Contains(boolFlags, name):
		default:
			// Uncredited, or unknown and so of unknown arity.
			return nil
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
