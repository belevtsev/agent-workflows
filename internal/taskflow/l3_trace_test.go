package taskflow_test

// Judging mapped tests and the trace report: R27-R35, R39, R40 (the new-cw
// half), R41, R42 and the verify entry order, plus R12/R14/R28 for a module
// below the project root.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	l3LostSuffix = " lost its mapped tests: every test-engineer assignment that linked it is closed unused, rejected or replaced"
	l3NotInPkg   = " is not in a package that go test ./... runs at the selected path"
)

func l3TT(path, name, pkg, status, detail string, runs ...taskflow.TraceRun) taskflow.TraceTest {
	if runs == nil {
		runs = []taskflow.TraceRun{}
	}
	return taskflow.TraceTest{Path: path, Name: name, Package: pkg, Status: status, Detail: detail, Runs: runs}
}

func l3TR(asg, req, status string, tests ...taskflow.TraceTest) taskflow.TraceRequirement {
	if tests == nil {
		tests = []taskflow.TraceTest{}
	}
	return taskflow.TraceRequirement{AssignmentID: asg, Requirement: req, Status: status, Tests: tests}
}

func l3Dropped(asg, req, state string) taskflow.TraceRequirement {
	r := l3TR(asg, req, "dropped")
	r.Dropped = state
	return r
}

func l3UncoveredTR(asg, req, reason string) taskflow.TraceRequirement {
	r := l3TR(asg, req, "uncovered")
	r.Uncovered = reason
	return r
}

func l3TC(id, status string, reqs ...taskflow.TraceRequirement) taskflow.TraceCriterion {
	if reqs == nil {
		reqs = []taskflow.TraceRequirement{}
	}
	return taskflow.TraceCriterion{ID: id, Description: "criterion " + id, Status: status, Requirements: reqs}
}

// l3SameJSON compares by JSON, which is what users read: equal values and
// every list an array, never null (R34).
func l3SameJSON(t *testing.T, label string, got, want any) {
	t.Helper()
	g, w := l3JSON(got), l3JSON(want)
	if g != w {
		t.Errorf("%s:\n got  %s\n want %s", label, g, w)
	}
}

// l3Criteria compares the report's criteria and checks its identity members
// (R34): task_id, fingerprint of the current source, problems.
func (f *l3Fix) l3Report(label string, rep taskflow.TraceReport, problems []string, want ...taskflow.TraceCriterion) {
	f.t.Helper()
	rec := f.load()
	if rep.TaskID != rec.ID {
		f.t.Errorf("%s: task_id %q, want %q (R34)", label, rep.TaskID, rec.ID)
	}
	if fp := f.verify().CurrentSnapshot.Fingerprint; rep.Fingerprint != fp {
		f.t.Errorf("%s: fingerprint %q, want the current source's %q (R34)", label, rep.Fingerprint, fp)
	}
	if problems == nil {
		problems = []string{}
	}
	l3SameJSON(f.t, label+" problems", rep.Problems, problems)
	l3SameJSON(f.t, label+" criteria", rep.Criteria, want)
	if strings.Contains(l3JSON(rep), "null") {
		f.t.Errorf("%s: report JSON holds null; every list must be an array (R34): %s", label, l3JSON(rep))
	}
}

