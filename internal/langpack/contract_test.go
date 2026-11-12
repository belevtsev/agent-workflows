package langpack_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// Independent black-box tests of the executed-evidence contract, written from
// the exported API and the documented contract only:
//
//   - every changed Go package needs a passed run whose argv is a direct
//     `go test` invocation with a pattern covering it: `./...`, `./pkg`, a
//     parent `./dir/...`, or `.` for the root package;
//   - otherwise the rule reports
//     `changed package <path> has no passed go test run at the current source`;
//   - a change to a _test.go file, a file under the package's testdata/ or
//     any non-Go file in a package directory changes the owning package
//     while it still has Go files; vendor, nested modules, `_`/`.`
//     directories and fully deleted packages are not changed packages;
//   - a change to go.mod, go.sum, go.work or go.work.sum needs a passed
//     `./...` run;
//   - values of value-taking flags are never patterns, in either
//     `-flag value` or `-flag=value` form;
//   - a run earns package credit only when every flag in its argv and in its
//     GOFLAGS (split like cmd/go, quotes included; `-`, `--` and `test.`
//     prefixes normalised) is on the allowlist, -count is an integer >= 1,
//     and neither -args nor --args appears;
//   - a changed non-Go file in a directory without Go files belongs to the
//     nearest ancestor package of the same module; a nested module stops the
//     walk, and no such package means no package change.

// contractModule is the current source of a module with a root package, a
// package and its sibling-prefixed neighbour, a nested package, ignored
// directories and a nested module.
var contractModule = []string{
	"go.mod",
	"main.go",
	"internal/task/task.go",
	"internal/task/task_test.go",
	"internal/taskflow/taskflow.go",
	"internal/taskflow/taskflow_test.go",
	"internal/taskflow/sub/sub.go",
	"internal/taskflow/testdata/fixture.go",
	"internal/taskflow/testdata/deep/deep.go",
	"internal/_gen/gen.go",
	"internal/.cache/cache.go",
	"_tools/tool.go",
	".github/ci.go",
	"vendor/example.com/dep/dep.go",
	"nested/go.mod",
	"nested/nested.go",
	"nested/inner/inner.go",
	"pkg/a.go",
	"pkg/b.go",
	"pkg/data.json",
	"README.md",
	"docs/guide.txt",
	"internal/gone2/notes.md",
	"e2e/e2e_test.go",
}

// contractProblemFor reports whether problems name pkg in the contract's
// message. The root package may be spelled "." or "./"; any other package
// may be spelled with or without a leading "./".
func contractProblemFor(problems []string, pkg string) bool {
	spellings := []string{pkg, "./" + pkg}
	if pkg == "." {
		spellings = []string{".", "./"}
	}
	for _, problem := range problems {
		for _, spelling := range spellings {
			if problem == fmt.Sprintf("changed package %s has no passed go test run at the current source", spelling) {
				return true
			}
		}
	}
	return false
}

// contractFacts are go.package-test/v2 facts that add nothing to the
// contract above: -race cannot run on the host, so a plain run counts, and no
// test binary links a changed package of another directory.
var contractFacts = langpack.GoFacts{NoRace: "the contract needs no -race run"}

// contractGoProblems runs the Go rule over contractModule with each argv as a
// passed run whose environment sets no GOFLAGS.
func contractGoProblems(t *testing.T, changed []string, passed ...[]string) []string {
	t.Helper()
	runs := []langpack.Run{}
	for _, argv := range passed {
		runs = append(runs, langpack.Run{Argv: argv})
	}
	return contractGoRunProblems(t, changed, runs...)
}

func contractGoRunProblems(t *testing.T, changed []string, passed ...langpack.Run) []string {
	t.Helper()
	return langpack.GoEvidence(langpack.Source{Current: contractModule, Changed: changed}, passed, contractFacts).Problems
}

// contractModuleProblem is the contract's problem for a changed module file.
func contractModuleProblem(file string) string {
	return fmt.Sprintf("changed %s has no passed go test ./... run at the current source", file)
}

// The Go rule applies only with go.mod at the selected path (oracle: a changed
// root file with no passed run is a problem there and nowhere else).
func TestContractGoRuleNeedsGoModAtTheSelectedPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current []string
		wantGo  bool
	}{
		{"go.mod at the selected path", []string{"go.mod", "a.go"}, true},
		{"no go.mod", []string{"a.go", "README.md"}, false},
		{"only a nested go.mod", []string{"sub/go.mod", "sub/a.go"}, false},
		{"go.mod in a vendor directory", []string{"vendor/x/go.mod", "a.go"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := langpack.GoEvidence(langpack.Source{Current: tc.current, Changed: []string{"a.go"}}, nil, contractFacts).Problems
			if applied := len(problems) > 0; applied != tc.wantGo {
				t.Fatalf("Go rule applied = %v, want %v (problems %q)", applied, tc.wantGo, problems)
			}
		})
	}
}

func TestContractAnyPassedRun(t *testing.T) {
	source := langpack.Source{Current: []string{"a.txt"}, Changed: []string{"a.txt"}}
	rule := langpack.AnyPassedRun{}
	if problems := rule.Problems(source, nil); len(problems) == 0 {
		t.Fatal("no passed run gives no problem")
	}
	if problems := rule.Problems(source, []langpack.Run{}); len(problems) == 0 {
		t.Fatal("an empty passed list gives no problem")
	}
	// Any passed argv counts, whatever the language or GOFLAGS.
	if problems := rule.Problems(source, []langpack.Run{{Argv: []string{"make", "check"}}}); len(problems) != 0 {
		t.Fatalf("a passed run still gives problems %q", problems)
	}
	if problems := rule.Problems(source, []langpack.Run{{Argv: []string{"go", "test", "./..."}, GOFLAGS: "-run=^$"}}); len(problems) != 0 {
		t.Fatalf("a passed filtered run still gives AnyPassedRun problems %q", problems)
	}
}

