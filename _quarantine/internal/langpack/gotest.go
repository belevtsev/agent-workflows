package langpack

import (
	"bytes"
	"cmp"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// MaxGoTestResults bounds the distinct tests one GoTestOutput records.
	MaxGoTestResults = 50000
	// MaxGoTestResultBytes bounds the recorded tests' size (see R22).
	MaxGoTestResultBytes = 7 << 20
	// MaxGoTestLineBytes is the longest line GoTestOutput reads.
	MaxGoTestLineBytes = 64 << 10
)

// GoTestResult counts one test's results in one package. Run and Marked are
// filled only by ResultsWithRuns.
type GoTestResult struct {
	Package string `json:"package"`
	Test    string `json:"test"`
	Pass    int    `json:"pass"`
	Fail    int    `json:"fail"`
	Skip    int    `json:"skip"`
	// Run counts the test's run events.
	Run int `json:"run,omitzero"`
	// Marked counts the test's output events whose Output begins with byte 0x16.
	Marked int `json:"marked,omitzero"`
}

// GoTestResults is every test result a GoTestOutput read.
type GoTestResults struct {
	Tests     []GoTestResult `json:"tests"`
	Truncated bool           `json:"truncated"`
}

// testKey identifies one test in one package.
type testKey struct{ pkg, test string }

// testTally is the counted results: one entry per recorded (package, test),
// the size bound's running sum, and whether a bound refused a new pair.
type testTally struct {
	counts    map[testKey]GoTestResult
	size      int
	truncated bool
}

// count adds one result event, recording a new pair only within the bounds.
func (tally *testTally) count(key testKey, action string) {
	result, ok := tally.counts[key]
	if !ok {
		size := tally.size + 2*(len(key.pkg)+len(key.test)) + 128
		if len(tally.counts) >= MaxGoTestResults || size > MaxGoTestResultBytes {
			tally.truncated = true
			return
		}
		if tally.counts == nil {
			tally.counts = map[testKey]GoTestResult{}
		}
		tally.size = size
		result = GoTestResult{Package: key.pkg, Test: key.test}
	}
	switch action {
	case "pass":
		result.Pass++
	case "fail":
		result.Fail++
	case "skip":
		result.Skip++
	case "run":
		result.Run++
	case "output":
		result.Marked++
	}
	tally.counts[key] = result
}

// resultAction reports whether action is a pass, fail or skip event, which
// the results tally counts; run and marked output events go to the runs tally.
func resultAction(action string) bool {
	return action == "pass" || action == "fail" || action == "skip"
}

// GoTestOutput reads the pass, fail and skip events of go test -json output
// as it is written. The zero value is ready to use; it is not safe for
// concurrent use.
type GoTestOutput struct {
	tally testTally
	// runs counts run events and marked output events per (package, test),
	// under its own bounds (R5.b).
	runs testTally
	// line holds the current line's bytes; overlong is set once the line
	// passed the bound, and its bytes are discarded through its newline.
	line     []byte
	overlong bool
}

// Write reads p as the next bytes of go test -json output.
func (o *GoTestOutput) Write(p []byte) (int, error) {
	rest := p
	for len(rest) > 0 {
		end := bytes.IndexByte(rest, '\n')
		chunk := rest
		if end >= 0 {
			chunk = rest[:end]
		}
		// One byte beyond the bound is kept for a trailing carriage return.
		if !o.overlong {
			if len(o.line)+len(chunk) > MaxGoTestLineBytes+1 {
				o.overlong, o.line = true, o.line[:0]
			} else {
				o.line = append(o.line, chunk...)
			}
		}
		if end < 0 {
			break
		}
		if !o.overlong {
			if key, action, ok := testEvent(o.line); ok {
				if resultAction(action) {
					o.tally.count(key, action)
				} else {
					o.runs.count(key, action)
				}
			}
		}
		o.line, o.overlong = o.line[:0], false
		rest = rest[end+1:]
	}
	return len(p), nil
}