// TestTraceJudgesMappedTests fails when a mapped test's status, detail,
// package or runs differ from R28/R29 for failed, skipped, missing and
// not_run tests, when requirement or criterion statuses differ from R35, or
// when verify's problems and limitations differ from R30/R31 (R27: an
// assignment without a completed result is not judged).
func TestTraceJudgesMappedTests(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{
		reqs:   []string{"C1", "C2", "C3", "C4", "C5"},
		checks: []taskflow.Check{l3Check("bad", l3JSONQ...), l3Check("ok", l3JSONP...)},
	})
	files := []string{"p/te_test.go", "p/never_test.go", "p/gone_test.go", "p/renamed_test.go", "n/n_test.go", "q/te_test.go"}
	f.mustAssign(f.brief("te1", []string{"p/te_test.go", "p/never_test.go", "p/gone_test.go", "p/renamed_test.go", "n", "q/te_test.go"},
		l3Req("R1", "C1"), l3Req("R2", "C2"), l3Req("R3", "C3")))
	f.write("p/te_test.go", l3PTests)
	f.write("p/never_test.go", l3NeverTests)
	f.write("p/gone_test.go", "package p\n\nimport \"testing\"\n\nfunc TestGone(t *testing.T) {}\n")
	f.write("p/renamed_test.go", "package p\n\nimport \"testing\"\n\nfunc TestRenamed(t *testing.T) {}\n")
	f.write("n/n_test.go", "package n\n\nimport \"testing\"\n\nfunc TestNested(t *testing.T) {}\n")
	f.write("q/te_test.go", l3QTests)
	r2 := []taskflow.MappedTest{
		l3Mapped("q/te_test.go", "TestFail"), l3Mapped("p/te_test.go", "TestSkip"), l3Mapped("p/never_test.go", "TestNever"),
		l3Mapped("p/gone_test.go", "TestGone"), l3Mapped("p/renamed_test.go", "TestRenamed"), l3Mapped("n/n_test.go", "TestNested"),
		l3Mapped("q/te_test.go", "TestQPass"),
	}
	f.mustSubmit(f.result("te1", "completed", files,
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped("p/te_test.go", "TestSub/a")),
		l3Map("R2", r2...),
		l3Uncovered("R3", "needs fault injection")))
	f.accept("te1", "accepted")
	f.mustAssign(f.brief("te2", []string{"r/r_test.go"}, l3Req("R9", "C4")))

	// After the handoff: one file deleted, one no longer declares its test,
	// and n becomes a nested module.
	f.remove("p/gone_test.go")
	f.write("p/renamed_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOther(t *testing.T) {}\n")
	f.write("n/go.mod", "module example.com/nested\n\ngo 1.22\n")
	ok := f.run("ok")
	bad := f.run("bad")
	if ok.Status != "passed" || bad.Status != "failed" {
		t.Fatalf("fixture runs: ok %q, bad %q; want passed and failed", ok.Status, bad.Status)
	}
	p, q := l3Module+"/p", l3Module+"/q"
	notCurrentPass := fmt.Sprintf("run %s of check bad recorded a PASS, but that run is not a passed latest run at the current source", bad.ID)

	rep := f.trace()
	f.l3Report("trace", rep, nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven",
			l3TT("p/te_test.go", "TestPass", p, "passed", "", l3TraceRun(ok, true, true, 1, 0, 0)),
			l3TT("p/te_test.go", "TestSub/a", p, "passed", "", l3TraceRun(ok, true, true, 1, 0, 0)))),
		l3TC("C2", "open", l3TR("te1", "R2", "unproven",
			l3TT("q/te_test.go", "TestFail", q, "failed", fmt.Sprintf("run %s of check bad recorded a FAIL", bad.ID), l3TraceRun(bad, true, true, 0, 1, 0)),
			l3TT("p/te_test.go", "TestSkip", p, "skipped", fmt.Sprintf("run %s of check ok recorded only SKIP", ok.ID), l3TraceRun(ok, true, true, 0, 0, 1)),
			l3TT("p/never_test.go", "TestNever", p, "not_run", ""),
			l3TT("p/gone_test.go", "TestGone", p, "missing", "p/gone_test.go is absent or not a regular file"),
			l3TT("p/renamed_test.go", "TestRenamed", p, "missing", "p/renamed_test.go declares no func TestRenamed"),
			l3TT("n/n_test.go", "TestNested", "", "missing", "n/n_test.go"+l3NotInPkg),
			l3TT("q/te_test.go", "TestQPass", q, "not_run", notCurrentPass, l3TraceRun(bad, true, true, 1, 0, 0)))),
		l3TC("C3", "open", l3UncoveredTR("te1", "R3", "needs fault injection")),
		l3TC("C4", "open", l3TR("te2", "R9", "pending")),
		l3TC("C5", "unmapped"),
	)

	v := f.verify()
	prefix := "worker assignment te1 requirement R2 test "
	want := []string{
		prefix + "q/te_test.go TestFail failed at the current source: run " + bad.ID + " of check bad recorded a FAIL",
		prefix + "p/te_test.go TestSkip was skipped at the current source: run " + ok.ID + " of check ok recorded only SKIP",
		prefix + "p/never_test.go TestNever has no PASS at the current source",
		prefix + "p/gone_test.go TestGone is missing at the current source: p/gone_test.go is absent or not a regular file",
		prefix + "p/renamed_test.go TestRenamed is missing at the current source: p/renamed_test.go declares no func TestRenamed",
		prefix + "n/n_test.go TestNested is missing at the current source: n/n_test.go" + l3NotInPkg,
		prefix + "q/te_test.go TestQPass has no PASS at the current source: " + notCurrentPass,
	}
	l3SameJSON(t, "verify R30 problems", l3WithPrefix(v.Problems, "worker assignment te1 requirement "), want)
	if got := l3WithPrefix(v.Problems, "worker assignment te2 requirement "); len(got) != 0 {
		t.Errorf("verify judged te2, which has no completed result: %v (R27)", got)
	}
	if !l3Has(v.Limitations, "worker assignment te1 requirement R3 uncovered: needs fault injection") {
		t.Errorf("verify limitations %v lack the uncovered entry (R31)", v.Limitations)
	}
	for _, x := range v.Problems {
		if strings.Contains(x, "R3") && strings.HasPrefix(x, "worker assignment te1 requirement R3") {
			t.Errorf("uncovered requirement added a problem %q (R31)", x)
		}
	}
	if v.Complete {
		t.Errorf("verify complete with failing mapped tests (R30)")
	}
}