func TestContractChangedPackages(t *testing.T) {
	// No run at all, so every changed package must be reported and nothing
	// else may be.
	for _, tc := range []struct {
		name    string
		changed []string
		want    []string
	}{
		{"root package", []string{"main.go"}, []string{"."}},
		{"one package, several files, reported once", []string{"pkg/a.go", "pkg/b.go"}, []string{"pkg"}},
		{"two packages", []string{"internal/task/task.go", "internal/taskflow/taskflow.go"}, []string{"internal/task", "internal/taskflow"}},
		{"nested package is its own package", []string{"internal/taskflow/sub/sub.go"}, []string{"internal/taskflow/sub"}},
		{"test-only change", []string{"internal/taskflow/taskflow_test.go", "internal/task/task_test.go"}, []string{"internal/taskflow", "internal/task"}},
		{"deleted test file of a surviving package", []string{"internal/task/old_test.go"}, []string{"internal/task"}},
		{"package with only test files", []string{"e2e/e2e_test.go"}, []string{"e2e"}},
		{"testdata belongs to its package", []string{"internal/taskflow/testdata/fixture.go"}, []string{"internal/taskflow"}},
		{"directory below testdata belongs to the package", []string{"internal/taskflow/testdata/deep/deep.go"}, []string{"internal/taskflow"}},
		{"non-Go file in a package directory", []string{"pkg/data.json"}, []string{"pkg"}},
		{"non-Go file in the root package directory", []string{"README.md"}, []string{"."}},
		// docs holds no Go file: its nearest ancestor package is the root.
		{"non-Go file in a directory without Go files", []string{"docs/guide.txt"}, []string{"."}},
		{"vendor", []string{"vendor/example.com/dep/dep.go"}, nil},
		{"underscore directory at the top", []string{"_tools/tool.go"}, nil},
		{"underscore directory below a package", []string{"internal/_gen/gen.go"}, nil},
		{"dot directory at the top", []string{".github/ci.go"}, nil},
		{"dot directory below a package", []string{"internal/.cache/cache.go"}, nil},
		{"nested module root", []string{"nested/nested.go"}, nil},
		{"package inside a nested module", []string{"nested/inner/inner.go"}, nil},
		{"nested go.mod itself", []string{"nested/go.mod"}, nil},
		{"deleted package", []string{"internal/gone/gone.go", "internal/gone/gone_test.go"}, nil},
		// The deleted Go file changes no package; the remaining notes.md is a
		// non-Go file in a directory without Go files, so it belongs to the
		// nearest ancestor package: internal holds no Go file, so the root.
		{"deleted package, its Go files only", []string{"internal/gone2/gone2.go"}, nil},
		{"deleted package keeping a non-Go file", []string{"internal/gone2/gone2.go", "internal/gone2/notes.md"}, []string{"."}},
		{"deleted file of a surviving package", []string{"pkg/c.go"}, []string{"pkg"}},
		{"mixed", []string{"pkg/a.go", "internal/task/task_test.go", "vendor/example.com/dep/dep.go", "nested/nested.go", "main.go", "_tools/tool.go"}, []string{".", "pkg", "internal/task"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := contractGoProblems(t, tc.changed)
			if len(problems) != len(tc.want) {
				t.Fatalf("changed %q: problems %q, want one per package in %q", tc.changed, problems, tc.want)
			}
			for _, pkg := range tc.want {
				if !contractProblemFor(problems, pkg) {
					t.Errorf("changed %q: problems %q do not report package %q", tc.changed, problems, pkg)
				}
			}
		})
	}
}

func TestContractPatternCoverage(t *testing.T) {
	const (
		taskflow = "internal/taskflow/taskflow.go"
		sub      = "internal/taskflow/sub/sub.go"
		root     = "main.go"
		task     = "internal/task/task.go"
	)
	for _, tc := range []struct {
		name    string
		changed string
		argv    []string
		covered bool
	}{
		// Covering patterns.
		{"./... covers a package", taskflow, []string{"go", "test", "./..."}, true},
		{"./... covers the root package", root, []string{"go", "test", "./..."}, true},
		{"exact package", taskflow, []string{"go", "test", "./internal/taskflow"}, true},
		{"parent ./dir/... covers a nested package", sub, []string{"go", "test", "./internal/..."}, true},
		{"package's own ./pkg/... covers it", taskflow, []string{"go", "test", "./internal/taskflow/..."}, true},
		{". covers the root package", root, []string{"go", "test", "."}, true},
		{"several flags", taskflow, []string{"go", "test", "-count=1", "-race", "-v", "./..."}, true},
		// Boolean flags take no value: the next argument is a pattern.
		{"-v before the pattern", taskflow, []string{"go", "test", "-v", "./internal/taskflow"}, true},
		{"-race before the pattern", taskflow, []string{"go", "test", "-race", "./internal/taskflow"}, true},
		{"-short before the pattern", taskflow, []string{"go", "test", "-short", "./internal/taskflow"}, true},
		{"-cover before the pattern", taskflow, []string{"go", "test", "-cover", "./internal/taskflow"}, true},
		{"-json before the pattern", taskflow, []string{"go", "test", "-json", "./internal/taskflow"}, true},
		{"-failfast before the pattern", taskflow, []string{"go", "test", "-failfast", "./internal/taskflow"}, true},
		// A value flag consumes only its value.
		{"-count 1 before the pattern", taskflow, []string{"go", "test", "-count", "1", "./internal/taskflow"}, true},
		{"-timeout 10m before the pattern", taskflow, []string{"go", "test", "-timeout", "10m", "./internal/taskflow"}, true},
		{"-coverpkg ./... covers its pattern", taskflow, []string{"go", "test", "-coverpkg", "./...", "./internal/taskflow"}, true},
		{"-bench . covers its pattern", taskflow, []string{"go", "test", "-bench", ".", "./internal/taskflow"}, true},
		{"one of several patterns", task, []string{"go", "test", "./pkg", "./internal/task"}, true},
		{"-v after the pattern", taskflow, []string{"go", "test", "./internal/taskflow", "-v"}, true},

		// Non-covering patterns.
		// Values of value-taking flags are not patterns.
		{"-coverpkg ./... value is not a pattern", root, []string{"go", "test", "-coverpkg", "./...", "./pkg"}, false},
		{"-coverpkg=./... value is not a pattern", root, []string{"go", "test", "-coverpkg=./...", "./pkg"}, false},
		{"-coverpkg value does not cover a package", taskflow, []string{"go", "test", "-coverpkg", "./internal/taskflow", "./pkg"}, false},
		{"-bench . value is not the root package", root, []string{"go", "test", "-bench", ".", "./pkg"}, false},
		{"-bench=. value is not the root package", root, []string{"go", "test", "-bench=.", "./pkg"}, false},
		{"-outputdir value is not a pattern", taskflow, []string{"go", "test", "-outputdir", "./internal/taskflow", "./pkg"}, false},
		{"-coverprofile value is not a pattern", root, []string{"go", "test", "-coverprofile", ".", "./pkg"}, false},
		// Filtered or non-running runs give no credit.
		{"-run X", taskflow, []string{"go", "test", "-run", "X", "./internal/taskflow"}, false},
		{"-run=X", taskflow, []string{"go", "test", "-run=X", "./internal/taskflow"}, false},
		{"-run with ./...", taskflow, []string{"go", "test", "-run", "TestA", "./..."}, false},
		{"-run after the pattern", taskflow, []string{"go", "test", "./internal/taskflow", "-run", "X"}, false},
		{"-skip X", taskflow, []string{"go", "test", "-skip", "X", "./internal/taskflow"}, false},
		{"-skip=X", taskflow, []string{"go", "test", "-skip=X", "./internal/taskflow"}, false},
		{"-list X", taskflow, []string{"go", "test", "-list", "X", "./internal/taskflow"}, false},
		{"-list=X", taskflow, []string{"go", "test", "-list=X", "./..."}, false},
		{"-exec X", taskflow, []string{"go", "test", "-exec", "true", "./internal/taskflow"}, false},
		{"-exec=X", taskflow, []string{"go", "test", "-exec=true", "./..."}, false},
		{"-c", taskflow, []string{"go", "test", "-c", "./internal/taskflow"}, false},
		{"-c=true", taskflow, []string{"go", "test", "-c=true", "./internal/taskflow"}, false},
		{"-c among other flags", root, []string{"go", "test", "-v", "-c", "-o", "x.test", "."}, false},
		{"-n prints without running", taskflow, []string{"go", "test", "-n", "./internal/taskflow"}, false},
		{"unknown flag", taskflow, []string{"go", "test", "-frobnicate", "./internal/taskflow"}, false},
		{"unknown flag with a value", taskflow, []string{"go", "test", "-frobnicate=1", "./..."}, false},
		{"sibling prefix", taskflow, []string{"go", "test", "./internal/task"}, false},
		{"sibling prefix with ...", taskflow, []string{"go", "test", "./internal/task/..."}, false},
		{"child does not cover parent", taskflow, []string{"go", "test", "./internal/taskflow/sub"}, false},
		{"child ... does not cover parent", taskflow, []string{"go", "test", "./internal/taskflow/sub/..."}, false},
		{"exact package does not cover a nested package", sub, []string{"go", "test", "./internal/taskflow"}, false},
		{". does not cover a subpackage", taskflow, []string{"go", "test", "."}, false},
		// Any -args gives no credit, even after a covering pattern.
		{"pattern before -args", taskflow, []string{"go", "test", "./internal/taskflow", "-args", "-v"}, false},
		{"pattern only after -args", taskflow, []string{"go", "test", "-args", "./internal/taskflow"}, false},
		{"./... only after -args", taskflow, []string{"go", "test", "./pkg", "-args", "./..."}, false},
		{"go vet is not go test", taskflow, []string{"go", "vet", "./..."}, false},
		{"go build is not go test", taskflow, []string{"go", "build", "./..."}, false},
		{"go tool is not go test", taskflow, []string{"go", "tool", "test", "./..."}, false},
		{"bare go test names no pattern", taskflow, []string{"go", "test"}, false},
		{"shell wrapper is not direct", taskflow, []string{"sh", "-c", "go test ./..."}, false},
		{"env wrapper is not direct", taskflow, []string{"env", "go", "test", "./..."}, false},
		{"make is not go test", taskflow, []string{"make", "test", "./..."}, false},
		{"gotestsum is not go test", taskflow, []string{"gotestsum", "--", "./..."}, false},
		{"empty argv", taskflow, []string{}, false},
		// -C runs go in another directory, so its patterns are relative to
		// that directory. The contract does not say whether -C is resolved
		// or refused; either way ./... under -C internal must not cover the
		// root package, and ./... under the nested module covers nothing
		// of this module. (`go test -C internal ./...` passes in a module
		// whose root package test fails: the root package never ran.)
		{"-C dir ./... does not cover the root package", root, []string{"go", "test", "-C", "internal", "./..."}, false},
		{"-C=dir ./... does not cover the root package", root, []string{"go", "test", "-C=internal", "./..."}, false},
		{"-C nested-module ./... does not cover this module", taskflow, []string{"go", "test", "-C", "nested", "./..."}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := contractGoProblems(t, []string{tc.changed}, tc.argv)
			pkg := tc.changed[:max(strings.LastIndex(tc.changed, "/"), 0)]
			if pkg == "" {
				pkg = "."
			}
			if tc.covered && len(problems) != 0 {
				t.Fatalf("argv %q should cover %s; problems %q", tc.argv, pkg, problems)
			}
			if !tc.covered && !contractProblemFor(problems, pkg) {
				t.Fatalf("argv %q should not cover %s; problems %q", tc.argv, pkg, problems)
			}
		})
	}
}