// Results reports the test results read so far. Bytes after the last newline
// are read as a final line for the returned value only.
func (o *GoTestOutput) Results() GoTestResults {
	tally := o.tally
	if !o.overlong && len(o.line) > 0 {
		if key, action, ok := testEvent(o.line); ok && resultAction(action) {
			tally.counts = maps.Clone(o.tally.counts)
			tally.count(key, action)
		}
	}
	tests := slices.Collect(maps.Values(tally.counts))
	if tests == nil {
		tests = []GoTestResult{}
	}
	slices.SortFunc(tests, func(a, b GoTestResult) int {
		return cmp.Or(strings.Compare(a.Package, b.Package), strings.Compare(a.Test, b.Test))
	})
	return GoTestResults{Tests: tests, Truncated: tally.truncated}
}

// ResultsWithRuns reports Results with each entry's Run and Marked counts (R5).
func (o *GoTestOutput) ResultsWithRuns() GoTestResults {
	results := o.Results()
	runs := o.runs
	if !o.overlong && len(o.line) > 0 {
		if key, action, ok := testEvent(o.line); ok && !resultAction(action) {
			runs.counts = maps.Clone(o.runs.counts)
			runs.count(key, action)
		}
	}
	for i, test := range results.Tests {
		counted := runs.counts[testKey{pkg: test.Package, test: test.Test}]
		results.Tests[i].Run, results.Tests[i].Marked = counted.Run, counted.Marked
	}
	results.Truncated = results.Truncated || runs.truncated
	return results
}

// testEvent reads one line (without its newline) as a go test -json pass,
// fail, skip or run event, or as an output event whose Output is a string
// beginning with byte 0x16, returned with the action "output" (R5.a). Member
// names match exactly; a duplicate member, a member of the wrong type or
// invalid UTF-8 makes the line count nothing.
func testEvent(line []byte) (testKey, string, bool) {
	line = bytes.TrimSuffix(line, []byte{'\r'})
	if len(line) == 0 || len(line) > MaxGoTestLineBytes || line[0] != '{' {
		return testKey{}, "", false
	}
	var event struct {
		Action  jsontext.Value `json:"Action"`
		Test    jsontext.Value `json:"Test"`
		Package jsontext.Value `json:"Package"`
		Output  jsontext.Value `json:"Output"`
	}
	if err := json.Unmarshal(line, &event); err != nil {
		return testKey{}, "", false
	}
	action, ok := jsonString(event.Action)
	if !ok {
		return testKey{}, "", false
	}
	switch action {
	case "pass", "fail", "skip", "run":
	case "output":
		if output, ok := jsonString(event.Output); !ok || !strings.HasPrefix(output, "\x16") {
			return testKey{}, "", false
		}
	default:
		return testKey{}, "", false
	}
	test, ok := jsonString(event.Test)
	if !ok || test == "" || strings.ContainsFunc(test, unicode.IsControl) {
		return testKey{}, "", false
	}
	pkg := ""
	if event.Package != nil {
		if pkg, ok = jsonString(event.Package); !ok || strings.ContainsFunc(pkg, unicode.IsControl) {
			return testKey{}, "", false
		}
	}
	return testKey{pkg: pkg, test: test}, action, true
}

// jsonString decodes value when it is a JSON string.
func jsonString(value jsontext.Value) (string, bool) {
	if value.Kind() != '"' {
		return "", false
	}
	var text string
	if err := json.Unmarshal(value, &text); err != nil {
		return "", false
	}
	return text, true
}

// GoTestsCredited reports whether run's per-test results may prove tests: the
// Go rule's flag rule (credited), whatever packages it names, with -json in
// argv.
func GoTestsCredited(run Run) bool {
	if credited(run) == nil {
		return false
	}
	found := false
	for i := 2; i < len(run.Argv); i++ {
		arg := run.Argv[i]
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		name, _, inline := parseFlag(arg)
		if name == "json" {
			if inline {
				return false
			}
			found = true
		}
		if allowedFlags[name] && !inline {
			i++
		}
	}
	return found
}

// testPrefixes are the function name prefixes go test runs as tests.
var testPrefixes = []string{"Test", "Fuzz", "Example"}