// TestTraceDetailsAcrossRuns fails when a test with no per-test evidence lacks
// detail 4, when an uncredited PASS lacks detail 1, when a PASS at an older
// source lacks detail 2 or still counts, or when a test's runs are not every
// terminal run holding its entry, in record order, with current, credited and
// fingerprint as R34 says.
func TestTraceDetailsAcrossRuns(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{
		reqs: []string{"C1", "C2"},
		checks: []taskflow.Check{
			l3Check("ok", l3JSONP...),
			l3Check("norun", "go", "test", "-json", "-run", ".", "./p/..."),
			l3Check("plain", l3PlainP...),
		},
	})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te1", "accepted")
	p := l3Module + "/p"
	expect := func(label, status, detail string, runs ...taskflow.TraceRun) {
		t.Helper()
		req, crit := "unproven", "open"
		if status == "passed" {
			req, crit = "proven", "proven"
		}
		f.l3Report(label, f.trace(), nil,
			l3TC("C1", crit, l3TR("te1", "R1", req, l3TT("p/te_test.go", "TestPass", p, status, detail, runs...))),
			l3TC("C2", "unmapped"))
	}

	expect("before any run", "not_run", l3Rule4)
	if plain := f.run("plain"); plain.Status != "passed" {
		t.Fatalf("plain run %q", plain.Status)
	}
	expect("after a run without -json", "not_run", l3Rule4)
	norun := f.run("norun")
	expect("after an uncredited -json run", "not_run",
		fmt.Sprintf("run %s of check norun recorded a PASS, but its argv earns no per-test credit", norun.ID),
		l3TraceRun(norun, true, false, 1, 0, 0))
	ok := f.run("ok")
	expect("after a credited run", "passed", "",
		l3TraceRun(norun, true, false, 1, 0, 0), l3TraceRun(ok, true, true, 1, 0, 0))

	f.write("p/doc.go", "// Package p changes after the runs.\npackage p\n")
	expect("after the source changed", "not_run",
		fmt.Sprintf("run %s of check ok recorded a PASS, but that run is not a passed latest run at the current source", ok.ID),
		l3TraceRun(norun, false, false, 1, 0, 0), l3TraceRun(ok, false, true, 1, 0, 0))
	again := f.run("ok")
	expect("after running again", "passed", "",
		l3TraceRun(norun, false, false, 1, 0, 0), l3TraceRun(ok, false, true, 1, 0, 0), l3TraceRun(again, true, true, 1, 0, 0))
}

// TestTraceCountsOnlyLatestRuns fails when a PASS in an earlier run of the
// same check counts, when a FAIL in an uncredited or failed run does not
// make the test failed, or when the failed and not_run details do not name
// the runs R29 picks (plan check order, last run in record order).
func TestTraceCountsOnlyLatestRuns(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	flag := filepath.Join(root, "flag")
	flaky := fmt.Sprintf("package p\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestFlaky(t *testing.T) {\n\tif _, err := os.Stat(%q); err != nil {\n\t\tt.Fatal(\"flag file absent\")\n\t}\n}\n", flag)
	// -count=1 keeps go from replaying a cached result: TestFlaky's outcome
	// depends on a file outside the source.
	f := l3New(t, l3Opts{checks: []taskflow.Check{
		l3Check("ok", "go", "test", "-json", "-count=1", "./p/..."),
		l3Check("norun", "go", "test", "-json", "-count=1", "-run", ".", "./p/..."),
	}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go", "p/flaky_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.write("p/flaky_test.go", flaky)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go", "p/flaky_test.go"},
		l3Map("R1", l3Mapped("p/flaky_test.go", "TestFlaky"), l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te1", "accepted")
	p := l3Module + "/p"

	l3WriteFile(t, flag, "present\n")
	run1 := f.run("ok")
	if err := os.Remove(flag); err != nil {
		t.Fatal(err)
	}
	run2 := f.run("norun")
	if run1.Status != "passed" || run2.Status != "failed" {
		t.Fatalf("fixture runs %q, %q; want passed then failed", run1.Status, run2.Status)
	}
	f.l3Report("uncredited failing run", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/flaky_test.go", "TestFlaky", p, "failed", fmt.Sprintf("run %s of check norun recorded a FAIL", run2.ID),
				l3TraceRun(run1, true, true, 1, 0, 0), l3TraceRun(run2, true, false, 0, 1, 0)),
			l3TT("p/te_test.go", "TestPass", p, "passed", "",
				l3TraceRun(run1, true, true, 1, 0, 0), l3TraceRun(run2, true, false, 1, 0, 0)))))

	run3 := f.run("ok")
	f.l3Report("latest run failed", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/flaky_test.go", "TestFlaky", p, "failed", fmt.Sprintf("run %s of check ok recorded a FAIL", run3.ID),
				l3TraceRun(run1, false, true, 1, 0, 0), l3TraceRun(run2, true, false, 0, 1, 0), l3TraceRun(run3, true, true, 0, 1, 0)),
			l3TT("p/te_test.go", "TestPass", p, "not_run",
				fmt.Sprintf("run %s of check ok recorded a PASS, but that run is not a passed latest run at the current source", run3.ID),
				l3TraceRun(run1, false, true, 1, 0, 0), l3TraceRun(run2, true, false, 1, 0, 0), l3TraceRun(run3, true, true, 1, 0, 0)))))
}