// A `go test` invocation through the go tool's absolute path is still a direct
// `go test` invocation: no shell, env or other wrapper stands between the
// check and the go command. The contract's "direct" excludes wrappers, not
// spelling the executable by path.
func TestContractGoByPath(t *testing.T) {
	for _, goPath := range []string{"/usr/local/go/bin/go", "/opt/homebrew/bin/go"} {
		problems := contractGoProblems(t, []string{"internal/taskflow/taskflow.go"}, []string{goPath, "test", "./..."})
		if len(problems) != 0 {
			t.Errorf("argv [%s test ./...] does not cover the changed package; problems %q", goPath, problems)
		}
	}
	// A path whose base is not go is not the go tool.
	problems := contractGoProblems(t, []string{"internal/taskflow/taskflow.go"}, []string{"/usr/local/go/bin/gofmt", "test", "./..."})
	if !contractProblemFor(problems, "internal/taskflow") {
		t.Errorf("gofmt argv covered the package; problems %q", problems)
	}
}

func TestContractCombinedRuns(t *testing.T) {
	changed := []string{"internal/task/task.go", "internal/taskflow/taskflow.go", "pkg/a.go"}

	// Each run covers one package; together they cover all three.
	problems := contractGoProblems(t, changed,
		[]string{"go", "test", "./internal/task"},
		[]string{"go", "test", "./internal/taskflow"},
		[]string{"go", "test", "./pkg"},
	)
	if len(problems) != 0 {
		t.Fatalf("runs that together cover every package leave problems %q", problems)
	}

	// Only the uncovered package is reported; a non-test run does not help.
	problems = contractGoProblems(t, changed,
		[]string{"go", "test", "./internal/task"},
		[]string{"go", "vet", "./..."},
		[]string{"go", "test", "./pkg"},
	)
	if len(problems) != 1 || !contractProblemFor(problems, "internal/taskflow") {
		t.Fatalf("problems %q, want exactly the internal/taskflow package problem", problems)
	}

	// No changed source, no problem, even without runs.
	if problems := contractGoProblems(t, []string{}); len(problems) != 0 {
		t.Fatalf("no change gives problems %q", problems)
	}
}

