package langpack_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// B1, J5: Evaluate knows go.package-test/v2: without go.mod it adds the
// fallback limitation, with no changed file it adds nothing, and in either
// case runs no command; otherwise it appends GoEvidence over LoadGoFacts with
// its own ctx (B1.a: a 6 s graph within a 2-minute deadline is not cut off).
// go.package-test/v1 still runs GoTests and no command.
func TestEvaluateV2B1(t *testing.T) {
	gfPosix(t)
	v2 := "go.package-test/v2"
	if !langpack.KnownRule(v2) {
		t.Errorf("B1 violated: KnownRule(%q) is false", v2)
	}
	rec := filepath.Join(gfDir(t), "rec")
	fake := gfFake(t, gfRecord(rec)+"case \" $* \" in *\" -deps \"*) /bin/sleep 6;; esac\nexit 0\n")
	env := gfEnv("PATH=" + fake)
	dir := gfModule(t, map[string]string{"go.mod": "module example.com/m\n\ngo 1.21\n", "a/a.go": "package a\n"})
	plain := []langpack.Run{{CheckID: "unit", Argv: []string{"go", "test", "./a"}}}
	ran := func() bool { _, err := os.Stat(rec); return err == nil }

	noMod := langpack.Evaluate(gfCtx(t), dir, env, []string{v2}, langpack.Source{Current: []string{"a/a.go"}, Changed: []string{"a/a.go"}}, plain)
	want := "evidence rule go.package-test/v2 needs go.mod at the selected path; only the fallback rule applied for it"
	if len(noMod.Problems) != 0 || !slices.Equal(noMod.Limitations, []string{want}) || ran() {
		t.Errorf("B1 violated (no go.mod): %+v, go ran: %v; want only the limitation %q and no command", noMod, ran(), want)
	}
	for _, changed := range [][]string{nil, {}} {
		got := langpack.Evaluate(gfCtx(t), dir, env, []string{v2}, langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: changed}, plain)
		if len(got.Problems) != 0 || len(got.Limitations) != 0 || ran() {
			t.Errorf("B1 violated (Changed %#v): %+v, go ran: %v; want nothing and no command", changed, got, ran())
		}
	}

	// J5: a v1 task keeps GoTests and runs no command.
	goSrc := langpack.Source{Current: []string{"go.mod", "a/a.go"}, Changed: []string{"a/a.go"}}
	v1 := langpack.Evaluate(gfCtx(t), dir, env, []string{langpack.GoPackageTestV1}, goSrc, nil)
	wantV1 := append(langpack.AnyPassedRun{}.Problems(goSrc, nil), langpack.GoTests{}.Problems(goSrc, nil)...)
	if !slices.Equal(v1.Problems, wantV1) || len(v1.Limitations) != 0 || ran() {
		t.Errorf("J5 violated: v1 gives %+v (go ran: %v), want problems %q", v1, ran(), wantV1)
	}

	// B1.a: the graph command sleeps 6 s, then prints nothing and exits 0.
	got := langpack.Evaluate(gfCtx(t), dir, env, []string{v2}, goSrc, plain)
	if !slices.Equal(got.Problems, []string{geT2("a")}) || !slices.Equal(got.Limitations, []string{geNotListed("unit", geWhyPlain)}) {
		t.Errorf("B1/B1.a violated: Evaluate gives %+v; want problems [%q] and limitations [%q]", got, geT2("a"), geNotListed("unit", geWhyPlain))
	}
	for _, l := range got.Limitations {
		if strings.HasPrefix(l, "go.package-test/v2 could not compute the package graph") {
			t.Errorf("B1.a violated: the 2-minute ctx deadline did not reach LoadGoFacts: %q", l)
		}
	}
	if len(gfLines(rec)) == 0 {
		t.Errorf("B1 violated: Evaluate did not run go through env's PATH")
	}
}

// B2: reverse dependencies come from the real package graph.
func TestEvaluateV2ExampleB2(t *testing.T) {
	gfRealGo(t)
	files := map[string]string{
		"go.mod":      "module example.com/m\n\ngo 1.21\n",
		"a/a.go":      "package a\n\nfunc F() int { return 1 }\n",
		"a/a_test.go": "package a\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) { _ = F() }\n",
		"b/b.go":      "package b\n\nimport \"example.com/m/a\"\n\nfunc G() int { return a.F() }\n",
		"b/b_test.go": "package b\n\nimport \"testing\"\n\nfunc TestG(t *testing.T) { _ = G() }\n",
	}
	dir := gfModule(t, files)
	passed := []langpack.Run{{CheckID: "unit", Argv: []string{"go", "test", "-race", "./a"}}}
	got := langpack.Evaluate(gfCtx(t), dir, gfEnv(), []string{"go.package-test/v2"}, langpack.Source{Current: gfKeys(files), Changed: []string{"a/a.go"}}, passed)
	if want := geDep("b", "a"); !slices.Contains(got.Problems, want) {
		t.Errorf("B2 violated: problems %q lack %q", got.Problems, want)
	}
}
