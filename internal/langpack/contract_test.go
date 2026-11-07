package langpack_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/internal/langpack"
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
//   - arguments after -args are not patterns, nor are values of value-taking
//     flags in either `-flag value` or `-flag=value` form;
//   - a run using -c, -list, -exec, -run or -skip gives no package credit.

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

// contractGoProblems runs GoTests over contractModule with each argv as a
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
	source := langpack.Source{Current: contractModule, Changed: changed}
	if !(langpack.GoTests{}).Applies(source) {
		t.Fatal("GoTests does not apply to a module with go.mod at the selected path")
	}
	return (langpack.GoTests{}).Problems(source, passed)
}

// contractModuleProblem is the contract's problem for a changed module file.
func contractModuleProblem(file string) string {
	return fmt.Sprintf("changed %s has no passed go test ./... run at the current source", file)
}

func TestContractRulesSelection(t *testing.T) {
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
			source := langpack.Source{Current: tc.current, Changed: []string{"a.go"}}
			rules := langpack.Rules(source)
			if len(rules) == 0 {
				t.Fatal("Rules returned no rules")
			}
			if _, ok := rules[0].(langpack.AnyPassedRun); !ok {
				t.Fatalf("first rule %T, want AnyPassedRun", rules[0])
			}
			hasGo := slices.ContainsFunc(rules, func(rule langpack.EvidenceRule) bool {
				_, ok := rule.(langpack.GoTests)
				return ok
			})
			if hasGo != tc.wantGo {
				t.Fatalf("GoTests selected = %v, want %v (rules %#v)", hasGo, tc.wantGo, rules)
			}
			if (langpack.GoTests{}).Applies(source) != tc.wantGo {
				t.Fatalf("GoTests.Applies = %v, want %v", !tc.wantGo, tc.wantGo)
			}
			for _, rule := range rules {
				if !rule.Applies(source) {
					t.Errorf("Rules returned %T, which does not apply", rule)
				}
			}
		})
	}
}

func TestContractAnyPassedRun(t *testing.T) {
	source := langpack.Source{Current: []string{"a.txt"}, Changed: []string{"a.txt"}}
	rule := langpack.AnyPassedRun{}
	if !rule.Applies(source) {
		t.Fatal("AnyPassedRun does not apply to a non-Go project")
	}
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
		{"non-Go file in a directory without Go files", []string{"docs/guide.txt"}, nil},
		{"vendor", []string{"vendor/example.com/dep/dep.go"}, nil},
		{"underscore directory at the top", []string{"_tools/tool.go"}, nil},
		{"underscore directory below a package", []string{"internal/_gen/gen.go"}, nil},
		{"dot directory at the top", []string{".github/ci.go"}, nil},
		{"dot directory below a package", []string{"internal/.cache/cache.go"}, nil},
		{"nested module root", []string{"nested/nested.go"}, nil},
		{"package inside a nested module", []string{"nested/inner/inner.go"}, nil},
		{"nested go.mod itself", []string{"nested/go.mod"}, nil},
		{"deleted package", []string{"internal/gone/gone.go", "internal/gone/gone_test.go"}, nil},
		{"deleted package keeping a non-Go file", []string{"internal/gone2/gone2.go", "internal/gone2/notes.md"}, nil},
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
		{"pattern before -args", taskflow, []string{"go", "test", "./internal/taskflow", "-args", "-v"}, true},

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