// A change to a module file needs a passed `./...` run: runs that together
// cover every package one by one do not satisfy it, nor does a filtered
// `./...` run, one under -C, or ./... as a flag value or after -args.
func TestContractModuleFilesNeedFullRun(t *testing.T) {
	everyPackage := [][]string{
		{"go", "test", "."},
		{"go", "test", "./internal/..."},
		{"go", "test", "./pkg"},
		{"go", "test", "./e2e"},
	}
	for _, file := range []string{"go.mod", "go.sum", "go.work", "go.work.sum"} {
		t.Run(file, func(t *testing.T) {
			want := contractModuleProblem(file)
			if problems := contractGoProblems(t, []string{file}); !slices.Contains(problems, want) {
				t.Fatalf("a %s change without runs: problems %q lack %q", file, problems, want)
			}
			if problems := contractGoProblems(t, []string{file}, everyPackage...); !slices.Equal(problems, []string{want}) {
				t.Errorf("a %s change with per-package runs: problems %q, want only %q", file, problems, want)
			}
			for _, argv := range [][]string{
				{"go", "test", "-run", "TestA", "./..."},
				{"go", "test", "-C", "internal", "./..."},
				{"go", "vet", "./..."},
				{"go", "test", "./pkg", "-args", "./..."},
				{"go", "test", "-coverpkg", "./...", "./pkg"},
				{"go", "test", "-n", "./..."},
			} {
				if problems := contractGoProblems(t, []string{file}, argv); !slices.Contains(problems, want) {
					t.Errorf("a %s change is satisfied by %q: problems %q", file, argv, problems)
				}
			}
			if problems := contractGoRunProblems(t, []string{file}, langpack.Run{Argv: []string{"go", "test", "./..."}, GOFLAGS: "-run=^$"}); !slices.Contains(problems, want) {
				t.Errorf("a %s change is satisfied by a ./... run with GOFLAGS=-run=^$: problems %q", file, problems)
			}
			if problems := contractGoProblems(t, []string{file}, []string{"go", "test", "-count=1", "./..."}); len(problems) != 0 {
				t.Errorf("a %s change with a passed ./... run gives problems %q", file, problems)
			}
		})
	}
	// A nested module's files belong to that module.
	if problems := contractGoProblems(t, []string{"nested/go.mod", "nested/go.sum"}); len(problems) != 0 {
		t.Errorf("a nested module file change gives problems %q", problems)
	}
}

// GOFLAGS of the run's environment filters a run like its argv does.
func TestContractGOFLAGS(t *testing.T) {
	const changed = "internal/taskflow/taskflow.go"
	for _, tc := range []struct {
		goflags string
		covered bool
	}{
		{"", true},
		{"-count=1", true},
		{"-count=1 -v -race", true},
		{"-mod=mod -tags=integration", true},
		{"-run=^$", false},
		{"-run=TestA", false},
		{"-skip=TestA", false},
		{"-list=.", false},
		{"-exec=true", false},
		{"-c", false},
		{"-n", false},
		{"-count=1 -run=TestA", false},
		{"-v -skip=X -count=1", false},
	} {
		t.Run("GOFLAGS="+tc.goflags, func(t *testing.T) {
			for _, argv := range [][]string{{"go", "test", "./internal/taskflow"}, {"go", "test", "./..."}} {
				problems := contractGoRunProblems(t, []string{changed}, langpack.Run{Argv: argv, GOFLAGS: tc.goflags})
				if tc.covered && len(problems) != 0 {
					t.Errorf("argv %q with GOFLAGS %q should cover; problems %q", argv, tc.goflags, problems)
				}
				if !tc.covered && !contractProblemFor(problems, "internal/taskflow") {
					t.Errorf("argv %q with GOFLAGS %q should not cover; problems %q", argv, tc.goflags, problems)
				}
			}
		})
	}
	// A filtered run does not hide an unfiltered one of the same argv.
	problems := contractGoRunProblems(t, []string{changed},
		langpack.Run{Argv: []string{"go", "test", "./..."}, GOFLAGS: "-run=^$"},
		langpack.Run{Argv: []string{"go", "test", "./internal/taskflow"}},
	)
	if len(problems) != 0 {
		t.Errorf("an unfiltered covering run beside a filtered one leaves problems %q", problems)
	}
}

// contractCovers reports whether a passed run covers the changed package
// internal/taskflow of contractModule, and fails the test when the problems
// are neither empty nor exactly that package's problem.
func contractCovers(t *testing.T, run langpack.Run) bool {
	t.Helper()
	problems := contractGoRunProblems(t, []string{"internal/taskflow/taskflow.go"}, run)
	if len(problems) == 0 {
		return true
	}
	if len(problems) != 1 || !contractProblemFor(problems, "internal/taskflow") {
		t.Fatalf("run %q GOFLAGS %q: problems %q, want none or the internal/taskflow problem", run.Argv, run.GOFLAGS, problems)
	}
	return false
}

func contractArgv(flags ...string) []string {
	return append([]string{"go", "test"}, flags...)
}

// contractTestBinaryFlags are the allowlisted flags cmd/go passes to the test
// binary; only these keep their credit when spelled -test.<flag>.
var contractTestBinaryFlags = map[string]bool{
	"v": true, "failfast": true, "short": true, "benchmem": true, "fullpath": true,
	"artifacts": true, "count": true, "timeout": true, "cpu": true, "parallel": true,
	"bench": true, "benchtime": true, "coverprofile": true, "cpuprofile": true,
	"memprofile": true, "memprofilerate": true, "blockprofile": true,
	"blockprofilerate": true, "mutexprofile": true, "mutexprofilefraction": true,
	"trace": true, "outputdir": true, "shuffle": true,
}

// contractValueFlags maps each allowlisted value-taking flag to a value that
// earns credit.
var contractValueFlags = map[string]string{
	"shuffle": "on", "count": "1", "timeout": "10m", "covermode": "atomic",
	"coverpkg": "./...", "coverprofile": "c.out", "outputdir": "out",
	"cpu": "1,2", "p": "4", "parallel": "4", "tags": "integration",
	"mod": "mod", "pgo": "off", "bench": ".",
	"benchtime": "1x", "cpuprofile": "cpu.out", "memprofile": "mem.out",
	"memprofilerate": "1", "blockprofile": "block.out",
	"blockprofilerate": "1", "mutexprofile": "mutex.out",
	"mutexprofilefraction": "1", "trace": "trace.out", "vet": "off",
}

// Every allowlisted flag earns credit in each of its spellings: boolean flags
// take no value, so the next argument is the pattern; value flags consume
// exactly their value.
func TestContractAllowlistedFlagsCredit(t *testing.T) {
	const pkg = "./internal/taskflow"
	booleans := []string{"v", "race", "msan", "asan", "failfast", "json", "cover", "short", "trimpath", "benchmem", "x", "work", "fullpath", "artifacts", "modcacherw", "a"}
	var cases [][]string
	for _, flag := range booleans {
		cases = append(cases,
			contractArgv("-"+flag, pkg),
			contractArgv("--"+flag, pkg),
			contractArgv("-"+flag+"=true", pkg),
			contractArgv(pkg, "-"+flag),
		)
		if contractTestBinaryFlags[flag] {
			cases = append(cases, contractArgv("-test."+flag, pkg))
		}
	}
	for flag, value := range contractValueFlags {
		cases = append(cases,
			contractArgv("-"+flag, value, pkg),
			contractArgv("-"+flag+"="+value, pkg),
			contractArgv("--"+flag+"="+value, pkg),
			contractArgv(pkg, "-"+flag, value),
		)
		if contractTestBinaryFlags[flag] {
			cases = append(cases, contractArgv("-test."+flag, value, pkg), contractArgv("-test."+flag+"="+value, pkg))
		}
	}
	cases = append(cases,
		contractArgv("-buildvcs=false", pkg),
		contractArgv("-count", "3", pkg),
		contractArgv("-shuffle", "12345", pkg),
		contractArgv("-shuffle=off", pkg),
	)
	for _, argv := range cases {
		if !contractCovers(t, langpack.Run{Argv: argv}) {
			t.Errorf("allowlisted argv %q earns no credit", argv)
		}
	}
}

