package langpack

import (
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// GoTests applies to a Go module (go.mod at the selected path). It maps each
// changed file to the work that must have been tested, and requires a passed
// run that covers it (see credited):
//
//   - go.mod, go.sum, go.work or go.work.sum at the module root needs a run
//     with the pattern ./... ;
//   - any other file belongs to a package (see packageOf). This includes
//     _test.go files, files under testdata, and non-Go files such as embedded
//     assets, .s and .c.
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
		if pkg, ok := packageOf(name, modules, sources); ok {
			found[pkg] = true
		}
	}
	slices.Sort(moduleFiles)
	return slices.Sorted(maps.Keys(found)), moduleFiles
}

// packageOf returns the package a changed file belongs to. The file's
// directory, or for a file under testdata the directory holding that
// testdata, must be matched by ./... at the module root: vendor, nested
// modules and directories named with a leading _ or . belong to no package.
// A .go file belongs to that directory when it still holds a .go file, so a
// fully deleted package is no change. Any other file belongs to the nearest
// directory, from there up to the module root, that holds a .go file, so an
// asset in pkg/static belongs to pkg; with none it belongs to no package.
func packageOf(name string, modules, sources map[string]bool) (string, bool) {
	dir := owner(name)
	if !inRootModule(dir, modules) {
		return "", false
	}
	if strings.HasSuffix(name, ".go") {
		return dir, sources[dir]
	}
	for !sources[dir] {
		if dir == "." {
			return "", false
		}
		dir = path.Dir(dir)
	}
	return dir, true
}

// owner returns a file's directory, or the directory holding the first
// testdata directory on its path.
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

// allowedFlags are the go test and build flags (go help testflag, go help
// build) that cannot skip, filter or replace the tests a run executes, mapped
// to whether they take a value. A run using any other flag earns no credit:
// among them run, skip, list, c, n, o, exec, toolexec, overlay, modfile,
// fuzz, fuzztime, fuzzminimizetime, C, every flag this list does not know,
// and ldflags, gcflags and asmflags, whose arguments can replace the test
// binary (for example -ldflags='-linkmode=external -extld=/fake'). a and
// modcacherw only force rebuilds or change module cache permissions. count
// must be an integer of at least 1, since -count=0 runs nothing.
var allowedFlags = map[string]bool{
	// Flags without a value.
	"v": false, "race": false, "msan": false, "asan": false, "failfast": false, "json": false, "cover": false,
	"short": false, "trimpath": false, "buildvcs": false, "benchmem": false, "artifacts": false, "fullpath": false,
	"x": false, "work": false, "a": false, "modcacherw": false,
	// Flags with a value.
	"shuffle": true, "count": true, "timeout": true, "covermode": true, "coverpkg": true, "coverprofile": true,
	"outputdir": true, "cpu": true, "p": true, "parallel": true, "tags": true, "mod": true, "pgo": true,
	"bench": true, "benchtime": true, "cpuprofile": true, "memprofile": true, "memprofilerate": true,
	"blockprofile": true, "blockprofilerate": true, "mutexprofile": true, "mutexprofilefraction": true,
	"trace": true, "vet": true,
}

// testPrefixed are the flags go test also accepts with a test. prefix: its
// passFlagToTest set (cmd/go/internal/test/flagdefs.go, go 1.27). Any other
// -test.X is unknown to go test, so it ends the package list.
var testPrefixed = []string{
	"artifacts", "bench", "benchmem", "benchtime", "blockprofile", "blockprofilerate", "count", "coverprofile",
	"cpu", "cpuprofile", "failfast", "fullpath", "fuzz", "fuzzminimizetime", "fuzztime", "list", "memprofile",
	"memprofilerate", "mutexprofile", "mutexprofilefraction", "outputdir", "parallel", "run", "short", "shuffle",
	"skip", "timeout", "trace", "v",
}

