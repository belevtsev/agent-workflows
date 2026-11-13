package langpack_test

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// Independent tests for GoEvidence, the go.package-test/v2 decision (P3a-2
// interface contract, B4-B15 and J1-J3). Every expected text is written from
// the contract; nothing here is computed by the code under test.

func geT1(p string) string {
	return "changed package " + p + " has no passed go test run at the current source"
}
func geT2(p string) string {
	return "changed package " + p + " has no passed go test -race run at the current source"
}
func geDep(q, c string) string {
	return "package " + q + " depends on changed package " + c + " and has no passed go test run at the current source"
}

// geFull is the B8 problem; reason "" is the module-file text without a suffix.
func geFull(file, reason string) string {
	s := "changed " + file + " has no passed go test ./... run at the current source"
	if reason != "" {
		s += " (" + reason + ")"
	}
	return s
}

const geGraphProblem = "changed Go packages need a passed go test ./... run at the current source because the package graph is unavailable"

func geGraphLim(e string) string {
	return "go.package-test/v2 could not compute the package graph: " + e
}
func geNoRaceLim(r string) string {
	return "-race cannot run on this host (" + r + "), so plain go test runs are credited for changed packages"
}
func geRerunLim(id string) string {
	return "check " + id + " passed after a failed run at the same source and GOFLAGS, so it earns no Go evidence credit"
}
func geSkipLim(id, name string) string {
	return "check " + id + ": test " + name + " was skipped and did not run"
}
func geNotListed(id, why string) string {
	return "check " + id + ": skipped tests are not listed because " + why
}
func geExcludedLim(f string) string {
	return "changed " + f + " is excluded by build constraints in the environment cw task verify uses, so no go test run there compiled it"
}

const (
	geWhyPlain = "it ran without -v or -json"
	geWhyNil   = "its log could not be read"
	geWhyCut   = "its log was cut at the storage limit"
)

// geModule is a module with packages a, b, c, p and q and no root package.
var geModule = []string{"go.mod", "a/a.go", "a/a_test.go", "b/b.go", "b/b_test.go", "c/c.go", "p/p.go", "q/q.go"}

func geSrc(changed ...string) langpack.Source {
	return langpack.Source{Current: geModule, Changed: changed}
}

// geRun is a passed run of check id with argv "go test <args>".
func geRun(id string, args ...string) langpack.Run {
	return langpack.Run{CheckID: id, Argv: append([]string{"go", "test"}, args...)}
}
func geRerun(r langpack.Run) langpack.Run { r.Rerun = true; return r }
func geLog(r langpack.Run, log string, truncated bool) langpack.Run {
	r.Log, r.Truncated = []byte(log), truncated
	return r
}
func geTested(kv ...[]string) map[string][]string {
	m := map[string][]string{}
	for _, e := range kv {
		m[e[0]] = e[1:]
	}
	return m
}

type geCase struct {
	name        string
	src         langpack.Source
	passed      []langpack.Run
	facts       langpack.GoFacts
	probs, lims []string
}

func geCheck(t *testing.T, id string, cases []geCase) {
	t.Helper()
	for _, c := range cases {
		got := langpack.GoEvidence(c.src, c.passed, c.facts)
		if !slices.Equal(got.Problems, c.probs) || !slices.Equal(got.Limitations, c.lims) {
			t.Errorf("%s violated (%s):\n problems    %q\n want        %q\n limitations %q\n want        %q",
				id, c.name, got.Problems, c.probs, got.Limitations, c.lims)
		}
	}
}

var geA = langpack.GoFacts{Tested: geTested([]string{"a", "a"})}

// B4: effective race/v/json values. Raced is observed through B5 (a clean run
// covering a: raced gives no problem, not raced gives T2); verbose through
// B12 with a nil Log. Fails when an inline false is read as true or GOFLAGS
// overrides argv.
func TestGoEvidenceFlagValuesB4(t *testing.T) {
	src := geSrc("a/a.go")
	for _, c := range []struct {
		goflags string
		args    []string
		raced   bool
	}{
		{"", []string{"-race", "./a"}, true}, {"", []string{"--race", "./a"}, true},
		{"", []string{"-race=true", "./a"}, true}, {"", []string{"-race=1", "./a"}, true},
		{"", []string{"-race=T", "./a"}, true}, {"", []string{"./a", "-race"}, true},
		{"-race", []string{"./a"}, true}, {"-race=false", []string{"-race", "./a"}, true},
		{"", []string{"./a"}, false}, {"", []string{"-race=false", "./a"}, false},
		{"", []string{"-race=0", "./a"}, false}, {"", []string{"-race=F", "./a"}, false},
		{"", []string{"-race", "-race=false", "./a"}, false}, {"-race", []string{"-race=false", "./a"}, false},
	} {
		r := geRun("unit", c.args...)
		r.GOFLAGS = c.goflags
		var want []string
		if !c.raced {
			want = []string{geT2("a")}
		}
		if got := langpack.GoEvidence(src, []langpack.Run{r}, geA).Problems; !slices.Equal(got, want) {
			t.Errorf("B4 violated: GOFLAGS=%q argv %q should be raced=%v; problems %q, want %q", c.goflags, r.Argv, c.raced, got, want)
		}
	}
	for _, c := range []struct {
		goflags string
		args    []string
		verbose bool
	}{
		{"", []string{"-v"}, true}, {"", []string{"-test.v"}, true}, {"", []string{"-json"}, true},
		{"", []string{"-v=true"}, true}, {"-v", nil, true}, {"-json", nil, true},
		{"", []string{"-v=false"}, false}, {"", []string{"-v=0"}, false},
		{"", []string{"-v=test2json"}, false}, {"", []string{"-v", "-v=false"}, false},
	} {
		r := geRun("unit", append(append([]string{"-race"}, c.args...), "./a")...)
		r.GOFLAGS = c.goflags
		want := []string{geNotListed("unit", geWhyPlain)}
		if c.verbose {
			want = []string{geNotListed("unit", geWhyNil)}
		}
		got := langpack.GoEvidence(src, []langpack.Run{r}, geA)
		if len(got.Problems) != 0 || !slices.Equal(got.Limitations, want) {
			t.Errorf("B4 violated: GOFLAGS=%q argv %q should be verbose=%v; got %+v, want limitations %q", c.goflags, r.Argv, c.verbose, got, want)
		}
	}
	// C1: a value-taking flag's separate value is its value, never a flag.
	tagsGOFLAGS := func(goflags string) langpack.Run { r := geRun("unit", "./a"); r.GOFLAGS = goflags; return r }
	geCheck(t, "B4/C1", []geCase{
		{"argv -tags -race: credited, not raced", src, []langpack.Run{geRun("unit", "-tags", "-race", "./a")}, geA, []string{geT2("a")}, []string{geNotListed("unit", geWhyPlain)}},
		{"GOFLAGS=-tags=-race: credited, not raced", src, []langpack.Run{tagsGOFLAGS("-tags=-race")}, geA, []string{geT2("a")}, []string{geNotListed("unit", geWhyPlain)}},
		{"GOFLAGS=-tags -race: not credited", src, []langpack.Run{tagsGOFLAGS("-tags -race")}, geA, []string{geT1("a")}, nil},
	})
}