// Contract item 6: false-denial guards named by the contract.
func TestContractFalseDenialGuards(t *testing.T) {
	for _, argv := range [][]string{
		contractArgv("-test.v", "./internal/taskflow"),
		contractArgv("-test.count=1", "./internal/taskflow"),
		contractArgv("-artifacts", "./internal/taskflow"),
		contractArgv("-race", "-shuffle=on", "-count=1", "./..."),
		contractArgv("-v", "-race", "-count=1", "-timeout=20m", "-coverprofile=cover.out", "-covermode=atomic", "-coverpkg=./...", "./..."),
	} {
		if !contractCovers(t, langpack.Run{Argv: argv}) {
			t.Errorf("argv %q earns no credit", argv)
		}
	}
	// The module-file rule credits the same plain ./... runs.
	for _, argv := range [][]string{
		contractArgv("-race", "-shuffle=on", "-count=1", "./..."),
		contractArgv("-test.v", "./..."),
	} {
		if problems := contractGoProblems(t, []string{"go.mod"}, argv); len(problems) != 0 {
			t.Errorf("a go.mod change with %q gives problems %q", argv, problems)
		}
	}
}

// Every flag off the allowlist, a non-positive or non-integer -count and any
// -args withdraw the credit of a run that would otherwise cover.
func TestContractFlagsOutsideAllowlistDeny(t *testing.T) {
	const pkg = "./internal/taskflow"
	for _, tc := range []struct {
		name string
		argv []string
	}{
		{"-run", contractArgv("-run", "X", pkg)},
		{"-test.run", contractArgv("-test.run=X", pkg)},
		{"--run", contractArgv("--run=X", pkg)},
		{"--test.run", contractArgv("--test.run", "X", pkg)},
		{"-skip", contractArgv("-skip=X", pkg)},
		{"-test.skip", contractArgv("-test.skip", ".", pkg)},
		{"-list", contractArgv("-list", ".", pkg)},
		{"--test.list", contractArgv("--test.list=.", pkg)},
		{"-c", contractArgv("-c", pkg)},
		{"-n", contractArgv("-n", pkg)},
		{"-exec", contractArgv("-exec", "true", pkg)},
		{"-toolexec", contractArgv("-toolexec", "true", pkg)},
		{"-toolexec=", contractArgv("-toolexec=true", pkg)},
		{"-overlay", contractArgv("-overlay=overlay.json", pkg)},
		{"-modfile", contractArgv("-modfile", "alt.mod", pkg)},
		{"-fuzz", contractArgv("-fuzz=FuzzX", pkg)},
		{"-test.fuzz", contractArgv("-test.fuzz", "FuzzX", pkg)},
		{"-C", contractArgv("-C", ".", pkg)},
		// The test. prefix is accepted only for flags cmd/go passes to the
		// test binary: go test -test.race ./pkg hands ./pkg to the binary as
		// the value of -test.race and tests the current directory instead.
		{"-test.race", contractArgv("-test.race", pkg)},
		{"-test.race after the pattern", contractArgv(pkg, "-test.race")},
		{"-test.tags=x", contractArgv("-test.tags=x", pkg)},
		{"-test.a", contractArgv("-test.a", pkg)},
		{"-test.p=4", contractArgv("-test.p=4", pkg)},
		{"--test.trimpath", contractArgv("--test.trimpath", pkg)},
		{"-test.json", contractArgv("-test.json", pkg)},
		{"-test.cover", contractArgv("-test.cover", pkg)},
		{"-C=", contractArgv("-C=.", "./...")},
		// ldflags, gcflags and asmflags are off the allowlist in every form.
		{"-ldflags=", contractArgv("-ldflags=-s -w", pkg)},
		{"-ldflags value", contractArgv("-ldflags", "-X main.v=1", pkg)},
		{"--ldflags", contractArgv("--ldflags=-s", pkg)},
		{"-gcflags=", contractArgv("-gcflags=all=-N -l", pkg)},
		{"-gcflags value", contractArgv("-gcflags", "-N", pkg)},
		{"-asmflags=", contractArgv("-asmflags=all=-trimpath=x", pkg)},
		{"-asmflags value", contractArgv("-asmflags", "-I", "x", pkg)},
		{"-ldflags after the pattern", contractArgv(pkg, "-ldflags=-s")},
		{"unknown flag", contractArgv("-frobnicate", pkg)},
		{"unknown flag with a value", contractArgv("-frobnicate=1", pkg)},
		{"unknown test. flag", contractArgv("-test.frobnicate", pkg)},
		{"unknown flag after the pattern", contractArgv(pkg, "-frobnicate")},
		{"allowlisted flags beside a denied one", contractArgv("-v", "-race", "-count=1", "-run=X", pkg)},
		// -count must be an integer >= 1.
		{"-count=0", contractArgv("-count=0", pkg)},
		{"-count 0", contractArgv("-count", "0", pkg)},
		{"-test.count=0", contractArgv("-test.count=0", pkg)},
		{"--count=0", contractArgv("--count=0", pkg)},
		{"-count=0 with ./...", contractArgv("-count=0", "./...")},
		{"-count=-1", contractArgv("-count=-1", pkg)},
		{"-count=x", contractArgv("-count=x", pkg)},
		{"-count=1.5", contractArgv("-count=1.5", pkg)},
		{"-count= empty", contractArgv("-count=", pkg)},
		{"a later -count=0", contractArgv("-count=1", "-count=0", pkg)},
		// Any -args or --args.
		{"-args -test.run=^$", contractArgv(pkg, "-args", "-test.run=^$")},
		{"-args -test.count=0", contractArgv(pkg, "-args", "-test.count=0")},
		{"-args -test.skip=.", contractArgv(pkg, "-args", "-test.skip=.")},
		{"-args -test.v", contractArgv(pkg, "-args", "-test.v")},
		{"bare trailing -args", contractArgv(pkg, "-args")},
		{"--args", contractArgv(pkg, "--args", "-test.run=^$")},
		{"-args with ./...", contractArgv("-count=1", "./...", "-args", "-test.list=.")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if contractCovers(t, langpack.Run{Argv: tc.argv}) {
				t.Errorf("argv %q earns credit", tc.argv)
			}
		})
	}
}

