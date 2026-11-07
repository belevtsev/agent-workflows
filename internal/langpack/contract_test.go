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
//   - every changed non-test Go package needs a passed run whose argv is a
//     direct `go test` invocation with a pattern covering it: `./...`,
//     `./pkg`, a parent `./dir/...`, or `.` for the root package;
//   - otherwise the rule reports
//     `changed package <path> has no passed go test run at the current source`;
//   - test-only changes, testdata, vendor, `_`/`.` directories, nested modules
//     and deleted packages are not changed packages;
//   - arguments after -args are not patterns.

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

func contractGoProblems(t *testing.T, changed []string, passed ...[]string) []string {
	t.Helper()
	source := langpack.Source{Current: contractModule, Changed: changed}
	if !(langpack.GoTests{}).Applies(source) {
		t.Fatal("GoTests does not apply to a module with go.mod at the selected path")
	}
	return (langpack.GoTests{}).Problems(source, passed)
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
	if problems := rule.Problems(source, [][]string{}); len(problems) == 0 {
		t.Fatal("an empty passed list gives no problem")
	}
	// Any passed argv counts, whatever the language.
	if problems := rule.Problems(source, [][]string{{"make", "check"}}); len(problems) != 0 {
		t.Fatalf("a passed run still gives problems %q", problems)
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
		{"test-only change", []string{"internal/taskflow/taskflow_test.go", "internal/task/task_test.go"}, nil},
		{"testdata", []string{"internal/taskflow/testdata/fixture.go"}, nil},
		{"directory below testdata", []string{"internal/taskflow/testdata/deep/deep.go"}, nil},
		{"vendor", []string{"vendor/example.com/dep/dep.go"}, nil},
		{"underscore directory at the top", []string{"_tools/tool.go"}, nil},
		{"underscore directory below a package", []string{"internal/_gen/gen.go"}, nil},
		{"dot directory at the top", []string{".github/ci.go"}, nil},
		{"dot directory below a package", []string{"internal/.cache/cache.go"}, nil},
		{"nested module root", []string{"nested/nested.go"}, nil},
		{"package inside a nested module", []string{"nested/inner/inner.go"}, nil},
		{"nested go.mod itself", []string{"nested/go.mod"}, nil},
		{"deleted package", []string{"internal/gone/gone.go", "internal/gone/gone_test.go"}, nil},
		{"deleted file of a surviving package", []string{"pkg/c.go"}, []string{"pkg"}},
		{"non-Go file", []string{"README.md", "docs/guide.txt"}, nil},
		{"mixed", []string{"pkg/a.go", "internal/task/task_test.go", "vendor/example.com/dep/dep.go", "nested/nested.go", "main.go"}, []string{".", "pkg"}},
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
		{"-run X before the pattern", taskflow, []string{"go", "test", "-run", "X", "./internal/taskflow"}, true},
		{"-run=X before the pattern", taskflow, []string{"go", "test", "-run=X", "./internal/taskflow"}, true},
		{"several flags", taskflow, []string{"go", "test", "-count=1", "-race", "-v", "./..."}, true},
		{"one of several patterns", task, []string{"go", "test", "./pkg", "./internal/task"}, true},
		{"pattern before -args", taskflow, []string{"go", "test", "./internal/taskflow", "-args", "-v"}, true},

		// Non-covering patterns.
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