// F1: the Go rule credits a run only under a go configuration that shows its
// tests ran. Oracle: a run without a recorded go env, or one whose GOOS or
// GOARCH is not its go command's host's (go test hands such test binaries to
// a go_<goos>_<goarch>_exec program from PATH), has a reason withheld; the
// rule then gives it no credit (T1 for its package, no per-test credit, a run
// limit of 0) and lists the reason once, only for a run its argv and GOFLAGS
// would credit; a host build withholds nothing; argv-only judgements are
// unchanged.
func TestGoEvidenceWithholdsCreditOutsideTheHostsGoConfiguration(t *testing.T) {
	host := langpack.GoEnv{GOFLAGS: "-v", GOOS: "linux", GOARCH: "amd64", GOHOSTOS: "linux", GOHOSTARCH: "amd64", C: &langpack.CToolchain{}}
	if got := langpack.GoEnvWithheld(&host); got != "" {
		t.Errorf("GoEnvWithheld(host build) = %q, want nothing withheld", got)
	}
	crossArch, crossOS, noC := host, host, host
	crossArch.GOARCH = "arm64"
	crossOS.GOOS = "windows"
	noC.C = nil
	for _, c := range []struct {
		name     string
		env      *langpack.GoEnv
		withheld string
	}{
		{"no go env", nil, "cw did not record the configuration its go command applied (go env); run it again"},
		{"another architecture", &crossArch, "it built its tests for linux/arm64, not for its go command's host linux/amd64, so go test may have handed them to a go_linux_arm64_exec program instead of running them"},
		{"another system", &crossOS, "it built its tests for windows/amd64, not for its go command's host linux/amd64, so go test may have handed them to a go_windows_amd64_exec program instead of running them"},
		{"recorded before cw judged the C toolchain (F18)", &noC, "cw did not record whether it verified the C toolchain its go command runs with cgo on; run it again"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := langpack.GoEnvWithheld(c.env); got != c.withheld {
				t.Fatalf("GoEnvWithheld = %q, want %q", got, c.withheld)
			}
			run := geRun("unit", "-race", "-json", "./a")
			run.Withheld = c.withheld
			if langpack.GoTestsCredited(run) || langpack.GoTestRunLimit(run) != 0 {
				t.Errorf("a withheld run is per-test credited (limit %d)", langpack.GoTestRunLimit(run))
			}
			if !langpack.CreditsGoTest(run.Argv) {
				t.Error("precondition: the argv alone earns no credit")
			}
			vet := langpack.Run{CheckID: "vet", Argv: []string{"go", "vet", "./..."}, Withheld: c.withheld}
			geCheck(t, "F1", []geCase{
				{c.name, geSrc("a/a.go"), []langpack.Run{run}, geA, []string{geT1("a")}, []string{"check unit passed, but earns no Go evidence credit: " + c.withheld}},
				{c.name + ", not go test", geSrc("a/a.go"), []langpack.Run{vet}, geA, []string{geT1("a")}, nil},
			})
		})
	}
}

