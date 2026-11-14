package langpack_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

func evLimit(id string) string {
	return "evidence rule " + id + " is not built into this cw; only the fallback rule applied for it"
}

// A11: Evaluate is AnyPassedRun, then each distinct known rule in order; an
// unknown id, the retired go.package-test/v1 included, adds one limitation;
// dir and env are ignored for these ids, since go.package-test/v2 runs no
// command without go.mod at the selected path.
func TestEvaluate(t *testing.T) {
	goSrc := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	plain := langpack.Source{Current: []string{"x.txt"}, Changed: []string{"x.txt"}}
	ver := []langpack.Run{{Argv: []string{"go", "version"}}}
	any := langpack.AnyPassedRun{}.Problems
	if len(any(goSrc, nil)) == 0 || len(any(goSrc, ver)) != 0 {
		t.Fatal("precondition: AnyPassedRun does not report as the contract describes")
	}
	v2 := langpack.GoPackageTestV2
	noGoMod := "evidence rule " + v2 + " needs go.mod at the selected path; only the fallback rule applied for it"
	cases := []struct {
		name             string
		ids              []string
		src              langpack.Source
		passed           []langpack.Run
		problems, limits []string
	}{
		{"no ids is the fallback alone", nil, goSrc, nil, any(goSrc, nil), nil},
		{"repeated v2 applies once", []string{v2, v2}, plain, nil, any(plain, nil), []string{noGoMod}},
		{"the retired v1 is not built in", []string{"go.package-test/v1"}, goSrc, nil, any(goSrc, nil), []string{evLimit("go.package-test/v1")}},
		{"unknown ids in order, each once", []string{"go.package-test/v9", v2, "example.rule/v1", "go.package-test/v9"}, plain, ver,
			nil, []string{evLimit("go.package-test/v9"), noGoMod, evLimit("example.rule/v1")}},
	}
	for _, c := range cases {
		a := langpack.Evaluate(context.Background(), t.TempDir(), nil, c.ids, c.src, c.passed)
		b := langpack.Evaluate(context.Background(), "/nonexistent/dir", []string{"PATH=", "GOFLAGS=-bogus"}, c.ids, c.src, c.passed)
		want := fmt.Sprintf("%q %q", c.problems, c.limits)
		if got := fmt.Sprintf("%q %q", a.Problems, a.Limitations); got != want {
			t.Errorf("A11 violated (%s): got %s, want %s", c.name, got, want)
		}
		if fmt.Sprintf("%q %q", b.Problems, b.Limitations) != fmt.Sprintf("%q %q", a.Problems, a.Limitations) {
			t.Errorf("A11 violated (%s): result depends on dir or env", c.name)
		}
	}
}
