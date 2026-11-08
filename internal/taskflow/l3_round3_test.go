package taskflow_test

// Round 3 (assignment l3-tests-3): R29's plan-order picks with the picked run
// in the middle of record order (U1), R32's condition that verify judges at
// least one test (C2), and R41's "first later current covering assignment"
// (C3). Every expected value comes from interface contract L3, revision 3.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// l3r3Find returns the trace entry of one mapped test of a current
// assignment's requirement, failing the test when the report has none.
func l3r3Find(t *testing.T, rep taskflow.TraceReport, asg, req, worker, name string) taskflow.TraceTest {
	t.Helper()
	for _, c := range rep.Criteria {
		for _, r := range c.Requirements {
			if r.AssignmentID != asg || r.Requirement != req {
				continue
			}
			for _, x := range r.Tests {
				if x.Path == worker && x.Name == name {
					return x
				}
			}
		}
	}
	t.Fatalf("trace has no test %s %s under %s/%s: %s", worker, name, asg, req, l3JSON(rep))
	return taskflow.TraceTest{}
}

// l3r3Expect checks one mapped test's status and detail in the trace (R28,
// R29) and the matching verify problem (R30).
func (f *l3Fix) l3r3Expect(label string, rep taskflow.TraceReport, v taskflow.Verification, req, worker, name, status, phrase, detail string) {
	f.t.Helper()
	got := l3r3Find(f.t, rep, "te1", req, worker, name)
	if got.Status != status || got.Detail != detail {
		f.t.Errorf("%s: trace %s %s status %q detail %q, want %q %q (R29)", label, worker, name, got.Status, got.Detail, status, detail)
	}
	want := fmt.Sprintf("worker assignment te1 requirement %s test %s %s %s: %s", req, worker, name, phrase, detail)
	if !l3Has(v.Problems, want) {
		f.t.Errorf("%s: verify problems %v lack %q (R30)", label, v.Problems, want)
	}
}

// l3r3RunAll runs the checks in the order given and requires each to end with
// status want, returning the runs by check ID.
func (f *l3Fix) l3r3RunAll(want string, checks ...string) map[string]taskflow.CheckRun {
	f.t.Helper()
	out := map[string]taskflow.CheckRun{}
	for _, id := range checks {
		run := f.run(id)
		if run.Status != want {
			f.t.Fatalf("fixture run of check %s: status %q, want %q", id, run.Status, want)
		}
		out[id] = run
	}
	return out
}