// GoTestName reports whether name is a test name as go test reports it.
func GoTestName(name string) bool {
	if len(name) > 1024 {
		return false
	}
	top, subtests, _ := strings.Cut(name, "/")
	if !goIdentifier(top) || !slices.ContainsFunc(testPrefixes, func(prefix string) bool {
		rest, ok := strings.CutPrefix(top, prefix)
		if !ok {
			return false
		}
		next, _ := utf8.DecodeRuneInString(rest)
		return rest == "" || !unicode.IsLower(next)
	}) {
		return false
	}
	if !strings.Contains(name, "/") {
		return true
	}
	for _, part := range strings.Split(subtests, "/") {
		if part == "" || !utf8.ValidString(part) || strings.ContainsFunc(part, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) {
			return false
		}
	}
	return true
}

// goIdentifier reports whether name is a Go identifier.
func goIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !unicode.IsLetter(r) && (i == 0 || !unicode.IsDigit(r)) {
			return false
		}
	}
	return true
}

// GoTestDeclared reports whether src declares the top-level test function of name.
func GoTestDeclared(src []byte, name string) bool {
	_, ok := declaringPackage(src, name)
	return ok
}

// declaringPackage returns src's package clause name when src parses and
// declares the top-level function, without a receiver, named the part of
// name before its first "/".
func declaringPackage(src []byte, name string) (string, bool) {
	top, _, _ := strings.Cut(name, "/")
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
	if err != nil {
		return "", false
	}
	return file.Name.Name, slices.ContainsFunc(file.Decls, func(decl ast.Decl) bool {
		function, ok := decl.(*ast.FuncDecl)
		return ok && function.Recv == nil && function.Name.Name == top
	})
}

// GoTestDeclaringPackages returns how many distinct package clauses among
// sources declare the top-level test function of name (the part before the
// first "/") without a receiver; a source that does not parse counts nothing
// (R7.a).
func GoTestDeclaringPackages(sources [][]byte, name string) int {
	packages := map[string]bool{}
	for _, src := range sources {
		if pkg, ok := declaringPackage(src, name); ok {
			packages[pkg] = true
		}
	}
	return len(packages)
}

// GoTestPackage returns the import path of the package go test ./... at the
// module root compiles file into, given the module's go.mod bytes and the
// module's current files, all relative to the module root.
func GoTestPackage(gomod []byte, current []string, file string) (string, bool) {
	module, ok := modulePath(gomod)
	if !ok || file == "" || path.IsAbs(file) || path.Clean(file) != file || file == ".." || strings.HasPrefix(file, "../") {
		return "", false
	}
	if base := path.Base(file); strings.HasPrefix(base, "_") || strings.HasPrefix(base, ".") {
		return "", false
	}
	dir := path.Dir(file)
	modules := map[string]bool{}
	for _, name := range current {
		if path.Base(name) == "go.mod" {
			modules[path.Dir(name)] = true
		}
	}
	if owner(file) != dir || !inRootModule(dir, modules) {
		return "", false
	}
	if dir == "." {
		return module, true
	}
	return module + "/" + dir, true
}

// modulePath reads the module path from the first line of gomod whose first
// field is module: module p or module "p", with an optional // comment.
func modulePath(gomod []byte) (string, bool) {
	for _, line := range strings.Split(string(gomod), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] != "module" {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module"))
		var module string
		if strings.HasPrefix(rest, `"`) || strings.HasPrefix(rest, "`") {
			quoted, err := strconv.QuotedPrefix(rest)
			if err != nil {
				return "", false
			}
			if module, err = strconv.Unquote(quoted); err != nil {
				return "", false
			}
			rest = strings.TrimSpace(rest[len(quoted):])
		} else {
			module, _, _ = strings.Cut(rest, "//")
			module, rest = strings.TrimSpace(module), ""
			if strings.ContainsFunc(module, unicode.IsSpace) {
				return "", false
			}
		}
		if module == "" || rest != "" && !strings.HasPrefix(rest, "//") {
			return "", false
		}
		return module, true
	}
	return "", false
}