// B5: each changed package needs a clean covering run (T1) and, while -race
// can run, a clean raced covering run (T2), with or without the graph.
func TestGoEvidenceChangedPackagesB5(t *testing.T) {
	plain, raced := geRun("unit", "./a"), geRun("unit", "-race", "./a")
	ab := langpack.GoFacts{Tested: geTested([]string{"a", "a"}, []string{"b", "b"})}
	noGraph := langpack.GoFacts{GraphError: "x"}
	geCheck(t, "B5", []geCase{
		{"no run", geSrc("a/a.go"), nil, geA, []string{geT1("a")}, nil},
		{"plain clean run while -race can run", geSrc("a/a.go"), []langpack.Run{plain}, geA, []string{geT2("a")}, []string{geNotListed("unit", geWhyPlain)}},
		{"clean raced run", geSrc("a/a.go"), []langpack.Run{raced}, geA, nil, []string{geNotListed("unit", geWhyPlain)}},
		{"raced run that is a rerun", geSrc("a/a.go"), []langpack.Run{geRerun(raced)}, geA, []string{geT1("a")}, []string{geRerunLim("unit")}},
		{"plain run plus a raced rerun", geSrc("a/a.go"), []langpack.Run{geRun("u1", "./a"), geRerun(geRun("u2", "-race", "./a"))}, geA,
			[]string{geT2("a")}, []string{geNotListed("u1", geWhyPlain), geRerunLim("u2")}},
		{"raced run of another package", geSrc("a/a.go"), []langpack.Run{geRun("unit", "-race", "./b")}, geA, []string{geT1("a")}, []string{geNotListed("unit", geWhyPlain)}},
		{"raced ./... covers", geSrc("a/a.go"), []langpack.Run{geRun("unit", "-race", "./...")}, geA, nil, []string{geNotListed("unit", geWhyPlain)}},
		{"two packages, one covered", geSrc("b/b.go", "a/a.go"), []langpack.Run{raced}, ab, []string{geT1("b")}, []string{geNotListed("unit", geWhyPlain)}},
		{"graph unavailable, plain run", geSrc("a/a.go"), []langpack.Run{plain}, noGraph,
			[]string{geGraphProblem, geT2("a")}, []string{geGraphLim("x"), geNotListed("unit", geWhyPlain)}},
	})
}

// B6: with NoRace set and a changed package, T2 is never given and one
// limitation names the reason; no changed package means no limitation.
func TestGoEvidenceRaceUnavailableB6(t *testing.T) {
	f := langpack.GoFacts{Tested: geTested([]string{"a", "a"}), NoRace: "-race is not supported on plan9/amd64"}
	lim := geNoRaceLim("-race is not supported on plan9/amd64")
	geCheck(t, "B6", []geCase{
		{"plain run credited", geSrc("a/a.go"), []langpack.Run{geRun("unit", "./a")}, f, nil, []string{lim, geNotListed("unit", geWhyPlain)}},
		{"no run still needs T1", geSrc("a/a.go"), nil, f, []string{geT1("a")}, []string{lim}},
		{"no changed package, no limitation", geSrc("go.mod"), []langpack.Run{geRun("unit", "./...")}, f, nil, []string{geNotListed("unit", geWhyPlain)}},
		{"one limitation for several changed packages", geSrc("a/a.go", "b/b.go", "c/c.go"), []langpack.Run{geRun("unit", "./...")}, f, nil,
			[]string{lim, geNotListed("unit", geWhyPlain)}},
	})
}

// B7: a tested package that links a propagating changed package needs a
// clean covering run; test-only and testdata changes do not propagate.
func TestGoEvidenceReverseDependenciesB7(t *testing.T) {
	f := langpack.GoFacts{Tested: geTested([]string{"a", "a"}, []string{"b", "a", "b"}, []string{"c", "c"}, []string{"d", "b", "d"})}
	ra := geRun("u1", "-race", "./a")
	l1 := geNotListed("u1", geWhyPlain)
	za := langpack.Source{Current: []string{"go.mod", "Z/z.go", "a/a.go", "q/q.go"}, Changed: []string{"a/a.go", "Z/z.go"}}
	zf := langpack.GoFacts{Tested: geTested([]string{"Z", "Z"}, []string{"a", "a"}, []string{"q", "Z", "a", "q"})}
	geCheck(t, "B7", []geCase{
		{"dependent without a run", geSrc("a/a.go"), []langpack.Run{ra}, f, []string{geDep("b", "a")}, []string{l1}},
		{"plain clean run of the dependent is enough", geSrc("a/a.go"), []langpack.Run{ra, geRun("u2", "./b")}, f, nil,
			[]string{l1, geNotListed("u2", geWhyPlain)}},
		{"rerun of the dependent earns nothing", geSrc("a/a.go"), []langpack.Run{ra, geRerun(geRun("u2", "-race", "./b"))}, f,
			[]string{geDep("b", "a")}, []string{l1, geRerunLim("u2")}},
		{"B7.a test-only change", geSrc("a/a_test.go"), []langpack.Run{ra}, f, nil, []string{l1}},
		{"B7.a testdata change", geSrc("a/testdata/in.txt"), []langpack.Run{ra}, f, nil, []string{l1}},
		{"non-Go file propagates", geSrc("a/hello.txt"), []langpack.Run{ra}, f, []string{geDep("b", "a")}, []string{l1}},
		// b gets B5 (T1), not "package b depends on changed package a"; d,
		// unchanged and linking the changed b, still gets B7.
		{"changed dependent gets B5, not B7", geSrc("a/a.go", "b/b.go"), []langpack.Run{ra}, f, []string{geT1("b"), geDep("d", "b")}, []string{l1}},
		{"smallest changed package in byte order", za, []langpack.Run{geRun("u1", "-race", "./Z", "./a")}, zf, []string{geDep("q", "Z")}, []string{l1}},
		{"graph unavailable skips B7", geSrc("a/a.go"), []langpack.Run{ra}, langpack.GoFacts{Tested: f.Tested, GraphError: "g"},
			[]string{geGraphProblem}, []string{geGraphLim("g"), l1}},
	})
}