// TestTraceGOFLAGSWithoutCredit fails when a PASS from a run whose GOFLAGS the
// Go evidence rule refuses proves a test, or when its detail does not name
// GOFLAGS (R28, R29 detail 1). It sets GOFLAGS, so it does not run in parallel.
func TestTraceGOFLAGSWithoutCredit(t *testing.T) {
	l3NeedNoGOFLAGS(t)
	t.Setenv("GOFLAGS", "-run=TestPass")
	f := l3New(t, l3Opts{ownGOFLAGS: true})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te1", "accepted")
	ok := f.run("ok")
	if ok.Status != "passed" || !strings.Contains(ok.GOFLAGS, "-run") {
		t.Fatalf("fixture run status %q goflags %q; want passed with the -run GOFLAGS recorded", ok.Status, ok.GOFLAGS)
	}
	detail := fmt.Sprintf("run %s of check ok recorded a PASS, but the GOFLAGS it ran with earn no per-test credit", ok.ID)
	f.l3Report("GOFLAGS run", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", l3Module+"/p", "not_run", detail, l3TraceRun(ok, true, false, 1, 0, 0)))))
	want := "worker assignment te1 requirement R1 test p/te_test.go TestPass has no PASS at the current source: " + detail
	if v := f.verify(); !l3Has(v.Problems, want) {
		t.Errorf("verify problems %v lack %q (R30)", v.Problems, want)
	}
}

// TestTraceTruncatedResults fails when a test the run's results could not
// record, because they hit their bound, lacks detail 3, or when a test
// recorded before the bound does not count (R22 through RunCheck, R29).
func TestTraceTruncatedResults(t *testing.T) {
	t.Parallel()
	many := `package b

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
	// Each subtest costs 2*(len("example.com/fix/b")+409)+128 = 980 bytes of
	// the 7 MiB bound, so 7489 fit and 812 bytes remain; TestZzz... costs
	// 1012, more than remains, so it is never recorded.
	last := "TestZ" + strings.Repeat("z", 420)
	// Two credited checks, listed in the reverse of their run order: detail 3
	// names the first in plan order (big2), not the first run (big1).
	f := l3New(t, l3Opts{checks: []taskflow.Check{
		l3Check("big2", "go", "test", "-json", "-count=1", "./b/..."),
		l3Check("big1", "go", "test", "-json", "./b/..."),
	}})
	f.mustAssign(f.brief("te1", []string{"b/b_test.go"}, l3Req("R1", "C1"), l3Req("R2", "C1")))
	f.write("b/b_test.go", many)
	first := "TestMany/00000" + strings.Repeat("x", 395)
	f.mustSubmit(f.result("te1", "completed", []string{"b/b_test.go"},
		l3Map("R1", l3Mapped("b/b_test.go", first)), l3Map("R2", l3Mapped("b/b_test.go", last))))
	f.accept("te1", "accepted")
	big1 := f.run("big1")
	big2 := f.run("big2")
	if big1.Status != "passed" || big2.Status != "passed" {
		t.Fatalf("big runs %q, %q", big1.Status, big2.Status)
	}
	b := l3Module + "/b"
	f.l3Report("truncated results", f.trace(), nil,
		l3TC("C1", "open",
			l3TR("te1", "R1", "proven", l3TT("b/b_test.go", first, b, "passed", "",
				l3TraceRun(big1, true, true, 1, 0, 0), l3TraceRun(big2, true, true, 1, 0, 0))),
			l3TR("te1", "R2", "unproven", l3TT("b/b_test.go", last, b, "not_run",
				fmt.Sprintf("run %s of check big2 stopped recording tests at its bound", big2.ID)))))
}

// TestTraceDropsRequirementsOfUnusedAssignments fails when requirements of an
// assignment closed unused are still judged, are not reported dropped and
// lost, keep their entries after a covering correction declares them, or
// when verify's entries and their order differ from R6, R32, R30, R42 then
// R31, R41 (R27, R34, R35, R41, R42).
func TestTraceDropsRequirementsOfUnusedAssignments(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2", "C3"}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1"), l3Req("R2", "C2")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"},
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass")), l3Map("R2", l3Mapped("p/te_test.go", "TestSkip"))))
	f.accept("te1", "unused")

	f.l3Report("closed unused", f.trace(), nil,
		l3TC("C1", "lost", l3Dropped("te1", "R1", "closed unused")),
		l3TC("C2", "lost", l3Dropped("te1", "R2", "closed unused")),
		l3TC("C3", "unmapped"))
	v := f.verify()
	l3SameJSON(t, "R42 problems", l3WithPrefix(v.Problems, "plan requirement "),
		[]string{"plan requirement C1" + l3LostSuffix, "plan requirement C2" + l3LostSuffix})
	l3SameJSON(t, "R41 limitations", l3WithPrefix(v.Limitations, "requirement "), []string{
		"requirement R1 is no longer mapped: worker assignment te1 declared it and is closed unused",
		"requirement R2 is no longer mapped: worker assignment te1 declared it and is closed unused",
	})
	if got := l3WithPrefix(v.Problems, "worker assignment te1 requirement "); len(got) != 0 {
		t.Errorf("verify judged the closed assignment: %v (R27)", got)
	}

	// A correction covering te1 declares R1 again (and R4); R2 is amended away.
	input := l3Input(t, filepath.Join(f.root, "amended.md"), "amended contract\n")
	b := f.brief("te2", []string{"p/te_test.go"}, l3Req("R1", "C1"), l3Req("R4", "C3"))
	b.Inputs = []taskflow.InputRef{input}
	f.mustAssign(b)
	f.l3Report("correction assigned", f.trace(), nil,
		l3TC("C1", "open", l3TR("te2", "R1", "pending")),
		l3TC("C2", "lost", l3Dropped("te1", "R2", "closed unused")),
		l3TC("C3", "open", l3TR("te2", "R4", "pending")))
	v = f.verify()
	l3SameJSON(t, "R42 problems after correction", l3WithPrefix(v.Problems, "plan requirement "), []string{"plan requirement C2" + l3LostSuffix})
	l3SameJSON(t, "R41 limitations after correction", l3WithPrefix(v.Limitations, "requirement "),
		[]string{"requirement R2 is no longer mapped: worker assignment te1 declared it and is closed unused"})

	// The correction completes: R1 uncovered still links C1 (no R42 for C1).
	f.write("p/te_test.go", l3PTests+"\nfunc TestMore(t *testing.T) {}\n")
	f.mustSubmit(f.result("te2", "completed", []string{"p/te_test.go"},
		l3Uncovered("R1", "amended away"), l3Map("R4", l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te2", "accepted")
	run := f.run("ok")
	if run.TestResults == "" {
		t.Fatalf("run recorded no test_results (R15)")
	}
	// Damage the stored input copy and the run's results file.
	if err := os.WriteFile(filepath.Join(f.dir, f.assignment("te2").InputFiles[0]), []byte("tampered\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.dir, run.TestResults)); err != nil {
		t.Fatal(err)
	}
	r32 := "check run " + run.ID + " test results: stored file cannot be read or differs from its sha256"
	f.l3Report("correction completed", f.trace(), []string{r32},
		l3TC("C1", "open", l3UncoveredTR("te2", "R1", "amended away")),
		l3TC("C2", "lost", l3Dropped("te1", "R2", "closed unused")),
		l3TC("C3", "open", l3TR("te2", "R4", "unproven", l3TT("p/te_test.go", "TestPass", l3Module+"/p", "not_run", l3Rule4))))
	v = f.verify()
	wantProblems := []string{
		"worker assignment te2 input " + input.Path + ": stored copy is missing or differs from its sha256",
		r32,
		"worker assignment te2 requirement R4 test p/te_test.go TestPass has no PASS at the current source: " + l3Rule4,
		"plan requirement C2" + l3LostSuffix,
	}
	if !l3InOrder(v.Problems, wantProblems) {
		t.Errorf("verify problems %v, want %v in this order (R6, R32, R30, R42)", v.Problems, wantProblems)
	}
	if l3Has(v.Problems, "plan requirement C1"+l3LostSuffix) {
		t.Errorf("C1 reported lost although te2 links it with an uncovered entry (R42)")
	}
	wantLimits := []string{
		"worker assignment te2 requirement R1 uncovered: amended away",
		"requirement R2 is no longer mapped: worker assignment te1 declared it and is closed unused",
	}
	if !l3InOrder(v.Limitations, wantLimits) {
		t.Errorf("verify limitations %v, want %v in this order (R31, R41)", v.Limitations, wantLimits)
	}
}

// TestTraceDropsReplacedAndRejectedRequirements fails when a replaced or
// rejected assignment's state is wrong, when a current assignment that does
// not overlap a closed one hides its dropped entry or limitation, or when a
// covering assignment that declares a requirement under another criterion
// keeps the old criterion linked (R34, R35, R41, R42).
func TestTraceDropsReplacedAndRejectedRequirements(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2", "C3"}})
	test := func(name string) string {
		return "package p\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {}\n"
	}
	f.mustAssign(f.brief("te1", []string{"p/a_test.go"}, l3Req("R1", "C1")))
	f.write("p/a_test.go", test("TestA"))
	f.mustSubmit(f.result("te1", "completed", []string{"p/a_test.go"}, l3Map("R1", l3Mapped("p/a_test.go", "TestA"))))
	f.mustAssign(f.brief("te2", []string{"p/b_test.go"}, l3Req("R2", "C2")))
	f.write("p/b_test.go", test("TestB"))
	f.mustSubmit(f.result("te2", "completed", []string{"p/b_test.go"}, l3Map("R2", l3Mapped("p/b_test.go", "TestB"))))
	f.accept("te2", "rejected")
	f.mustAssign(f.brief("te3", []string{"p/a_test.go", "p/b_test.go"}, l3Req("R1", "C3")))
	f.mustAssign(f.brief("te4", []string{"r/r_test.go"}, l3Req("R2", "C2")))

	f.l3Report("replaced and rejected", f.trace(), nil,
		l3TC("C1", "lost", l3Dropped("te1", "R1", "replaced by worker assignment te3")),
		l3TC("C2", "open", l3Dropped("te2", "R2", "rejected"), l3TR("te4", "R2", "pending")),
		l3TC("C3", "open", l3TR("te3", "R1", "pending")))
	v := f.verify()
	l3SameJSON(t, "R42 problems", l3WithPrefix(v.Problems, "plan requirement "), []string{"plan requirement C1" + l3LostSuffix})
	l3SameJSON(t, "R41 limitations", l3WithPrefix(v.Limitations, "requirement "),
		[]string{"requirement R2 is no longer mapped: worker assignment te2 declared it and is rejected"})
	for _, id := range []string{"te1", "te2"} {
		if got := l3WithPrefix(v.Problems, "worker assignment "+id+" requirement "); len(got) != 0 {
			t.Errorf("verify judged closed %s: %v (R27)", id, got)
		}
	}
}

// TestTraceIsReadOnly fails when Trace writes a revision or any file, or
// returns a report when the task cannot be loaded or the source cannot be
// captured (R33).
func TestTraceIsReadOnly(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	f.run("ok")
	f.verify()
	rev := f.revision()
	taskBefore, repoBefore := l3Tree(t, f.dir), l3Tree(t, f.repo)
	f.trace()
	f.trace()
	l3SameTree(t, "Trace (task directory)", taskBefore, l3Tree(t, f.dir))
	l3SameTree(t, "Trace (project)", repoBefore, l3Tree(t, f.repo))
	if got := f.revision(); got != rev {
		t.Errorf("Trace moved the record from revision %d to %d (R33)", rev, got)
	}

	// An abandoned task traces as before and is still not written to.
	before := f.trace()
	if _, err := taskflow.Abandon(f.dir, "fixture abandoned"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	rev = f.revision()
	taskBefore = l3Tree(t, f.dir)
	if after, err := l3TraceErr(f.ctx, f.dir); err != nil {
		t.Errorf("Trace of an abandoned task: %v (R33)", err)
	} else {
		l3SameJSON(t, "trace after abandonment (R33)", after, before)
	}
	l3SameTree(t, "Trace of an abandoned task", taskBefore, l3Tree(t, f.dir))
	if got := f.revision(); got != rev {
		t.Errorf("Trace moved the abandoned record from revision %d to %d (R33)", rev, got)
	}

	ctx := context.Background()
	if _, err := l3TraceErr(ctx, filepath.Join(f.root, "no-such-task")); err == nil {
		t.Errorf("Trace of a missing task directory returned no error (R33)")
	}
	if _, err := l3TraceErr(ctx, t.TempDir()); err == nil {
		t.Errorf("Trace of an empty directory returned no error (R33)")
	}
	if err := os.Rename(f.repo, f.repo+" moved"); err != nil {
		t.Fatal(err)
	}
	if _, err := l3TraceErr(ctx, f.dir); err == nil {
		t.Errorf("Trace returned no error although the project is gone (R33: source cannot be captured)")
	}
}

// TestVerifyResultsFileIntegrity fails when a run's damaged or missing
// results file is not reported once per run in record order, or still counts
// as evidence (R32, R34 problems).
func TestVerifyResultsFileIntegrity(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te1", "accepted")
	run1, run2 := f.run("ok"), f.run("ok")
	if run1.TestResults == "" || run2.TestResults == "" {
		t.Fatalf("runs recorded no test_results (R15)")
	}
	name1 := filepath.Join(f.dir, run1.TestResults)
	data, err := os.ReadFile(name1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name1, append(data, ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.dir, run2.TestResults)); err != nil {
		t.Fatal(err)
	}
	r32 := func(run taskflow.CheckRun) string {
		return "check run " + run.ID + " test results: stored file cannot be read or differs from its sha256"
	}
	f.l3Report("damaged results", f.trace(), []string{r32(run1), r32(run2)},
		l3TC("C1", "open", l3TR("te1", "R1", "unproven", l3TT("p/te_test.go", "TestPass", l3Module+"/p", "not_run", l3Rule4))))
	v := f.verify()
	want := []string{r32(run1), r32(run2), "worker assignment te1 requirement R1 test p/te_test.go TestPass has no PASS at the current source: " + l3Rule4}
	l3SameJSON(t, "verify R32 and R30 problems", append(l3WithPrefix(v.Problems, "check run "), l3WithPrefix(v.Problems, "worker assignment te1 requirement ")...), want)
	if !l3InOrder(v.Problems, want) {
		t.Errorf("verify problems %v, want %v in this order (R32 before R30)", v.Problems, want)
	}
}

var l3NewForms = []*regexp.Regexp{
	regexp.MustCompile(`^worker assignment \S+ input `),
	regexp.MustCompile(`^worker assignment \S+ requirement \S+ (test |uncovered: )`),
	regexp.MustCompile(`^check run \S+ test results: `),
	regexp.MustCompile(`^requirement \S+ is no longer mapped: `),
	regexp.MustCompile(`^plan requirement \S+ lost its mapped tests: `),
}

func l3NoNewForms(t *testing.T, label string, v taskflow.Verification) {
	t.Helper()
	for _, list := range [][]string{v.Problems, v.Limitations} {
		for _, x := range list {
			for _, re := range l3NewForms {
				if re.MatchString(x) {
					t.Errorf("%s: verify added %q", label, x)
				}
			}
		}
	}
}

// TestVerifyAddsNothingWithoutNewMembers fails when a task whose briefs carry
// no inputs or requirements gets an R6, R30, R31, R32, R41 or R42 entry, even
// with a damaged results file (R39).
func TestVerifyAddsNothingWithoutNewMembers(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	f.mustAssign(f.plainBrief("impl", []string{"p/te_test.go"}))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("impl", "completed", []string{"p/te_test.go"}))
	run := f.run("ok")
	if run.TestResults == "" {
		t.Fatalf("run recorded no test_results (R15), so the case below proves nothing")
	}
	intact := f.verify()
	l3NoNewForms(t, "intact results", intact)
	if err := os.Remove(filepath.Join(f.dir, run.TestResults)); err != nil {
		t.Fatal(err)
	}
	missing := f.verify()
	l3NoNewForms(t, "missing results file (R32 applies only when verify judges a test)", missing)
	// The verdict does not depend on whether the run's results exist.
	if missing.Complete != intact.Complete {
		t.Errorf("complete %v with the results file, %v without it (R39)", intact.Complete, missing.Complete)
	}
	l3SameJSON(t, "problems without the results file (R39)", missing.Problems, intact.Problems)
	l3SameJSON(t, "limitations without the results file (R39)", missing.Limitations, intact.Limitations)
}

// TestContractWithoutRequirementsIsNotJudged fails when a contract-bearing
// assignment without requirements, in a plan that cannot prove tests, gets
// judged or traced (R40, new-cw half; R8). Only a small plan takes such a
// brief: a substantial one refuses it (L4 R17).
func TestContractWithoutRequirementsIsNotJudged(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{small: true, checks: []taskflow.Check{l3Check("plain", l3PlainP...), l3Check("norun", "go", "test", "-json", "-run", ".", "./p/...")}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}))
	f.run("plain")
	f.run("norun")
	l3NoNewForms(t, "contract without requirements", f.verify())
	f.l3Report("contract without requirements", f.trace(), nil, l3TC("C1", "unmapped"))
}

// TestTraceModuleBelowProjectRoot fails when worker paths are not taken
// relative to the project root, when a test outside the selected path's
// module is accepted at handoff, or when its package is not resolved from
// the module-relative path (R12, R14, R28).
func TestTraceModuleBelowProjectRoot(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{sub: true})
	if f.p("p/te_test.go") != "mod/p/te_test.go" {
		t.Fatalf("fixture prefix %q", f.prefix)
	}
	f.mustAssign(f.brief("te1", []string{"mod/p/te_test.go", "tools"}, l3Req("R1", "C1")))
	f.write("mod/p/te_test.go", l3PTests)
	f.write("tools/x_test.go", "package tools\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	base := f.result("te1", "completed", []string{"mod/p/te_test.go", "tools/x_test.go"})
	try := func(want string, tests ...taskflow.MappedTest) {
		t.Helper()
		res := base
		res.RequirementMap = []taskflow.RequirementMapping{l3Map("R1", tests...)}
		f.submitFails(res, want)
	}
	try("requirement_map entry R1 test tools/x_test.go TestX is missing at the current source: tools/x_test.go"+l3NotInPkg, l3Mapped("tools/x_test.go", "TestX"))
	try("requirement_map entry R1 test p/te_test.go TestPass: path must be a _test.go file inside the brief's owned_paths", l3Mapped("p/te_test.go", "TestPass"))
	res := base
	res.RequirementMap = []taskflow.RequirementMapping{l3Map("R1", l3Mapped("mod/p/te_test.go", "TestPass"))}
	f.mustSubmit(res)
	f.accept("te1", "accepted")
	ok := f.run("ok")
	f.l3Report("module below the project root", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven",
			l3TT("mod/p/te_test.go", "TestPass", l3Module+"/p", "passed", "", l3TraceRun(ok, true, true, 1, 0, 0)))))
}

var l3JudgedForm = regexp.MustCompile(`^worker assignment \S+ requirement \S+ test `)

// TestVerifyOrdersMappedTestProblems fails when R30 problems do not follow
// assignment record order, then map entry order, then test order, or when a
// criterion's requirements do not follow assignment record order and then
// brief order (R30, R34). Every order here differs from sorted order and the
// map order differs from the brief order.
func TestVerifyOrdersMappedTestProblems(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2"}})
	f.mustAssign(f.brief("zz", []string{"p/z_test.go"}, l3Req("R1", "C1"), l3Req("R2", "C1")))
	f.write("p/z_test.go", "package p\n\nimport \"testing\"\n\nfunc TestZ(t *testing.T) {}\n\nfunc TestA(t *testing.T) {}\n")
	f.mustSubmit(f.result("zz", "completed", []string{"p/z_test.go"},
		l3Map("R2", l3Mapped("p/z_test.go", "TestZ"), l3Mapped("p/z_test.go", "TestA")),
		l3Map("R1", l3Mapped("p/z_test.go", "TestA"))))
	f.accept("zz", "accepted")
	f.mustAssign(f.brief("aa", []string{"p/a_test.go"}, l3Req("R3", "C1")))
	f.write("p/a_test.go", "package p\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")
	f.mustSubmit(f.result("aa", "completed", []string{"p/a_test.go"}, l3Map("R3", l3Mapped("p/a_test.go", "TestB"))))
	f.accept("aa", "accepted")

	var judged []string
	for _, x := range f.verify().Problems {
		if l3JudgedForm.MatchString(x) {
			judged = append(judged, x)
		}
	}
	tail := " has no PASS at the current source: " + l3Rule4
	l3SameJSON(t, "R30 problem order", judged, []string{
		"worker assignment zz requirement R2 test p/z_test.go TestZ" + tail,
		"worker assignment zz requirement R2 test p/z_test.go TestA" + tail,
		"worker assignment zz requirement R1 test p/z_test.go TestA" + tail,
		"worker assignment aa requirement R3 test p/a_test.go TestB" + tail,
	})
	p := l3Module + "/p"
	f.l3Report("requirement order", f.trace(), nil,
		l3TC("C1", "open",
			l3TR("zz", "R1", "unproven", l3TT("p/z_test.go", "TestA", p, "not_run", l3Rule4)),
			l3TR("zz", "R2", "unproven",
				l3TT("p/z_test.go", "TestZ", p, "not_run", l3Rule4),
				l3TT("p/z_test.go", "TestA", p, "not_run", l3Rule4)),
			l3TR("aa", "R3", "unproven", l3TT("p/a_test.go", "TestB", p, "not_run", l3Rule4))),
		l3TC("C2", "unmapped"))
}

// TestTracePicksRunsInPlanCheckOrder fails when the skipped detail or not_run
// detail 1 names the first candidate run in record order instead of the first
// in plan check order (R29). The plan lists each pair of checks in the reverse
// of the order they run.
func TestTracePicksRunsInPlanCheckOrder(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{checks: []taskflow.Check{
		l3Check("s2", "go", "test", "-json", "-count=1", "./p/..."),
		l3Check("s1", l3JSONP...),
		l3Check("u2", "go", "test", "-json", "-run", "Test", "./p/..."),
		l3Check("u1", "go", "test", "-json", "-run", ".", "./p/..."),
	}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"},
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped("p/te_test.go", "TestSkip"))))
	f.accept("te1", "accepted")
	p := l3Module + "/p"

	u1 := f.run("u1")
	u2 := f.run("u2")
	f.l3Report("two uncredited passing runs", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", p, "not_run",
				fmt.Sprintf("run %s of check u2 recorded a PASS, but its argv earns no per-test credit", u2.ID),
				l3TraceRun(u1, true, false, 1, 0, 0), l3TraceRun(u2, true, false, 1, 0, 0)),
			l3TT("p/te_test.go", "TestSkip", p, "not_run", l3Rule4,
				l3TraceRun(u1, true, false, 0, 0, 1), l3TraceRun(u2, true, false, 0, 0, 1)))))

	s1 := f.run("s1")
	s2 := f.run("s2")
	f.l3Report("two credited skipping runs", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", p, "passed", "",
				l3TraceRun(u1, true, false, 1, 0, 0), l3TraceRun(u2, true, false, 1, 0, 0),
				l3TraceRun(s1, true, true, 1, 0, 0), l3TraceRun(s2, true, true, 1, 0, 0)),
			l3TT("p/te_test.go", "TestSkip", p, "skipped", fmt.Sprintf("run %s of check s2 recorded only SKIP", s2.ID),
				l3TraceRun(u1, true, false, 0, 0, 1), l3TraceRun(u2, true, false, 0, 0, 1),
				l3TraceRun(s1, true, true, 0, 0, 1), l3TraceRun(s2, true, true, 0, 0, 1)))))
}