// parseFlag splits a flag argument ("-name", "--name", "-name=value") into its
// name, inline value and whether it has one. A test. prefix is removed only
// from a name in testPrefixed; otherwise it stays, so the name is unknown.
func parseFlag(arg string) (name, value string, inline bool) {
	name, value, inline = strings.Cut(strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"), "=")
	if bare, ok := strings.CutPrefix(name, "test."); ok && slices.Contains(testPrefixed, bare) {
		name = bare
	}
	return name, value, inline
}

// allowedValue reports whether an allowlisted flag's value is acceptable.
func allowedValue(name, value string) bool {
	if name != "count" {
		return true
	}
	count, err := strconv.Atoi(value)
	return err == nil && count >= 1
}

// credited returns the package patterns a passed run proves, or nil when it
// proves none. Shell text is never parsed. A run counts only when:
//
//   - argv[0] is named go or go.exe (not GO.EXE) and argv[1] is test, so
//     wrappers such as make or scripts are not credited;
//   - argv has no -args, since flags after it reach the test binary;
//   - no non-flag argument follows a flag that itself followed a non-flag
//     argument: cmd/go ends the package list there and hands the rest to the
//     test binary, so `go test ./a -v ./b` tests only ./a;
//   - every flag in argv and every field of its GOFLAGS (split like cmd/go,
//     see splitQuoted; a field must be a flag with any value inline) is in
//     allowedFlags with an acceptable value.
//
// Patterns are the non-flag arguments that are "." or start with "./",
// cleaned. Import paths and Windows-style .\pkg patterns are not credited.
// GOFLAGS set through `go env -w` rather than the environment is not seen.
//
// The flag sets target go 1.27, the version this repository targets. A
// local toolchain that does not know an allowlisted flag ends the package
// list at it, so a flag before the patterns makes go test only ".": such a
// run is still credited.
func credited(run Run) []string {
	argv := run.Argv
	if len(argv) < 2 || strings.TrimSuffix(filepath.Base(argv[0]), ".exe") != "go" || argv[1] != "test" {
		return nil
	}
	fields, err := splitQuoted(run.GOFLAGS)
	if err != nil {
		return nil
	}
	for _, field := range fields {
		name, value, inline := parseFlag(field)
		takesValue, known := allowedFlags[name]
		if !strings.HasPrefix(field, "-") || !known || takesValue && !inline || !allowedValue(name, value) {
			return nil
		}
	}
	found := []string{}
	positional, flagAfterPositional := false, false
	for i := 2; i < len(argv); i++ {
		arg := argv[i]
		if !strings.HasPrefix(arg, "-") {
			if flagAfterPositional {
				return nil
			}
			positional = true
			if arg == "." || strings.HasPrefix(arg, "./") {
				found = append(found, path.Clean(arg))
			}
			continue
		}
		flagAfterPositional = positional
		name, value, inline := parseFlag(arg)
		takesValue, known := allowedFlags[name]
		if !known {
			return nil // includes args, run, skip, ldflags and unknown flags
		}
		if takesValue && !inline {
			if i++; i >= len(argv) {
				return nil
			}
			value = argv[i]
		}
		if !allowedValue(name, value) {
			return nil
		}
	}
	return found
}

// splitQuoted splits s into fields like cmd/go splits GOFLAGS
// (cmd/internal/quoted.Split): fields are separated by spaces, tabs and
// newlines, and a field may be wrapped in single or double quotes, with no
// unescaping inside. An unterminated quote is an error.
func splitQuoted(s string) ([]string, error) {
	var fields []string
	for {
		s = strings.TrimLeft(s, " \t\n\r")
		if s == "" {
			return fields, nil
		}
		if quote := s[0]; quote == '"' || quote == '\'' {
			end := strings.IndexByte(s[1:], quote)
			if end < 0 {
				return nil, fmt.Errorf("unterminated %c string", quote)
			}
			fields = append(fields, s[1:1+end])
			s = s[2+end:]
			continue
		}
		end := strings.IndexAny(s, " \t\n\r")
		if end < 0 {
			end = len(s)
		}
		fields = append(fields, s[:end])
		s = s[end:]
	}
}

// redactedFlags carry compiler and linker arguments, such as -X values,
// that may hold secrets.
var redactedFlags = []string{"ldflags", "gcflags", "asmflags", "gccgoflags"}

// unparsableGOFLAGS replaces GOFLAGS that RedactGOFLAGS cannot rewrite; its
// fields are not flags, so it earns no credit.
const unparsableGOFLAGS = "<redacted: unparsable GOFLAGS>"

// RedactGOFLAGS returns GOFLAGS for storage: each field keeps its flag name
// and value, except that the value of ldflags, gcflags, asmflags and
// gccgoflags, with or without a test. prefix, becomes <redacted>, whether
// inline (-ldflags=v) or in the next field (-ldflags v). After the first such
// flag, every later field that is not an allowedFlags flag becomes <redacted>
// as well (-ldflags=-X main.token=v, -ldflags=-s -X=main.k=v). Redacted
// flags and fields earn no credit before or after redaction, so it never
// changes the rule's result. Fields are re-joined with spaces, and a field
// holding a space or quote is wrapped in quotes, so credited reads the result
// as it would the original. GOFLAGS that cannot be split, or holding a field
// with both quote kinds that cannot be re-quoted, is stored as
// unparsableGOFLAGS, which earns no credit.
func RedactGOFLAGS(goflags string) string {
	fields, err := splitQuoted(goflags)
	if err != nil {
		return unparsableGOFLAGS
	}
	joined := make([]string, 0, len(fields))
	// redactNext marks the bare value of a redacted flag; redacting is set
	// once a redacted flag is seen and redacts every later field that is not
	// an allowlisted flag.
	redactNext, redacting := false, false
	for _, field := range fields {
		raw, _, inline := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(field, "-"), "-"), "=")
		name, _, _ := parseFlag(field)
		_, allowed := allowedFlags[name]
		switch {
		case redactNext:
			field, redactNext = "<redacted>", false
		case strings.HasPrefix(field, "-") && slices.Contains(redactedFlags, strings.TrimPrefix(raw, "test.")):
			redacting = true
			if inline {
				field = field[:strings.IndexByte(field, '=')+1] + "<redacted>"
			} else {
				redactNext = true
			}
		case redacting && !(strings.HasPrefix(field, "-") && allowed):
			field = "<redacted>"
		}
		switch {
		case !strings.ContainsAny(field, " \t\n\r'\""):
		case !strings.Contains(field, "'"):
			field = "'" + field + "'"
		case !strings.Contains(field, `"`):
			field = `"` + field + `"`
		default:
			return unparsableGOFLAGS
		}
		joined = append(joined, field)
	}
	return strings.Join(joined, " ")
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