// B8: full-run triggers, first reason wins, root-module paths only; only a
// clean ./... run (raced or not) satisfies them. Observed as the B8 problems
// for each named file, so unrelated package problems do not matter here.
func TestGoEvidenceFullRunTriggersB8(t *testing.T) {
	base := []string{"go.mod", "a/a.go", "tools/go.mod", "tools/t.go"}
	vendored := append(slices.Clone(base), "vendor/modules.txt")
	for _, c := range []struct {
		name    string
		current []string
		facts   langpack.GoFacts
		passed  []langpack.Run
		want    map[string]string // changed file -> reason; "-" means no B8 problem
	}{
		{"module files", base, langpack.GoFacts{}, nil, map[string]string{"go.mod": "", "go.sum": "", "go.work": "", "go.work.sum": "", "sub/go.sum": "-"}},
		{"module file wins over embedded", base, langpack.GoFacts{Embedded: []string{"go.mod"}}, nil, map[string]string{"go.mod": ""}},
		{"vendored dependency", vendored, langpack.GoFacts{Constrained: []string{"vendor/example.com/d/d.go"}}, nil,
			map[string]string{"vendor/example.com/d/d.go": "vendored dependency", "vendor/modules.txt": "vendored dependency", "a/vendor/x.go": "-"}},
		{"vendor without modules.txt", base, langpack.GoFacts{Constrained: []string{"a/vendor/x.go", "vendor/example.com/d/d.go"}}, nil,
			map[string]string{"vendor/example.com/d/d.go": "-", "a/vendor/x.go": "-"}},
		{"build constraint first", base, langpack.GoFacts{Constrained: []string{"a/a.go"}, Generated: []string{"a/a.go"}, Embedded: []string{"a/a.go"}}, nil,
			map[string]string{"a/a.go": "build constraint"}},
		{"generated code before embedded", base, langpack.GoFacts{Generated: []string{"a/gen.go"}, Embedded: []string{"a/gen.go"}}, nil,
			map[string]string{"a/gen.go": "generated code"}},
		{"embedded file", base, langpack.GoFacts{Embedded: []string{"a/hello.txt", "a/testdata/e.txt"}}, nil,
			map[string]string{"a/hello.txt": "embedded file", "a/testdata/e.txt": "embedded file", "a/other.txt": "-"}},
		{"shared testdata", base, langpack.GoFacts{}, nil,
			map[string]string{"testdata/x.json": "shared testdata", "shared/testdata/x.json": "shared testdata", "a/testdata/x.json": "-"}},
		{"nested module, _ and . paths never trigger", base,
			langpack.GoFacts{Constrained: []string{".cache/c.go", "tools/t.go"}, Generated: []string{"_gen/g.go", "tools/sub/x.go"}, Embedded: []string{"a/_x/y.txt", "a/.d/z.txt"}}, nil,
			map[string]string{"tools/t.go": "-", "tools/sub/x.go": "-", "_gen/g.go": "-", ".cache/c.go": "-", "a/_x/y.txt": "-", "a/.d/z.txt": "-", "tools/testdata/x.json": "-"}},
		{"other changed files never trigger", base, langpack.GoFacts{}, nil, map[string]string{"a/a.go": "-", "a/doc.txt": "-"}},
		{"plain full run satisfies", base, langpack.GoFacts{Constrained: []string{"a/a.go"}}, []langpack.Run{geRun("unit", "./...")},
			map[string]string{"a/a.go": "-", "go.mod": "-"}},
		{"raced full run satisfies", base, langpack.GoFacts{Constrained: []string{"a/a.go"}}, []langpack.Run{geRun("unit", "-race", "./...")},
			map[string]string{"a/a.go": "-", "go.mod": "-"}},
		{"rerun ./... does not satisfy", base, langpack.GoFacts{Constrained: []string{"a/a.go"}}, []langpack.Run{geRerun(geRun("unit", "-race", "./..."))},
			map[string]string{"a/a.go": "build constraint", "go.mod": ""}},
		{"filtered ./... does not satisfy", base, langpack.GoFacts{Constrained: []string{"a/a.go"}},
			[]langpack.Run{geRun("u1", "-run=X", "./..."), {CheckID: "u2", Argv: []string{"go", "test", "./..."}, GOFLAGS: "-run=^$"}},
			map[string]string{"a/a.go": "build constraint", "go.mod": ""}},
		{"./a/... is not a full run", base, langpack.GoFacts{Constrained: []string{"a/a.go"}}, []langpack.Run{geRun("unit", "-race", "./a/...")},
			map[string]string{"a/a.go": "build constraint"}},
	} {
		changed := make([]string, 0, len(c.want))
		for f := range c.want {
			changed = append(changed, f)
		}
		slices.Sort(changed)
		got := langpack.GoEvidence(langpack.Source{Current: c.current, Changed: changed}, c.passed, c.facts).Problems
		for _, f := range changed {
			var found []string
			for _, p := range got {
				if p == geFull(f, "") || strings.HasPrefix(p, geFull(f, "")+" (") {
					found = append(found, p)
				}
			}
			var want []string
			if c.want[f] != "-" {
				want = []string{geFull(f, c.want[f])}
			}
			if !slices.Equal(found, want) {
				t.Errorf("B8 violated (%s): B8 problems for %s are %q, want %q\nall problems %q", c.name, f, found, want, got)
			}
		}
	}
	geCheck(t, "B8", []geCase{
		// B5 still applies after a plain full run.
		{"full plain run leaves T2", langpack.Source{Current: base, Changed: []string{"a/a.go"}},
			[]langpack.Run{geRun("unit", "./...")}, langpack.GoFacts{Constrained: []string{"a/a.go"}, Tested: geA.Tested},
			[]string{geT2("a")}, []string{geNotListed("unit", geWhyPlain)}},
		// Only changed files trigger: unchanged Embedded, Constrained and Generated entries never do.
		{"unchanged fact entries never trigger", geSrc("a/a.go"), []langpack.Run{geRun("unit", "-race", "./a")},
			langpack.GoFacts{Tested: geA.Tested, Embedded: []string{"b/emb.txt"}, Constrained: []string{"c/c.go"}, Generated: []string{"p/p.go"}},
			nil, []string{geNotListed("unit", geWhyPlain)}},
	})
}