// The test. prefix keeps credit only for flags cmd/go passes to the test
// binary; every other allowlisted flag spelled with it earns none, in argv
// and in GOFLAGS.
func TestContractTestPrefixOnlyForTestBinaryFlags(t *testing.T) {
	const pkg = "./internal/taskflow"
	booleans := []string{"v", "race", "msan", "asan", "failfast", "json", "cover", "short", "trimpath", "benchmem", "x", "work", "fullpath", "artifacts", "modcacherw", "a"}
	check := func(argv []string, goflags string, want bool) {
		t.Helper()
		if got := contractCovers(t, langpack.Run{Argv: argv, GOFLAGS: goflags}); got != want {
			t.Errorf("argv %q GOFLAGS %q: covered = %v, want %v", argv, goflags, got, want)
		}
	}
	for _, flag := range booleans {
		want := contractTestBinaryFlags[flag]
		check(contractArgv("-test."+flag, pkg), "", want)
		check(contractArgv("--test."+flag, pkg), "", want)
		check(contractArgv(pkg), "-test."+flag, want)
	}
	for flag, value := range contractValueFlags {
		want := contractTestBinaryFlags[flag]
		check(contractArgv("-test."+flag+"="+value, pkg), "", want)
		check(contractArgv("--test."+flag+"="+value, pkg), "", want)
		check(contractArgv(pkg), "'-test."+flag+"="+value+"'", want)
	}
}

// Values of value-taking flags are never patterns, whatever they look like,
// in either spelling.
func TestContractFlagValuesAreNotPatterns(t *testing.T) {
	for flag := range contractValueFlags {
		argvs := [][]string{
			contractArgv("-"+flag, "./internal/taskflow", "./pkg"),
			contractArgv("-"+flag+"=./internal/taskflow", "./pkg"),
			contractArgv("-"+flag, "./...", "./pkg"),
			contractArgv("-"+flag+"=./...", "./pkg"),
			contractArgv("--"+flag, "./...", "./pkg"),
		}
		if contractTestBinaryFlags[flag] {
			argvs = append(argvs, contractArgv("-test."+flag, "./...", "./pkg"))
		}
		for _, argv := range argvs {
			if contractCovers(t, langpack.Run{Argv: argv}) {
				t.Errorf("argv %q credits internal/taskflow through a flag value", argv)
			}
		}
		// A value of "." does not cover the root package.
		argv := contractArgv("-"+flag, ".", "./pkg")
		if problems := contractGoProblems(t, []string{"main.go"}, argv); !contractProblemFor(problems, ".") {
			t.Errorf("argv %q credits the root package through a flag value; problems %q", argv, problems)
		}
	}
}

// GOFLAGS is split like cmd/go splits it, with quoted fields, and each field
// meets the same allowlist as argv.
func TestContractGOFLAGSAllowlist(t *testing.T) {
	for _, tc := range []struct {
		goflags string
		covered bool
	}{
		{"-count=1 -v", true},
		{"  -count=1\t-v  ", true},
		{"-race -shuffle=on", true},
		{"-test.v", true},
		{"-test.count=1", true},
		{"--v --count=1", true},
		{"-tags=integration -mod=mod -trimpath", true},
		{"-modcacherw", true},
		{"-a", true},
		{"-modcacherw -a -count=1", true},
		{"-test.v -test.count=1", true},
		// test. only for flags cmd/go passes to the test binary.
		{"-test.race", false},
		{"-test.tags=x", false},
		{"-test.a", false},
		{"-test.p=4", false},
		{"--test.trimpath", false},
		{"-test.json", false},
		{"-test.cover", false},
		{"-count=1 -test.modcacherw", false},
		{"'-tags=a b' -count=1", true},
		{`"-tags=a b"`, true},
		// ldflags, gcflags and asmflags earn no credit, redacted or not.
		{"-ldflags=<redacted>", false},
		{"-ldflags=<redacted> -gcflags=<redacted> -asmflags=<redacted> -count=1", false},
		{"'-ldflags=<redacted>' -count=1", false},
		{"-ldflags=-s", false},
		{"'-ldflags=-s -w' -count=1", false},
		{"-gcflags=all=-N", false},
		{"-asmflags=x -v", false},
		// A value flag needs its value inline, and every field is a flag.
		{"-tags integration", false},
		{"-timeout 10m", false},
		{"-tags", false},
		{"-count", false},
		{"-v -count", false},
		{"count=1", false},
		{"v", false},
		{"./...", false},
		{"-v ./internal/taskflow", false},

		{"'-run=^$'", false},
		{`"-run=^$"`, false},
		{`"-test.skip=."`, false},
		{"'-test.run=X'", false},
		{"-v '-skip=X'", false},
		{`-count=1 "-list=."`, false},
		{"-frobnicate", false},
		{"-frobnicate=1", false},
		{"-v -frobnicate -count=1", false},
		{"-count=0", false},
		{"-test.count=0", false},
		{"-v -count=0", false},
		{"'-count=0'", false},
		{"-toolexec=true", false},
		{"-overlay=o.json", false},
		{"-modfile=alt.mod", false},
		{"-fuzz=FuzzX", false},
		{"-exec=true", false},
	} {
		t.Run("GOFLAGS="+tc.goflags, func(t *testing.T) {
			for _, argv := range [][]string{contractArgv("./internal/taskflow"), contractArgv("-count=1", "./...")} {
				if got := contractCovers(t, langpack.Run{Argv: argv, GOFLAGS: tc.goflags}); got != tc.covered {
					t.Errorf("argv %q with GOFLAGS %q: covered = %v, want %v", argv, tc.goflags, got, tc.covered)
				}
			}
		})
	}
	// GOFLAGS that withdraws package credit also withdraws module-file credit.
	for _, goflags := range []string{"'-run=^$'", "-count=0", "-frobnicate"} {
		run := langpack.Run{Argv: contractArgv("./..."), GOFLAGS: goflags}
		if problems := contractGoRunProblems(t, []string{"go.mod"}, run); !slices.Contains(problems, contractModuleProblem("go.mod")) {
			t.Errorf("a go.mod change is satisfied by ./... with GOFLAGS %q: problems %q", goflags, problems)
		}
	}
}

// Module-file changes: the plain ./... run must also meet the allowlist.
func TestContractModuleFilesAllowlist(t *testing.T) {
	want := contractModuleProblem("go.sum")
	for _, argv := range [][]string{
		contractArgv("-count=0", "./..."),
		contractArgv("./...", "-args", "-test.run=^$"),
		contractArgv("./...", "--args"),
		contractArgv("-toolexec=true", "./..."),
		contractArgv("-frobnicate", "./..."),
	} {
		if problems := contractGoProblems(t, []string{"go.sum"}, argv); !slices.Contains(problems, want) {
			t.Errorf("a go.sum change is satisfied by %q: problems %q", argv, problems)
		}
	}
}

// assetModule has packages with asset subdirectories that hold no Go files,
// and nested modules with their own asset directories.
var assetModule = []string{
	"go.mod",
	"main.go",
	"pkg/a.go",
	"pkg/static/index.html",
	"pkg/static/css/site.css",
	"pkg/sub/sub.go",
	"pkg/sub/assets/logo.png",
	"pkg/testdata/golden.txt",
	"docs/guide.txt",
	"nested/go.mod",
	"nested/n.go",
	"nested/web/app.js",
	"nested/deep/er/x.txt",
	"tools/nested2/go.mod",
	"tools/nested2/README.md",
}

