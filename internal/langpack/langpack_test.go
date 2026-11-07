package langpack

import (
	"reflect"
	"slices"
	"testing"
)

// goModule is a module with two packages, a test-only directory, a testdata
// file and a nested module.
var goModule = []string{"go.mod", "a.go", "internal/pkg/pkg.go", "internal/pkg/pkg_test.go", "internal/pkg/testdata/fixture.go", "internal/other/other.go", "nested/go.mod", "nested/nested.go"}

func TestRulesSelectsLanguageRulesByProject(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current []string
		want    []EvidenceRule
	}{
		{"Go module", goModule, []EvidenceRule{AnyPassedRun{}, GoTests{}}},
		{"go.mod below the selected path only", []string{"tools/go.mod", "tools/main.go"}, []EvidenceRule{AnyPassedRun{}}},
		{"no language pack", []string{"README.md", "main.py"}, []EvidenceRule{AnyPassedRun{}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Rules(Source{Current: tc.current}); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Rules = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestAnyPassedRunNeedsOnePassedRun(t *testing.T) {
	source := Source{Current: []string{"main.py"}, Changed: []string{"main.py"}}
	if got := (AnyPassedRun{}).Problems(source, nil); !slices.Equal(got, []string{"changed substantial task has no passed check run at the current source"}) {
		t.Fatalf("no passed run: problems %q", got)
	}
	if got := (AnyPassedRun{}).Problems(source, [][]string{{"pytest"}}); len(got) != 0 {
		t.Fatalf("one passed run: problems %q", got)
	}
}

func TestGoTestsNeedsACoveringRunPerChangedPackage(t *testing.T) {
	const pkgProblem = "changed package internal/pkg has no passed go test run at the current source"
	for _, tc := range []struct {
		name    string
		changed []string
		passed  [][]string
		want    []string
	}{
		{"run covers another package only", []string{"internal/pkg/pkg.go"}, [][]string{{"go", "test", "./internal/other"}}, []string{pkgProblem}},
		{"run covers the whole module", []string{"internal/pkg/pkg.go"}, [][]string{{"go", "test", "-count=1", "./..."}}, nil},
		{"run names the exact package", []string{"internal/pkg/pkg.go"}, [][]string{{"go", "test", "./internal/pkg/"}}, nil},
		{"run names a parent pattern", []string{"internal/pkg/pkg.go"}, [][]string{{"/usr/local/go/bin/go", "test", "./internal/..."}}, nil},
		{"no passed run", []string{"internal/pkg/pkg.go"}, nil, []string{pkgProblem}},
		{"pattern only after -args or in go vet", []string{"internal/pkg/pkg.go"}, [][]string{{"go", "test", "-args", "./..."}, {"go", "vet", "./..."}}, []string{pkgProblem}},
		{"root package", []string{"a.go"}, [][]string{{"go", "test", "./internal/..."}}, []string{"changed package . has no passed go test run at the current source"}},
		{"root package covered by .", []string{"a.go"}, [][]string{{"go", "test", "."}}, nil},
		{"deleted file of a remaining package", []string{"internal/pkg/gone.go"}, nil, []string{pkgProblem}},
		{"deleted package", []string{"internal/removed/removed.go"}, nil, nil},
		{"only a test file changed", []string{"internal/pkg/pkg_test.go"}, nil, nil},
		{"only testdata changed", []string{"internal/pkg/testdata/fixture.go"}, nil, nil},
		{"only a nested module changed", []string{"nested/nested.go"}, nil, nil},
		{"only non-Go files changed", []string{"README.md"}, nil, nil},
		{"changed files unknown", nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (GoTests{}).Problems(Source{Current: goModule, Changed: tc.changed}, tc.passed)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("problems %q, want %q", got, tc.want)
			}
		})
	}
}