// B9: a missing graph adds its limitation and, unless a full run exists, the
// full-run problem; B7 and B13 are skipped; no changed package, no B9.
func TestGoEvidenceGraphUnavailableB9(t *testing.T) {
	g := langpack.GoFacts{GraphError: "go list: exit status 1", Tested: geTested([]string{"b", "a", "b"}), Excluded: []string{"a/a.go"}}
	lim := geGraphLim("go list: exit status 1")
	geCheck(t, "B9", []geCase{
		{"no run", geSrc("a/a.go"), nil, g, []string{geGraphProblem, geT1("a")}, []string{lim}},
		{"raced package run", geSrc("a/a.go"), []langpack.Run{geRun("u", "-race", "./a")}, g, []string{geGraphProblem}, []string{lim, geNotListed("u", geWhyPlain)}},
		{"raced full run", geSrc("a/a.go"), []langpack.Run{geRun("u", "-race", "./...")}, g, nil, []string{lim, geNotListed("u", geWhyPlain)}},
		{"plain full run", geSrc("a/a.go"), []langpack.Run{geRun("u", "./...")}, g, []string{geT2("a")}, []string{lim, geNotListed("u", geWhyPlain)}},
		{"rerun full run", geSrc("a/a.go"), []langpack.Run{geRerun(geRun("u", "-race", "./...")), geRun("v", "-race", "./a")}, g,
			[]string{geGraphProblem}, []string{lim, geRerunLim("u"), geNotListed("v", geWhyPlain)}},
		{"only a module file changed", geSrc("go.mod"), nil, g, []string{geFull("go.mod", "")}, nil},
	})
}

// B10: a rerun earns nothing and says so, once per credited rerun; a rerun's
// log is not read.
func TestGoEvidenceRerunsB10(t *testing.T) {
	skip := "--- SKIP: TestX (0.00s)\nok  \texample.com/m/a\t0.1s\n"
	geCheck(t, "B10", []geCase{
		{"raced rerun", geSrc("a/a.go"), []langpack.Run{geRerun(geRun("u", "-race", "./a"))}, geA, []string{geT1("a")}, []string{geRerunLim("u")}},
		{"verbose rerun with skips", geSrc("a/a.go"), []langpack.Run{geLog(geRerun(geRun("u", "-race", "-v", "./a")), skip, true)}, geA,
			[]string{geT1("a")}, []string{geRerunLim("u")}},
		{"uncredited rerun is silent", geSrc("a/a.go"), []langpack.Run{{CheckID: "vet", Argv: []string{"go", "vet", "./..."}, Rerun: true}, geRun("u", "-race", "./a")}, geA,
			nil, []string{geNotListed("u", geWhyPlain)}},
	})
}

// B11, B12: skipped tests of clean verbose runs are listed once per name and
// package, in first-seen order, and keep the run's credit; otherwise at most
// one limitation says why skips are not listed.
func TestGoEvidenceSkippedTestsB11B12(t *testing.T) {
	v := func(log string, truncated bool, args ...string) []langpack.Run {
		return []langpack.Run{geLog(geRun("unit", append([]string{"-race"}, append(args, "./...")...)...), log, truncated)}
	}
	sk := func(names ...string) []string {
		var out []string
		for _, n := range names {
			out = append(out, geSkipLim("unit", n))
		}
		return out
	}
	src := geSrc("a/a.go")
	twoPkgs := "=== RUN   TestIntegration\n    a_test.go:9: needs a database\n--- SKIP: TestIntegration (0.00s)\n=== RUN   TestOK\n--- PASS: TestOK (0.00s)\nPASS\n" +
		"ok  \texample.com/m/a\t0.1s\n=== RUN   TestIntegration\n--- SKIP: TestIntegration (0.00s)\nPASS\nok  \texample.com/m/b\t0.1s\n"
	json := `{"Time":"2026-01-02T03:04:05Z","Action":"run","Package":"example.com/m/b","Test":"TestJ"}` + "\n" +
		`{"Time":"2026-01-02T03:04:05Z","Action":"output","Package":"example.com/m/b","Test":"TestJ","Output":"--- SKIP: TestJ (0.00s)\n"}` + "\n" +
		`{"Time":"2026-01-02T03:04:05Z","Action":"skip","Package":"example.com/m/b","Test":"TestJ","Elapsed":0}` + "\n" +
		`{"Action":"skip","Package":"example.com/m/a","Test":"TestJ"}` + "\n" +
		`{"Action":"skip","Test":"TestNoPackage"}` + "\n" +
		`{"Action":"skip","Package":"example.com/m/c"}` + "\n" +
		`{"Action":"output","Package":"example.com/m/b","Output":"ok  \texample.com/m/b\t0.1s\n"}` + "\n"
	geCheck(t, "B11/B12", []geCase{
		{"B11.a same test in two packages", src, v(twoPkgs, false, "-v"), geA, nil, sk("example.com/m/a TestIntegration", "example.com/m/b TestIntegration")},
		{"B11.b subtest, cached result line", src, v("    --- SKIP: TestOther/inner (0.00s)\nok  \texample.com/m/a\t(cached)\n", false, "-v"), geA, nil,
			sk("example.com/m/a TestOther/inner")},
		{"B11.c no result line, log cut", src, v("--- SKIP: TestLast (0.00s)\n", true, "-v"), geA, nil, append(sk("TestLast"), geNotListed("unit", geWhyCut))},
		{"FAIL result line, first-seen order, listed once", src,
			v("--- SKIP: TestB (0.00s)\n--- SKIP: TestA (0.00s)\n--- SKIP: TestB (0.00s)\nFAIL\texample.com/m/c\t0.2s\n", false, "-v"), geA, nil,
			sk("example.com/m/c TestB", "example.com/m/c TestA")},
		{"-json events, output events not counted", src, v(json, false, "-json"), geA, nil,
			sk("example.com/m/b TestJ", "example.com/m/a TestJ", "TestNoPackage")},
		{"B12.1 not verbose: log not read", src, v(twoPkgs, true), geA, nil, []string{geNotListed("unit", geWhyPlain)}},
		{"B12.2 verbose, nil log", src, []langpack.Run{{CheckID: "unit", Argv: []string{"go", "test", "-race", "-v", "./..."}, Truncated: true}}, geA, nil,
			[]string{geNotListed("unit", geWhyNil)}},
		{"verbose, empty non-nil log", src, v("", false, "-v"), geA, nil, nil},
		{"B12.3 verbose, cut log without skips", src, v("PASS\nok  \texample.com/m/a\t0.1s\n", true, "-v"), geA, nil, []string{geNotListed("unit", geWhyCut)}},
	})
}

