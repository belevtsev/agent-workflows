package langpack

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// GoFacts are the toolchain and source facts GoEvidence reads. Paths are
// slash-separated and relative to the selected path; the root package is ".".
type GoFacts struct {
	// Tested maps each package of the module at the selected path that has a
	// test binary to the sorted, distinct directories of that module's
	// packages the binary links, its own included.
	Tested map[string][]string
	// Embedded lists, sorted, the files that //go:embed directives of the
	// module's packages, their tests and their external tests match.
	Embedded []string
	// Excluded lists, sorted, the files of the module's packages that build
	// constraints exclude in the go environment of the call.
	Excluded []string
	// Constrained lists, sorted, the changed .go files outside testdata with a
	// //go:build or // +build line before the package clause.
	Constrained []string
	// Generated lists, sorted, the changed .go files outside testdata with a
	// "// Code generated ... DO NOT EDIT." line before the package clause.
	Generated []string
	// GraphError says why Tested, Embedded and Excluded were not computed;
	// "" when they were.
	GraphError string
	// NoRace is the go command's reason that -race cannot run on this host;
	// "" when it can or when that is unknown.
	NoRace string
}

// GoEvidence applies go.package-test/v2 to source, the passed runs and facts.
// It does no I/O, and equal inputs give equal outputs.
func GoEvidence(source Source, passed []Run, facts GoFacts) Evidence {
	if !slices.Contains(source.Current, "go.mod") || source.Changed == nil {
		return Evidence{}
	}
	modules, sources := goLayout(source.Current)
	packages, _ := changedWork(source)
	changed := slices.Compact(slices.Sorted(slices.Values(source.Changed)))
	runs := make([]goRun, 0, len(passed))
	for _, run := range passed {
		runs = append(runs, classify(run))
	}
	// covered reports whether a clean run, raced when raced is set, covers pkg.
	covered := func(pkg string, raced bool) bool {
		return slices.ContainsFunc(runs, func(run goRun) bool {
			return run.clean && (run.raced || !raced) && slices.ContainsFunc(run.patterns, func(pattern string) bool { return covers(pattern, pkg) })
		})
	}
	full := slices.ContainsFunc(runs, func(run goRun) bool { return run.clean && slices.Contains(run.patterns, "...") })

	var evidence Evidence
	triggered := false
	for _, name := range changed {
		reason, ok := fullRunReason(name, source, facts, modules, sources)
		if !ok {
			continue
		}
		triggered = true
		if full {
			continue
		}
		problem := fmt.Sprintf("changed %s has no passed go test ./... run at the current source", name)
		if reason != "" {
			problem += " (" + reason + ")"
		}
		evidence.Problems = append(evidence.Problems, problem)
	}
	if len(packages) == 0 && !triggered {
		return Evidence{}
	}
	graph := facts.GraphError == ""
	if !graph && len(packages) > 0 {
		evidence.Limitations = append(evidence.Limitations, "go.package-test/v2 could not compute the package graph: "+facts.GraphError)
		if !full {
			evidence.Problems = append(evidence.Problems, "changed Go packages need a passed go test ./... run at the current source because the package graph is unavailable")
		}
	}
	for _, pkg := range packages {
		switch {
		case !covered(pkg, false):
			evidence.Problems = append(evidence.Problems, fmt.Sprintf("changed package %s has no passed go test run at the current source", pkg))
		case facts.NoRace == "" && !covered(pkg, true):
			evidence.Problems = append(evidence.Problems, fmt.Sprintf("changed package %s has no passed go test -race run at the current source", pkg))
		}
	}
	if facts.NoRace != "" && len(packages) > 0 {
		evidence.Limitations = append(evidence.Limitations, fmt.Sprintf("-race cannot run on this host (%s), so plain go test runs are credited for changed packages", facts.NoRace))
	}
	if graph {
		propagating := propagatingPackages(changed, modules, sources)
		for _, tested := range slices.Sorted(maps.Keys(facts.Tested)) {
			if slices.Contains(packages, tested) || covered(tested, false) {
				continue
			}
			dependency := ""
			for _, linked := range facts.Tested[tested] {
				if propagating[linked] && (dependency == "" || linked < dependency) {
					dependency = linked
				}
			}
			if dependency != "" {
				evidence.Problems = append(evidence.Problems, fmt.Sprintf("package %s depends on changed package %s and has no passed go test run at the current source", tested, dependency))
			}
		}
		for _, name := range changed {
			if inRootModule(owner(name), modules) && slices.Contains(facts.Excluded, name) {
				evidence.Limitations = append(evidence.Limitations, fmt.Sprintf("changed %s is excluded by build constraints in the environment cw task verify uses, so no go test run there compiled it", name))
			}
		}
	}
	for _, run := range runs {
		evidence.Limitations = append(evidence.Limitations, run.limitations()...)
	}
	return evidence
}

// goRun is a passed run as go.package-test/v2 sees it.
type goRun struct {
	Run
	// patterns are credited's patterns; nil when the run is not credited.
	patterns []string
	// clean is a credited run that is not a rerun; raced and verbose are the
	// effective -race, and -v or -json, of a credited run.
	clean, raced, verbose bool
}

func classify(run Run) goRun {
	classified := goRun{Run: run, patterns: credited(run)}
	if classified.patterns == nil {
		return classified
	}
	classified.clean = !run.Rerun
	classified.raced, classified.verbose = effectiveFlags(run)
	return classified
}