// TestTracePicksMiddleRunInPlanCheckOrder fails when the failed detail, the
// skipped detail or not_run detail 1 does not name the first candidate run in
// plan check order (R29). Each group has three candidate runs: the plan lists
// x2, x1, x3 and they run x1, x2, x3, so x2, the run R29 picks, is neither the
// first nor the last in record order, nor the first check ID in sorted order.
// A walk oldest-first names x1, newest-first x3.
func TestTracePicksMiddleRunInPlanCheckOrder(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{checks: []taskflow.Check{
		// Credited, passing, TestSkip skips (skipped detail).
		l3Check("s2", "go", "test", "-json", "-count=1", "./p/..."),
		l3Check("s1", "go", "test", "-json", "./p/..."),
		l3Check("s3", "go", "test", "-json", l3Module+"/p"),
		// Not credited (-run), passing, TestPass passes (not_run detail 1).
		l3Check("u2", "go", "test", "-json", "-run", "Test", "./p/..."),
		l3Check("u1", "go", "test", "-json", "-run", ".", "./p/..."),
		l3Check("u3", "go", "test", "-json", "-run", "Pass", "./p/..."),
		// Credited, failing, TestFail fails (failed detail).
		l3Check("f2", "go", "test", "-json", "-count=1", "./q/..."),
		l3Check("f1", "go", "test", "-json", "./q/..."),
		l3Check("f3", "go", "test", "-json", l3Module+"/q"),
	}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go", "q/te_test.go"}, l3Req("R1", "C1"), l3Req("R2", "C1"), l3Req("R3", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.write("q/te_test.go", l3QTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go", "q/te_test.go"},
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass")),
		l3Map("R2", l3Mapped("p/te_test.go", "TestSkip")),
		l3Map("R3", l3Mapped("q/te_test.go", "TestFail"))))

	// Phase 1: only uncredited passing runs of p, so TestPass is not_run with
	// detail 1; and three failing runs of q.
	u := f.l3r3RunAll("passed", "u1", "u2", "u3")
	fl := f.l3r3RunAll("failed", "f1", "f2", "f3")
	rep, v := f.trace(), f.verify()
	f.l3r3Expect("uncredited PASS (not_run detail 1)", rep, v, "R1", "p/te_test.go", "TestPass", "not_run", "has no PASS at the current source",
		fmt.Sprintf("run %s of check u2 recorded a PASS, but its argv earns no per-test credit", u["u2"].ID))
	f.l3r3Expect("FAIL (failed detail)", rep, v, "R3", "q/te_test.go", "TestFail", "failed", "failed at the current source",
		fmt.Sprintf("run %s of check f2 recorded a FAIL", fl["f2"].ID))

	// Phase 2: three credited passing runs of p in which TestSkip skips.
	s := f.l3r3RunAll("passed", "s1", "s2", "s3")
	rep, v = f.trace(), f.verify()
	f.l3r3Expect("SKIP (skipped detail)", rep, v, "R2", "p/te_test.go", "TestSkip", "skipped", "was skipped at the current source",
		fmt.Sprintf("run %s of check s2 recorded only SKIP", s["s2"].ID))
	f.l3r3Expect("FAIL after more runs (failed detail)", rep, v, "R3", "q/te_test.go", "TestFail", "failed", "failed at the current source",
		fmt.Sprintf("run %s of check f2 recorded a FAIL", fl["f2"].ID))
	if got := l3r3Find(t, rep, "te1", "R1", "p/te_test.go", "TestPass"); got.Status != "passed" {
		t.Errorf("TestPass status %q after credited passing runs, want passed (R28)", got.Status)
	}
}

// l3r3Many is a package whose credited run's results hit the R22 byte bound
// before its last test (the same arithmetic as TestTraceTruncatedResults:
// each subtest costs 2*(len("example.com/fix/b")+409)+128 = 980 bytes of the
// 7 MiB bound, 7489 fit, and TestZzz... costs 1012 of the 812 that remain).
var l3r3Many = `package b

import (
	"fmt"
	"strings"
	"testing"
)

func TestMany(t *testing.T) {
	pad := strings.Repeat("x", 395)
	for i := 0; i < 8000; i++ {
		t.Run(fmt.Sprintf("%05d%s", i, pad), func(t *testing.T) {})
	}
}

func TestZ` + strings.Repeat("z", 420) + `(t *testing.T) {}
`

// TestTraceTruncatedPicksMiddleRunInPlanCheckOrder fails when not_run detail 3
// does not name the first per-test credited current passed run with truncated
// results in plan check order (R29). The plan lists t2, t1, t3 and they run
// t1, t2, t3, so t2 is neither the first nor the last in record order.
func TestTraceTruncatedPicksMiddleRunInPlanCheckOrder(t *testing.T) {
	t.Parallel()
	last := "TestZ" + strings.Repeat("z", 420)
	f := l3New(t, l3Opts{checks: []taskflow.Check{
		l3Check("t2", "go", "test", "-json", "-count=1", "./b/..."),
		l3Check("t1", "go", "test", "-json", "./b/..."),
		l3Check("t3", "go", "test", "-json", l3Module+"/b"),
	}})
	f.mustAssign(f.brief("te1", []string{"b/b_test.go"}, l3Req("R1", "C1")))
	f.write("b/b_test.go", l3r3Many)
	f.mustSubmit(f.result("te1", "completed", []string{"b/b_test.go"}, l3Map("R1", l3Mapped("b/b_test.go", last))))
	runs := f.l3r3RunAll("passed", "t1", "t2", "t3")
	f.l3r3Expect("truncated results (not_run detail 3)", f.trace(), f.verify(), "R1", "b/b_test.go", last, "not_run", "has no PASS at the current source",
		fmt.Sprintf("run %s of check t2 stopped recording tests at its bound", runs["t2"].ID))
}

var l3r3R32Form = regexp.MustCompile(`^check run \S+ test results: `)