// B13: changed root-module paths in Excluded are listed by path, with no problem.
func TestGoEvidenceExcludedFilesB13(t *testing.T) {
	src := langpack.Source{Current: append(slices.Clone(geModule), "a/x_windows.go", "tools/go.mod", "tools/t.go"),
		Changed: []string{"tools/t.go", "b/b.go", "a/x_windows.go", "a/a.go"}}
	f := langpack.GoFacts{Tested: geTested([]string{"a", "a"}, []string{"b", "b"}), Excluded: []string{"a/x_windows.go", "b/b.go", "c/c.go", "tools/t.go"}}
	lims := []string{geExcludedLim("a/x_windows.go"), geExcludedLim("b/b.go")}
	geCheck(t, "B13", []geCase{
		{"listed by path", src, []langpack.Run{geRun("u", "-race", "./...")}, f, nil, append(slices.Clone(lims), geNotListed("u", geWhyPlain))},
		{"adds no problem", src, nil, f, []string{geT1("a"), geT1("b")}, lims},
	})
}

// geUncredited are runs B14 says never affect the result.
var geUncredited = []langpack.Run{
	{CheckID: "x1", Argv: []string{"go", "vet", "./..."}},
	{CheckID: "x2", Argv: []string{"go", "build", "./..."}, Rerun: true},
	geLog(geRun("x3", "-v", "-run=X", "./..."), "--- SKIP: TestS (0.00s)\nok  \tm/a\t1s\n", true),
	geRun("x4", "-race", "./a", "-args", "-v"),
	geRun("x5", "-race", "./a", "-count=1", "./b"),
	{CheckID: "x6", Argv: []string{"gotest", "-race", "./..."}},
	{CheckID: "x7", Argv: []string{"go", "test", "-v", "-race", "./..."}, GOFLAGS: "-run=^$", Rerun: true},
	geRun("x8", "-frobnicate", "-race", "./..."),
}

// B14: nothing to check gives Evidence{} (nil slices); uncredited runs never
// change any result.
func TestGoEvidenceNothingToCheckB14(t *testing.T) {
	clean := []langpack.Run{geLog(geRun("u", "-v", "./..."), "--- SKIP: TestS (0.00s)\n", true), geRerun(geRun("r", "./a"))}
	none := []string{".github/ci.go", "_tools/tool.go", "vendor/example.com/dep/dep.go", "nested/nested.go"}
	for _, c := range []struct {
		name string
		src  langpack.Source
	}{
		{"no go.mod", langpack.Source{Current: []string{"a/a.go"}, Changed: []string{"a/a.go"}}},
		{"Changed nil", langpack.Source{Current: geModule, Changed: nil}},
		{"Changed empty", langpack.Source{Current: geModule, Changed: []string{}}},
		{"no package changed, no B8 reason", langpack.Source{Current: append([]string{"go.mod", "nested/go.mod"}, none...), Changed: none}},
	} {
		f := langpack.GoFacts{Tested: geA.Tested, Generated: []string{"_tools/tool.go", "nested/nested.go"}, NoRace: "r", GraphError: "g"}
		for _, facts := range []langpack.GoFacts{f, {}} {
			if got := langpack.GoEvidence(c.src, clean, facts); !reflect.DeepEqual(got, langpack.Evidence{}) {
				t.Errorf("B14 violated (%s): got %#v, want Evidence{}", c.name, got)
			}
		}
	}
	scenarios := []geCase{
		{src: geSrc("a/a.go"), facts: geA}, {src: geSrc("go.mod", "a/a.go"), facts: langpack.GoFacts{GraphError: "g", NoRace: "r"}},
		{src: geSrc("a/a.go"), passed: []langpack.Run{geRun("u", "./a")}, facts: langpack.GoFacts{Tested: geTested([]string{"b", "a", "b"})}},
	}
	for i, s := range scenarios {
		want := langpack.GoEvidence(s.src, s.passed, s.facts)
		for _, x := range geUncredited {
			for _, passed := range [][]langpack.Run{append([]langpack.Run{x}, s.passed...), append(slices.Clone(s.passed), x)} {
				if got := langpack.GoEvidence(s.src, passed, s.facts); !slices.Equal(got.Problems, want.Problems) || !slices.Equal(got.Limitations, want.Limitations) {
					t.Errorf("B14 violated: uncredited run %q (GOFLAGS %q) changed scenario %d from %+v to %+v", x.Argv, x.GOFLAGS, i, want, got)
				}
			}
		}
	}
	if got := langpack.GoEvidence(geSrc("a/a.go"), nil, geA); len(got.Problems) == 0 {
		t.Errorf("B14 control violated: a changed package with no run gives no problem")
	}
}