func assetProblems(t *testing.T, current, changed []string, passed ...[]string) []string {
	t.Helper()
	runs := []langpack.Run{}
	for _, argv := range passed {
		runs = append(runs, langpack.Run{Argv: argv})
	}
	return langpack.GoEvidence(langpack.Source{Current: current, Changed: changed}, runs, contractFacts).Problems
}

// A changed non-Go file in a directory without Go files belongs to the
// nearest ancestor package within the same module.
func TestContractNonGoFileNearestAncestorPackage(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed string
		want    string // "" when no package changes
	}{
		{"asset directory of a package", "pkg/static/index.html", "pkg"},
		{"two levels below a package", "pkg/static/css/site.css", "pkg"},
		{"asset directory of a nested package", "pkg/sub/assets/logo.png", "pkg/sub"},
		{"testdata stays with its package", "pkg/testdata/golden.txt", "pkg"},
		{"top-level directory without Go files", "docs/guide.txt", "."},
		{"nested module asset", "nested/web/app.js", ""},
		{"nested module deep asset", "nested/deep/er/x.txt", ""},
		{"nested module without packages", "tools/nested2/README.md", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := assetProblems(t, assetModule, []string{tc.changed})
			if tc.want == "" {
				if len(problems) != 0 {
					t.Fatalf("changed %s: problems %q, want none", tc.changed, problems)
				}
				return
			}
			if len(problems) != 1 || !contractProblemFor(problems, tc.want) {
				t.Fatalf("changed %s: problems %q, want only package %s", tc.changed, problems, tc.want)
			}
		})
	}

	// The owning package's run covers the asset; a pattern naming the asset
	// directory, a sibling or the root package does not.
	const asset = "pkg/static/css/site.css"
	for _, tc := range []struct {
		argv    []string
		covered bool
	}{
		{contractArgv("./pkg"), true},
		{contractArgv("./pkg/..."), true},
		{contractArgv("./..."), true},
		{contractArgv("./pkg/static/..."), false},
		{contractArgv("./pkg/static"), false},
		{contractArgv("./pkg/sub"), false},
		{contractArgv("."), false},
	} {
		problems := assetProblems(t, assetModule, []string{asset}, tc.argv)
		if tc.covered && len(problems) != 0 {
			t.Errorf("changed %s with %q: problems %q", asset, tc.argv, problems)
		}
		if !tc.covered && !contractProblemFor(problems, "pkg") {
			t.Errorf("changed %s is covered by %q; problems %q", asset, tc.argv, problems)
		}
	}

	// Without an ancestor package there is no package change.
	noRoot := []string{"go.mod", "cmd/tool/main.go", "docs/guide.txt", "assets/logo.png", "README.md"}
	for _, changed := range []string{"docs/guide.txt", "assets/logo.png", "README.md"} {
		if problems := assetProblems(t, noRoot, []string{changed}); len(problems) != 0 {
			t.Errorf("changed %s in a module without a root package: problems %q", changed, problems)
		}
	}
}

// RedactGOFLAGS keeps flag names and other fields, replaces the values of
// ldflags, gcflags, asmflags and gccgoflags (inline or in the following
// field) with <redacted>, and its result earns exactly the credit the
// original would: none for a run using those flags.
func TestContractRedactGOFLAGS(t *testing.T) {
	const secret = "s3cr3t-value"
	for _, tc := range []struct {
		goflags  string
		redacted bool     // the stored form holds <redacted>
		kept     []string // fields the stored form keeps verbatim
		covered  bool
	}{
		{"", false, nil, true},
		{"-count=1 -v", false, []string{"-count=1", "-v"}, true},
		{"'-tags=a b' -count=1", false, []string{"-count=1"}, true},
		{`"-tags=a b"`, false, nil, true},
		{"-modcacherw -a -count=1", false, []string{"-modcacherw", "-a", "-count=1"}, true},
		{"-ldflags=-X=main.v=" + secret, true, nil, false},
		{"'-ldflags=-X main.v=" + secret + "' -count=1", true, []string{"-count=1"}, false},
		{`"-gcflags=all=-N -l ` + secret + `" -v`, true, []string{"-v"}, false},
		{"-asmflags=" + secret + " -race", true, []string{"-race"}, false},
		{"--ldflags=" + secret, true, nil, false},
		{"-gccgoflags=" + secret, true, nil, false},
		{"-tags=a -ldflags=" + secret, true, []string{"-tags=a"}, false},
		{"'-ldflags=-X main.v=" + secret + "' '-run=^$'", true, nil, false},
		// Bare form: the value is the following field, which must not be
		// stored verbatim.
		{"-ldflags -X=main.k=" + secret + " -count=1", false, nil, false},
		{"-gcflags " + secret, false, nil, false},
		{"--asmflags " + secret + " -v", false, nil, false},
		{"-gccgoflags " + secret, false, nil, false},
		// Every non-dash field after such a field is redacted too.
		{"-ldflags=-X main.token=" + secret + " -count=1", true, []string{"-count=1"}, false},
		// A dash field such as -l or a second -X does not end the spill: an
		// unquoted multi -X ldflags must not store the later value.
		{"-gcflags=-N -l " + secret + " -v", true, []string{"-v"}, false},
		{"-ldflags=-X main.a=1 -X main.token=" + secret + " -count=1", true, []string{"-count=1"}, false},
		{"-asmflags=-I x " + secret + " -race", true, []string{"-race"}, false},
		{"-gccgoflags=-O2 " + secret + " -count=1", true, []string{"-count=1"}, false},
		{"-tags=a -ldflags -X main.k=" + secret + " -v", false, []string{"-tags=a", "-v"}, false},
		// test.-prefixed compiler and linker flags are redacted alike.
		{"-test.gcflags " + secret, false, nil, false},
		{"--test.ldflags=-X main.k=" + secret, false, nil, false},
		{"-test.asmflags=x " + secret, false, nil, false},
		// Once redaction starts, a later field whose flag is not allowlisted
		// is redacted too; allowlisted flags are kept.
		{"-ldflags=-s '-X main.k=" + secret + "' -count=1", true, []string{"-count=1"}, false},
		{"-ldflags=-s -X=main.k=" + secret + " -v", true, []string{"-v"}, false},
		{"'-ldflags=" + secret, false, nil, false}, // unbalanced quote: cannot be split
	} {
		t.Run(tc.goflags, func(t *testing.T) {
			stored := langpack.RedactGOFLAGS(tc.goflags)
			if strings.Contains(stored, secret) {
				t.Errorf("RedactGOFLAGS(%q) = %q keeps the value", tc.goflags, stored)
			}
			if tc.goflags == "" && stored != "" {
				t.Errorf("RedactGOFLAGS(\"\") = %q, want empty", stored)
			}
			if tc.redacted && !strings.Contains(stored, "<redacted>") {
				t.Errorf("RedactGOFLAGS(%q) = %q has no <redacted> value", tc.goflags, stored)
			}
			for _, kept := range tc.kept {
				if !slices.Contains(strings.Fields(stored), kept) {
					t.Errorf("RedactGOFLAGS(%q) = %q drops %s", tc.goflags, stored, kept)
				}
			}
			for _, argv := range [][]string{contractArgv("./internal/taskflow"), contractArgv("-count=1", "./...")} {
				if got := contractCovers(t, langpack.Run{Argv: argv, GOFLAGS: stored}); got != tc.covered {
					t.Errorf("argv %q with stored GOFLAGS %q (from %q): covered = %v, want %v", argv, stored, tc.goflags, got, tc.covered)
				}
			}
		})
	}
}