// TestVerifyResultsIntegrityOnlyWhenATestIsJudged fails when verify reports an
// unreadable results file although it judges no test, or does not report it
// once a test is judged (R32: "when verify judges at least one test"; R27: an
// assignment without a completed result is not judged).
func TestVerifyResultsIntegrityOnlyWhenATestIsJudged(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	// The plan's -json check makes the plan prove tests, so the map-bearing
	// brief is accepted (R8); te1 records no completed result yet.
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	run := f.run("ok")
	if run.Status != "passed" || run.TestResults == "" {
		t.Fatalf("fixture run: status %q, test_results %q; want passed with results (R15)", run.Status, run.TestResults)
	}
	if err := os.Remove(filepath.Join(f.dir, run.TestResults)); err != nil {
		t.Fatal(err)
	}
	r32 := "check run " + run.ID + " test results: stored file cannot be read or differs from its sha256"

	v := f.verify()
	for _, list := range [][]string{v.Problems, v.Limitations} {
		for _, x := range list {
			if l3r3R32Form.MatchString(x) {
				t.Errorf("verify judges no test but added %q (R32)", x)
			}
		}
	}
	if got := l3WithPrefix(v.Problems, "worker assignment te1 requirement "); len(got) != 0 {
		t.Errorf("verify judged te1, which has no completed result: %v (R27)", got)
	}

	// Positive control: once te1's mapped test is judged, the same missing
	// file is reported, exactly once.
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	v = f.verify()
	var got []string
	for _, x := range v.Problems {
		if l3r3R32Form.MatchString(x) {
			got = append(got, x)
		}
	}
	l3SameJSON(t, "R32 problems once a test is judged", got, []string{r32})
}

// TestTraceReplacedNamesFirstLaterCoveringAssignment fails when a replaced
// assignment's closed state names any assignment other than the first later
// current map-bearing assignment that covers it, in record order (Terms, R34,
// R41). te1 is covered by zz2 and then aa3; zz2 and aa3 do not cover each
// other, so both are current, and neither declares R1. The IDs sort in the
// reverse of record order, so naming the smallest ID or the last covering
// assignment gives aa3.
func TestTraceReplacedNamesFirstLaterCoveringAssignment(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2", "C3"}})
	test := func(name string) string {
		return "package p\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {}\n"
	}
	f.mustAssign(f.brief("te1", []string{"p/a_test.go"}, l3Req("R1", "C1")))
	f.write("p/a_test.go", test("TestA"))
	f.mustSubmit(f.result("te1", "completed", []string{"p/a_test.go"}, l3Map("R1", l3Mapped("p/a_test.go", "TestA"))))
	f.mustAssign(f.brief("zz2", []string{"p/a_test.go", "p/b_test.go"}, l3Req("R2", "C2")))
	f.write("p/b_test.go", test("TestB"))
	f.mustSubmit(f.result("zz2", "completed", []string{"p/b_test.go"}, l3Map("R2", l3Mapped("p/b_test.go", "TestB"))))
	f.mustAssign(f.brief("aa3", []string{"p/a_test.go", "p/c_test.go"}, l3Req("R3", "C3")))

	state := "replaced by worker assignment zz2"
	rep := f.trace()
	l3SameJSON(t, "C1 of te1 replaced (R34, R35)", l3Criterion(t, rep, "C1"),
		l3TC("C1", "lost", l3Dropped("te1", "R1", state)))
	// Fixture checks: zz2 and aa3 are both current (neither is dropped).
	for _, c := range []struct{ crit, asg string }{{"C2", "zz2"}, {"C3", "aa3"}} {
		reqs := l3Criterion(t, rep, c.crit).Requirements
		if len(reqs) != 1 || reqs[0].AssignmentID != c.asg || reqs[0].Status == "dropped" {
			t.Errorf("criterion %s requirements %s, want one current entry of %s (Terms: current)", c.crit, l3JSON(reqs), c.asg)
		}
	}
	l3SameJSON(t, "R41 limitations", l3WithPrefix(f.verify().Limitations, "requirement "),
		[]string{"requirement R1 is no longer mapped: worker assignment te1 declared it and is " + state})
}