// B15: problem and limitation order.
func TestGoEvidenceOrderB15(t *testing.T) {
	src := langpack.Source{Current: []string{"go.mod", "go.sum", "a/a.go", "a/gen.go", "b/b.go", "p/p.go", "q/q.go"},
		Changed: []string{"go.sum", "b/b.go", "go.mod", "a/gen.go", "a/a.go"}}
	f := langpack.GoFacts{Tested: geTested([]string{"a", "a"}, []string{"b", "b"}, []string{"q", "b", "q"}, []string{"p", "a", "p"}), Generated: []string{"a/gen.go"}}
	b8 := []string{geFull("a/gen.go", "generated code"), geFull("go.mod", ""), geFull("go.sum", "")}
	fg := f
	fg.GraphError = "g"
	src2 := langpack.Source{Current: []string{"go.mod", "a/a.go", "a/w.go", "a/x.go"}, Changed: []string{"a/x.go", "a/w.go"}}
	f2 := langpack.GoFacts{Tested: geA.Tested, Excluded: []string{"a/w.go", "a/x.go"}, NoRace: "r"}
	f2g := f2
	f2g.GraphError = "g"
	rr, vb, pl := geRerun(geRun("r1", "-race", "./a")), geLog(geRun("v1", "-v", "./..."), "--- SKIP: TestS (0.00s)\nok  \tm/a\t0.1s\n", true), geRun("n1", "./a")
	runLims := []string{geRerunLim("r1"), geSkipLim("v1", "m/a TestS"), geNotListed("v1", geWhyCut), geNotListed("n1", geWhyPlain)}
	reversed := []string{geNotListed("n1", geWhyPlain), geSkipLim("v1", "m/a TestS"), geNotListed("v1", geWhyCut), geRerunLim("r1")}
	head := []string{geNoRaceLim("r"), geExcludedLim("a/w.go"), geExcludedLim("a/x.go")}
	geCheck(t, "B15", []geCase{
		{"problems: B8 by path, B5 by package, B7 by Q", src, nil, f, append(slices.Clone(b8), geT1("a"), geT1("b"), geDep("p", "a"), geDep("q", "b")), nil},
		{"problems: B9 after B8, before B5", src, nil, fg, append(slices.Clone(b8), geGraphProblem, geT1("a"), geT1("b")), []string{geGraphLim("g")}},
		{"limitations: B6, B13 by path, then runs in passed order", src2, []langpack.Run{rr, vb, pl}, f2, nil, append(slices.Clone(head), runLims...)},
		{"limitations: runs reversed", src2, []langpack.Run{pl, vb, rr}, f2, nil, append(slices.Clone(head), reversed...)},
		{"limitations: B9 before B6", src2, []langpack.Run{rr, vb, pl}, f2g, nil, append([]string{geGraphLim("g"), geNoRaceLim("r")}, runLims...)},
	})
}

// geGenerate draws a source, passed runs and graph facts for the invariants.
func geGenerate(r *rand.Rand) (langpack.Source, []langpack.Run, langpack.GoFacts) {
	sub := func(pool ...string) []string {
		var out []string
		for _, s := range pool {
			if r.IntN(2) == 0 {
				out = append(out, s)
			}
		}
		return out
	}
	current := []string{"go.mod", "go.sum", "main.go", "a/a.go", "a/a_test.go", "b/b.go", "b/emb.txt", "c/c.go", "shared/testdata/s.json"}
	if r.IntN(3) == 0 {
		current = append(current, "vendor/modules.txt")
	}
	changed := sub("_tools/t.go", "a/a.go", "a/a_test.go", "a/testdata/t.json", "b/b.go", "b/emb.txt", "c/c.go", "go.mod", "go.sum",
		"main.go", "shared/testdata/s.json", "vendor/example.com/d/d.go")
	in := func(pool []string) []string {
		var out []string
		for _, s := range pool {
			if slices.Contains(changed, s) {
				out = append(out, s)
			}
		}
		return out
	}
	tested := map[string][]string{}
	for _, p := range []string{".", "a", "b", "c"} {
		if r.IntN(4) == 0 {
			continue
		}
		deps := []string{p}
		for _, d := range []string{".", "a", "b", "c"} {
			if d != p && r.IntN(3) == 0 {
				deps = append(deps, d)
			}
		}
		slices.Sort(deps)
		tested[p] = deps
	}
	f := langpack.GoFacts{Tested: tested, Embedded: sub("a/testdata/t.json", "b/emb.txt"), Excluded: sub("a/a.go", "c/c.go"),
		Constrained: in(sub("a/a.go", "c/c.go", "main.go")), Generated: in(sub("b/b.go", "main.go"))}
	if r.IntN(3) == 0 {
		f.NoRace = "no race here"
	}
	args := [][]string{{"-race", "./..."}, {"./..."}, {"-race", "./a"}, {"./b"}, {"-race", "./a", "./c"}, {"-race", "."},
		{"-v", "-race", "./..."}, {"-json", "./b"}, {"-run=X", "./..."}, {"-race", "./c"}, {"-race", "./b"}}
	passed := make([]langpack.Run, r.IntN(4))
	for i := range passed {
		passed[i] = geRun(fmt.Sprintf("u%d", i), args[r.IntN(len(args))]...)
		passed[i].Rerun, passed[i].Truncated = r.IntN(4) == 0, r.IntN(4) == 0
		if r.IntN(2) == 0 {
			passed[i].Log = []byte("--- SKIP: TestOld (0.00s)\nok  \texample.com/m/a\t0.1s\n")
		}
	}
	return langpack.Source{Current: current, Changed: changed}, passed, f
}