// Like cmd/go, once a flag follows a package pattern, a later non-flag
// argument ends the package list and goes to the test binary: such a run
// earns no credit for any package. A flag's value is not such an argument.
func TestContractPackageListEnds(t *testing.T) {
	const (
		taskflow = "internal/taskflow/taskflow.go"
		pkgFile  = "pkg/a.go"
	)
	changed := []string{taskflow, pkgFile}
	for _, tc := range []struct {
		name    string
		argv    []string
		covered bool
	}{
		{"flag between patterns", contractArgv("./internal/taskflow", "-v", "./pkg"), false},
		{"value flag between patterns", contractArgv("./internal/taskflow", "-count=1", "./pkg"), false},
		{"-race between patterns", contractArgv("./pkg", "-race", "./internal/taskflow"), false},
		{"separate value flag between patterns", contractArgv("./internal/taskflow", "-count", "1", "./pkg"), false},
		{"./... then a flag then a pattern", contractArgv("./...", "-v", "./pkg"), false},
		{"leading flag, then split", contractArgv("-v", "./internal/taskflow", "-race", "./pkg"), false},
		{"non-pattern word after a flag", contractArgv("./internal/taskflow", "./pkg", "-v", "extra"), false},
		{"flags after both patterns", contractArgv("./internal/taskflow", "./pkg", "-v"), true},
		{"flags before both patterns", contractArgv("-v", "./internal/taskflow", "./pkg"), true},
		{"flags around both patterns", contractArgv("-race", "./internal/taskflow", "./pkg", "-count=1", "-v"), true},
		{"separate values after the patterns", contractArgv("./internal/taskflow", "./pkg", "-timeout", "10m", "-count", "1"), true},
		{"-bench . after the patterns", contractArgv("./internal/taskflow", "./pkg", "-bench", ".", "-benchtime", "1x"), true},
		{"./... with trailing flags", contractArgv("./...", "-count=1", "-shuffle", "on"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := contractGoProblems(t, changed, tc.argv)
			if tc.covered && len(problems) != 0 {
				t.Fatalf("argv %q should cover both packages; problems %q", tc.argv, problems)
			}
			if !tc.covered && (!contractProblemFor(problems, "internal/taskflow") || !contractProblemFor(problems, "pkg") || len(problems) != 2) {
				t.Fatalf("argv %q should cover neither package; problems %q", tc.argv, problems)
			}
		})
	}
	// A split ./... run does not satisfy a module-file change.
	if problems := contractGoProblems(t, []string{"go.mod"}, contractArgv("./...", "-v", "./pkg")); !slices.Contains(problems, contractModuleProblem("go.mod")) {
		t.Errorf("a go.mod change is satisfied by a split ./... run: problems %q", problems)
	}
}

// contractAllowlist is every flag name the contract allowlists, with a value
// that earns credit ("" for a boolean flag).
var contractAllowlist = map[string]string{
	"v": "", "race": "", "msan": "", "asan": "", "failfast": "", "json": "",
	"cover": "", "short": "", "trimpath": "", "benchmem": "", "x": "",
	"work": "", "fullpath": "", "artifacts": "", "modcacherw": "", "a": "",
	"buildvcs": "false", "shuffle": "on", "count": "1", "timeout": "10m", "covermode": "atomic",
	"coverpkg": "./...", "coverprofile": "c.out", "outputdir": "out",
	"cpu": "1,2", "p": "4", "parallel": "4", "tags": "integration",
	"mod": "mod", "pgo": "off", "bench": ".", "benchtime": "1x",
	"cpuprofile": "cpu.out", "memprofile": "mem.out", "memprofilerate": "1",
	"blockprofile": "block.out", "blockprofilerate": "1",
	"mutexprofile": "mutex.out", "mutexprofilefraction": "1",
	"trace": "trace.out", "vet": "off",
}

// Drift guard: the flags that keep credit as -test.<flag> are exactly the
// allowlisted flags the installed go command passes to the test binary, as
// listed by passFlagToTest in cmd/go/internal/test/flagdefs.go.
func TestContractTestPrefixMatchesFlagdefs(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Skip("no go command on PATH")
	}
	output, err := exec.CommandContext(t.Context(), goTool, "env", "GOROOT").Output()
	if err != nil {
		t.Skipf("go env GOROOT: %v", err)
	}
	path := filepath.Join(strings.TrimSpace(string(output)), "src", "cmd", "go", "internal", "test", "flagdefs.go")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no flagdefs.go to compare with: %v", err)
	}
	block := regexp.MustCompile(`(?s)var passFlagToTest = map\[string\]bool\{(.*?)\n\}`).FindSubmatch(data)
	if block == nil {
		t.Fatalf("%s has no passFlagToTest map", path)
	}
	passed := map[string]bool{}
	for _, match := range regexp.MustCompile(`"([a-z]+)":\s*true`).FindAllSubmatch(block[1], -1) {
		passed[string(match[1])] = true
	}
	if len(passed) == 0 {
		t.Fatalf("parsed no names from passFlagToTest in %s", path)
	}
	for name, value := range contractAllowlist {
		flag, plain := "-test."+name, "-"+name
		if value != "" {
			flag += "=" + value
			plain += "=" + value
		}
		got := contractCovers(t, langpack.Run{Argv: contractArgv(flag, "./internal/taskflow")})
		if want := passed[name]; got != want {
			t.Errorf("%s ./internal/taskflow: covered = %v, want %v (passFlagToTest holds %s: %v)", flag, got, want, name, want)
		}
		// The plain spelling of every allowlisted flag credits.
		if !contractCovers(t, langpack.Run{Argv: contractArgv(plain, "./internal/taskflow")}) {
			t.Errorf("allowlisted %s earns no credit", plain)
		}
	}
	// The contract's own list of test-binary flags agrees with flagdefs.
	for name := range contractTestBinaryFlags {
		if !passed[name] {
			t.Errorf("contractTestBinaryFlags names %s, which passFlagToTest does not", name)
		}
	}
}