// effectiveFlags returns the effective values of -race, and of -v or -json, of
// a credited run: GOFLAGS fields first, then argv flags, over the fields
// credited reads; the last occurrence wins. A bare flag is true, and an inline
// value that strconv.ParseBool rejects is false.
func effectiveFlags(run Run) (raced, verbose bool) {
	fields, _ := splitQuoted(run.GOFLAGS)
	for i := 2; i < len(run.Argv); i++ {
		if arg := run.Argv[i]; strings.HasPrefix(arg, "-") {
			fields = append(fields, arg)
			if name, _, inline := parseFlag(arg); allowedFlags[name] && !inline {
				i++ // the flag's value
			}
		}
	}
	values := map[string]bool{}
	for _, field := range fields {
		name, value, inline := parseFlag(field)
		on := true
		if inline {
			on, _ = strconv.ParseBool(value)
		}
		values[name] = on
	}
	return values["race"], values["v"] || values["json"]
}

// limitations returns a passed run's limitations: the rerun line for a rerun,
// and the skipped-test lines of a clean run.
func (run goRun) limitations() []string {
	switch {
	case run.patterns == nil:
		return nil
	case run.Rerun:
		return []string{fmt.Sprintf("check %s passed after a failed run at the same source and GOFLAGS, so it earns no Go evidence credit", run.CheckID)}
	case !run.verbose:
		return []string{run.unlisted("it ran without -v or -json")}
	case run.Log == nil:
		return []string{run.unlisted("its log could not be read")}
	}
	var lines []string
	for _, name := range skippedTests(run.Log) {
		lines = append(lines, fmt.Sprintf("check %s: test %s was skipped and did not run", run.CheckID, name))
	}
	if run.Truncated {
		lines = append(lines, run.unlisted("its log was cut at the storage limit"))
	}
	return lines
}

func (run goRun) unlisted(reason string) string {
	return fmt.Sprintf("check %s: skipped tests are not listed because %s", run.CheckID, reason)
}

var (
	skipLine   = regexp.MustCompile(`^\s*--- SKIP: (\S+)`)
	resultLine = regexp.MustCompile(`^(ok  |FAIL)\t(\S+)`)
)

// skippedTests returns the distinct skipped tests a go test log names, in
// the order of their first skip line: "<package> <test>", or "<test>" when
// the log names no package for it. A -v skip line takes the package of the
// next result line; a -json skip event names its own.
func skippedTests(log []byte) []string {
	var names []string
	var pending []int // indexes in names that wait for a result line
	for _, line := range strings.Split(string(log), "\n") {
		var event struct{ Action, Package, Test string }
		if json.Unmarshal([]byte(line), &event) == nil {
			if event.Action == "skip" && event.Test != "" {
				name := event.Test
				if event.Package != "" {
					name = event.Package + " " + name
				}
				names = append(names, name)
			}
			continue
		}
		if match := skipLine.FindStringSubmatch(line); match != nil {
			pending = append(pending, len(names))
			names = append(names, match[1])
		} else if match := resultLine.FindStringSubmatch(line); match != nil {
			for _, i := range pending {
				names[i] = match[2] + " " + names[i]
			}
			pending = pending[:0]
		}
	}
	var distinct []string
	for _, name := range names {
		if !slices.Contains(distinct, name) {
			distinct = append(distinct, name)
		}
	}
	return distinct
}

// fullRunReason reports whether a changed file needs a full run, and why:
// "" for a module file, whose problem has no reason suffix.
func fullRunReason(name string, source Source, facts GoFacts, modules, sources map[string]bool) (string, bool) {
	if slices.Contains(moduleFileNames, name) {
		return "", true
	}
	if strings.HasPrefix(name, "vendor/") && slices.Contains(source.Current, "vendor/modules.txt") {
		return "vendored dependency", true
	}
	dir := owner(name)
	switch {
	case !inRootModule(dir, modules):
		return "", false
	case slices.Contains(facts.Constrained, name):
		return "build constraint", true
	case slices.Contains(facts.Generated, name):
		return "generated code", true
	case slices.Contains(facts.Embedded, name):
		return "embedded file", true
	case inTestdata(name) && !sources[dir]:
		return "shared testdata", true
	}
	return "", false
}

// propagatingPackages returns the changed packages with a changed file that
// is neither a _test.go file nor under testdata.
func propagatingPackages(changed []string, modules, sources map[string]bool) map[string]bool {
	propagating := map[string]bool{}
	for _, name := range changed {
		if slices.Contains(moduleFileNames, name) || strings.HasSuffix(name, "_test.go") || inTestdata(name) {
			continue
		}
		if pkg, ok := packageOf(name, modules, sources); ok {
			propagating[pkg] = true
		}
	}
	return propagating
}

// goLayout returns the directories of current that hold a go.mod and those
// that hold a .go file, as changedWork reads them.
func goLayout(current []string) (modules, sources map[string]bool) {
	modules, sources = map[string]bool{}, map[string]bool{}
	for _, name := range current {
		if path.Base(name) == "go.mod" {
			modules[path.Dir(name)] = true
		}
		if strings.HasSuffix(name, ".go") {
			sources[path.Dir(name)] = true
		}
	}
	return modules, sources
}

// inTestdata reports whether a path has a testdata directory segment.
func inTestdata(name string) bool {
	return slices.Contains(strings.Split(path.Dir(name), "/"), "testdata")
}