// J1: if GoEvidence finds no problem without the graph, it finds none with it.
// The counters guard against a vacuous generator.
func TestGoEvidenceMissingGraphNeverLoosensJ1(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	var emptyWithout, failWithout, failWith int
	for i := 0; i < 4000; i++ {
		src, passed, f := geGenerate(r)
		without := f
		without.GraphError, without.Tested, without.Embedded, without.Excluded = "graph broken", nil, nil, nil
		pWith, pWithout := langpack.GoEvidence(src, passed, f).Problems, langpack.GoEvidence(src, passed, without).Problems
		if len(pWithout) == 0 {
			emptyWithout++
			if len(pWith) != 0 {
				t.Fatalf("J1 violated: no problem without the graph, but %q with it\nsource %+v\npassed %+v\nfacts %+v", pWith, src, passed, f)
			}
		} else {
			failWithout++
		}
		if len(pWith) > 0 {
			failWith++
		}
	}
	if emptyWithout < 100 || failWithout < 100 || failWith < 100 {
		t.Fatalf("J1 not exercised: %d cases pass without the graph, %d fail without it, %d fail with it; want 100 each", emptyWithout, failWithout, failWith)
	}
}

// J2: a rerun counts as absent for problems; skip lines in a clean run's log
// never change problems.
func TestGoEvidenceRerunsAndSkipsJ2(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	extra := "--- SKIP: TestAdded (0.00s)\nok  \texample.com/m/c\t0.1s\n" + `{"Action":"skip","Package":"example.com/m/c","Test":"TestJSON"}` + "\n"
	var mattered, listed int
	for i := 0; i < 4000; i++ {
		src, passed, f := geGenerate(r)
		if len(passed) == 0 {
			continue
		}
		k := r.IntN(len(passed))
		orig := slices.Clone(passed)
		orig[k].Rerun = false
		set := slices.Clone(orig)
		set[k].Rerun = true
		removed := slices.Delete(slices.Clone(orig), k, k+1)
		pSet, pRemoved := langpack.GoEvidence(src, set, f).Problems, langpack.GoEvidence(src, removed, f).Problems
		if !slices.Equal(pSet, pRemoved) {
			t.Fatalf("J2 violated: Rerun on run %d gives %q, removing it gives %q\npassed %+v", k, pSet, pRemoved, set)
		}
		before := langpack.GoEvidence(src, orig, f)
		if !slices.Equal(before.Problems, pRemoved) {
			mattered++
		}
		skipped := slices.Clone(orig)
		skipped[k].Log = append(slices.Clone(orig[k].Log), extra...)
		after := langpack.GoEvidence(src, skipped, f)
		if !slices.Equal(after.Problems, before.Problems) {
			t.Fatalf("J2 violated: skip lines changed problems from %q to %q\nrun %+v", before.Problems, after.Problems, skipped[k])
		}
		if !slices.Equal(after.Limitations, before.Limitations) {
			listed++
		}
	}
	if mattered < 100 || listed < 100 {
		t.Fatalf("J2 not exercised: the chosen run mattered in %d cases and skips were listed in %d; want 100 each", mattered, listed)
	}
}

// J3: equal inputs give equal outputs, and order never follows map iteration.
func TestGoEvidenceDeterministicJ3(t *testing.T) {
	tested, current := geTested([]string{"a", "a"}), []string{"go.mod", "a/a.go"}
	var want []string
	for i := 0; i < 30; i++ {
		q := fmt.Sprintf("q%02d", i)
		tested[q] = []string{"a", q}
		current = append(current, q+"/q.go")
		want = append(want, geDep(q, "a"))
	}
	src, passed := langpack.Source{Current: current, Changed: []string{"a/a.go"}}, []langpack.Run{geRun("u", "-race", "./a")}
	for i := 0; i < 20; i++ {
		if got := langpack.GoEvidence(src, passed, langpack.GoFacts{Tested: tested}).Problems; !slices.Equal(got, want) {
			t.Fatalf("J3/B15 violated on call %d: problems %q, want %q", i, got, want)
		}
	}
	r := rand.New(rand.NewPCG(5, 6))
	for i := 0; i < 1000; i++ {
		src, passed, f := geGenerate(r)
		a := langpack.GoEvidence(src, passed, f)
		f2 := f
		f2.Tested = map[string][]string{}
		for k, v := range f.Tested {
			f2.Tested[k] = slices.Clone(v)
		}
		b := langpack.GoEvidence(langpack.Source{Current: slices.Clone(src.Current), Changed: slices.Clone(src.Changed)}, slices.Clone(passed), f2)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("J3 violated: equal inputs gave %+v and %+v", a, b)
		}
	}
}
