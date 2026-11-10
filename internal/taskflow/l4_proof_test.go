package taskflow_test

// Proving a mapped PASS (interface contract L4, third version): R7, R7.a, R8,
// R9 and R22 through real go test -json runs. The forging fixtures write
// test2json framing from test code, as the contract's observed go1.27.3
// behaviour describes, so the stored run and pass counts are known by hand.

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// forgeRound assigns te1 over the forging fixture's files, maps them and,
// when accept is set, accepts the round.
func (f *l3Fix) forgeRound(accept bool, rmap ...taskflow.RequirementMapping) {
	f.t.Helper()
	files := l4ForgeFiles()
	var owned []string
	for p := range files {
		owned = append(owned, p)
	}
	sort.Strings(owned)
	var reqs []taskflow.ContractRequirement
	for i, m := range rmap {
		reqs = append(reqs, l3Req(m.Requirement, fmt.Sprintf("C%d", i+1)))
	}
	f.mustAssign(f.brief("te1", owned, reqs...))
	f.apply(files)
	f.mustSubmit(f.result("te1", "completed", owned, rmap...))
	if accept {
		f.accept("te1", "accepted")
	}
}

// TestTraceRunEventProof fails when a mapped test of a gated record is
// passed without a run event per result, with more run events than its limit
// in every run, or with marked output; when the -count of the run, the
// declaration count of R7.a or a proving later run in plan order is not
// honoured; when a pass_unproven detail is not the first that applies for the
// first credited passed run in plan check order; when TraceRun lacks run and
// marked; or when verify's problems differ (R7, R7.a, R8, R9, R22).
func TestTraceRunEventProof(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{reqs: []string{"C1", "C2"}, checks: []taskflow.Check{
		l3Check("ok", l3JSONP...),
		l3Check("c2", "go", "test", "-json", "-count=2", "./p/..."),
	}})
	f.forgeRound(true,
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped("p/dup_test.go", "TestDup"), l3Mapped("p/te_test.go", "TestOnce")),
		l3Map("R2", l3Mapped("p/ghost_test.go", "TestGhost"), l3Mapped("p/te_test.go", "TestSkipped"), l3Mapped("p/te_test.go", "TestTwice"),
			l3Mapped("p/te_test.go", "TestMarked"), l3Mapped("p/te_test.go", "TestBoth")))
	// Run in the reverse of plan order: details name the first run in plan
	// check order (ok), and runs are listed in record order (c2, ok).
	c2 := f.run("c2")
	ok := f.run("ok")
	if ok.Status != "passed" || c2.Status != "passed" {
		t.Fatalf("fixture runs %q %q", ok.Status, c2.Status)
	}
	p := l3Module + "/p"
	tt := func(path, name, status, detail string, runs ...taskflow.TraceRun) taskflow.TraceTest {
		return l3TT(path, name, p, status, detail, runs...)
	}
	f.l3Report("run-event proof", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven",
			// One run event per result, within -count 1 and 2.
			tt("p/te_test.go", "TestPass", "passed", "", l4TR(c2, 2, 0, 0, 2, 0), l4TR(ok, 1, 0, 0, 1, 0)),
			// Declared in p and p_test: two run events per execution are
			// within the limit (R7.a).
			tt("p/dup_test.go", "TestDup", "passed", "", l4TR(c2, 4, 0, 0, 4, 0), l4TR(ok, 2, 0, 0, 2, 0)),
			// Too many run events in ok, proven by c2 (R8.4: some run).
			tt("p/te_test.go", "TestOnce", "passed", "", l4TR(c2, 2, 0, 0, 2, 0), l4TR(ok, 2, 0, 0, 2, 0)))),
		l3TC("C2", "open", l3TR("te1", "R2", "unproven",
			tt("p/ghost_test.go", "TestGhost", "pass_unproven", fmt.Sprintf(l4DetailNoRun, ok.ID, "ok"), l4TR(c2, 2, 0, 0, 0, 0), l4TR(ok, 1, 0, 0, 0, 0)),
			tt("p/te_test.go", "TestSkipped", "pass_unproven", fmt.Sprintf(l4DetailNoRun, ok.ID, "ok"), l4TR(c2, 2, 0, 2, 2, 0), l4TR(ok, 1, 0, 1, 1, 0)),
			tt("p/te_test.go", "TestTwice", "pass_unproven", fmt.Sprintf(l4DetailTooMany, ok.ID, "ok", 2, 1), l4TR(c2, 4, 0, 0, 4, 0), l4TR(ok, 2, 0, 0, 2, 0)),
			tt("p/te_test.go", "TestMarked", "pass_unproven", fmt.Sprintf(l4DetailMarked, ok.ID, "ok"), l4TR(c2, 2, 0, 0, 2, 2), l4TR(ok, 1, 0, 0, 1, 1)),
			// Marked and too many run events: the marked detail comes first.
			tt("p/te_test.go", "TestBoth", "pass_unproven", fmt.Sprintf(l4DetailMarked, ok.ID, "ok"), l4TR(c2, 4, 0, 0, 4, 2), l4TR(ok, 2, 0, 0, 2, 1)))),
	)
	v := f.verify()
	pre := "worker assignment te1 requirement R2 test "
	want := []string{
		pre + "p/ghost_test.go TestGhost" + l4UnprovenPhrase + fmt.Sprintf(l4DetailNoRun, ok.ID, "ok"),
		pre + "p/te_test.go TestSkipped" + l4UnprovenPhrase + fmt.Sprintf(l4DetailNoRun, ok.ID, "ok"),
		pre + "p/te_test.go TestTwice" + l4UnprovenPhrase + fmt.Sprintf(l4DetailTooMany, ok.ID, "ok", 2, 1),
		pre + "p/te_test.go TestMarked" + l4UnprovenPhrase + fmt.Sprintf(l4DetailMarked, ok.ID, "ok"),
		pre + "p/te_test.go TestBoth" + l4UnprovenPhrase + fmt.Sprintf(l4DetailMarked, ok.ID, "ok"),
	}
	l3SameJSON(t, "verify R9 problems", l3WithPrefix(v.Problems, "worker assignment te1 requirement "), want)
	if got := l4AuthorshipProblems(v); len(got) != 0 {
		t.Errorf("verify added authorship problems %v for files an accepted test engineer wrote (R14)", got)
	}
	if v.Complete {
		t.Errorf("verify complete with pass_unproven tests (R9)")
	}
}

// TestTraceRunLimitFromGOFLAGS fails when a run's limit ignores the -count of
// its recorded GOFLAGS (R22, R7). It sets GOFLAGS, so it does not run in
// parallel.
func TestTraceRunLimitFromGOFLAGS(t *testing.T) {
	t.Setenv("GOFLAGS", "-count=2")
	f := l4New(t, l4Opts{ownGOFLAGS: true, reqs: []string{"C1", "C2"}})
	f.forgeRound(true, l3Map("R1", l3Mapped("p/te_test.go", "TestOnce")), l3Map("R2", l3Mapped("p/te_test.go", "TestTwice")))
	ok := f.run("ok")
	if ok.Status != "passed" || ok.GOFLAGS != "-count=2" {
		t.Fatalf("fixture run %q with recorded GOFLAGS %q, want passed with -count=2", ok.Status, ok.GOFLAGS)
	}
	p := l3Module + "/p"
	f.l3Report("GOFLAGS -count", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven", l3TT("p/te_test.go", "TestOnce", p, "passed", "", l4TR(ok, 2, 0, 0, 2, 0)))),
		l3TC("C2", "open", l3TR("te1", "R2", "unproven", l3TT("p/te_test.go", "TestTwice", p, "pass_unproven",
			fmt.Sprintf(l4DetailTooMany, ok.ID, "ok", 4, 2), l4TR(ok, 4, 0, 0, 4, 0)))))
}

// TestTraceRunLimitCPU fails when a run's limit ignores the number of its
// -cpu values or multiplies it by the wrong declaration count (R22, R7.a).
func TestTraceRunLimitCPU(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{reqs: []string{"C1", "C2"}, checks: []taskflow.Check{l3Check("cpu", "go", "test", "-json", "-cpu=1,2", "./p/...")}})
	f.forgeRound(true,
		l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped("p/dup_ext_test.go", "TestDup")),
		l3Map("R2", l3Mapped("p/te_test.go", "TestTwice")))
	cpu := f.run("cpu")
	if cpu.Status != "passed" {
		t.Fatalf("fixture run %q", cpu.Status)
	}
	p := l3Module + "/p"
	f.l3Report("-cpu=1,2", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven",
			l3TT("p/te_test.go", "TestPass", p, "passed", "", l4TR(cpu, 2, 0, 0, 2, 0)),
			l3TT("p/dup_ext_test.go", "TestDup", p, "passed", "", l4TR(cpu, 4, 0, 0, 4, 0)))),
		l3TC("C2", "open", l3TR("te1", "R2", "unproven", l3TT("p/te_test.go", "TestTwice", p, "pass_unproven",
			fmt.Sprintf(l4DetailTooMany, cpu.ID, "cpu", 4, 2), l4TR(cpu, 4, 0, 0, 4, 0)))))
}

// l4FillTests forges run events for 50,000 other names before TestTarget
// runs, so the run counts hit their bound before TestTarget's run event while
// its PASS is still recorded.
const l4FillTests = `package b

import (
	"fmt"
	"os"
	"testing"
)

func TestFill(t *testing.T) {
	for i := 0; i < 50000; i++ {
		fmt.Fprintf(os.Stdout, "\x16=== RUN   F%05d\n", i)
	}
}

func TestTarget(t *testing.T) {}
`

// TestTraceTruncatedRunCounts fails when a PASS whose run event was not
// counted because the run counts hit their bound lacks the "stopped
// recording at its bound" detail, or when a test counted before the bound is
// not passed (R5.b, R8).
func TestTraceTruncatedRunCounts(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{reqs: []string{"C1", "C2"}, checks: []taskflow.Check{l3Check("big", "go", "test", "-json", "./b/...")}})
	f.mustAssign(f.brief("te1", []string{"b/b_test.go"}, l3Req("R1", "C1"), l3Req("R2", "C2")))
	f.write("b/b_test.go", l4FillTests)
	f.mustSubmit(f.result("te1", "completed", []string{"b/b_test.go"},
		l3Map("R1", l3Mapped("b/b_test.go", "TestFill")), l3Map("R2", l3Mapped("b/b_test.go", "TestTarget"))))
	f.accept("te1", "accepted")
	big := f.run("big")
	if big.Status != "passed" {
		t.Fatalf("fixture run %q", big.Status)
	}
	res, err := l3ReadResults(f.dir, big)
	if err != nil || !res.Truncated {
		t.Errorf("stored results truncated = %v (%v), want true: the run counts hit their bound (R5.b, R6)", res.Truncated, err)
	}
	b := l3Module + "/b"
	f.l3Report("truncated run counts", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven", l3TT("b/b_test.go", "TestFill", b, "passed", "", l4TR(big, 1, 0, 0, 1, 0)))),
		l3TC("C2", "open", l3TR("te1", "R2", "unproven", l3TT("b/b_test.go", "TestTarget", b, "pass_unproven",
			fmt.Sprintf(l4DetailBound, big.ID, "big"), l4TR(big, 1, 0, 0, 0, 0)))))
}

// TestTraceNotIndependent fails when a mapped test of a gated record whose
// file is not independently authored is not not_independent with R12's
// status and reason, when not_independent does not precede failed or follow
// missing, when verify lacks R9's problem, or when the status stays after
// the round is accepted (R8, R9, R12).
func TestTraceNotIndependent(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{checks: []taskflow.Check{l3Check("ok", l3JSONP...), l3Check("bad", l3JSONQ...)}})
	owned := []string{"p/gone_test.go", "p/te_test.go", "q/te_test.go"}
	f.mustAssign(f.brief("te1", owned, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.write("q/te_test.go", l3QTests)
	f.write("p/gone_test.go", l4Test("p", "TestGone"))
	f.mustSubmit(f.result("te1", "completed", owned, l3Map("R1",
		l3Mapped("p/te_test.go", "TestPass"), l3Mapped("q/te_test.go", "TestFail"), l3Mapped("p/gone_test.go", "TestGone"))))
	f.remove("p/gone_test.go")
	ok, bad := f.run("ok"), f.run("bad")
	if ok.Status != "passed" || bad.Status != "failed" {
		t.Fatalf("fixture runs %q %q", ok.Status, bad.Status)
	}
	p, q := l3Module+"/p", l3Module+"/q"
	reason := func(path string) string {
		return path + " is unowned: " + fmt.Sprintf(l4FaultBytes, "te1", "it has no acceptance")
	}
	gone := "p/gone_test.go is absent or not a regular file"
	f.l3Report("unaccepted round", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", p, "not_independent", reason("p/te_test.go"), l4TR(ok, 1, 0, 0, 1, 0)),
			l3TT("q/te_test.go", "TestFail", q, "not_independent", reason("q/te_test.go"), l4TR(bad, 0, 1, 0, 1, 0)),
			l3TT("p/gone_test.go", "TestGone", p, "missing", gone))))
	pre := "worker assignment te1 requirement R1 test "
	l3SameJSON(t, "verify R9 problems", l3WithPrefix(f.verify().Problems, pre), []string{
		pre + "p/te_test.go TestPass" + l4NotIndependent + reason("p/te_test.go"),
		pre + "q/te_test.go TestFail" + l4NotIndependent + reason("q/te_test.go"),
		pre + "p/gone_test.go TestGone is missing at the current source: " + gone,
	})

	// Acceptance needs the owned files as handed off: restore the deleted
	// file, accept, and delete it again, back to the source of the runs.
	f.write("p/gone_test.go", l4Test("p", "TestGone"))
	f.accept("te1", "accepted")
	f.remove("p/gone_test.go")
	f.l3Report("accepted round", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", p, "passed", "", l4TR(ok, 1, 0, 0, 1, 0)),
			l3TT("q/te_test.go", "TestFail", q, "failed", fmt.Sprintf("run %s of check bad recorded a FAIL", bad.ID), l4TR(bad, 0, 1, 0, 1, 0)),
			l3TT("p/gone_test.go", "TestGone", p, "missing", gone))))
	if got := l3WithPrefix(f.verify().Problems, pre+"p/te_test.go "); len(got) != 0 {
		t.Errorf("verify still reports TestPass of an accepted round: %v (R9)", got)
	}
}

// TestTraceNotIndependentWhenManifestUnreadable fails when a mapped test of
// a gated record whose baseline manifest cannot be read is not
// not_independent with R12.0's unowned reason, or when verify lacks R9's or
// R16's problem (R8, R9, R12.0, R16).
func TestTraceNotIndependentWhenManifestUnreadable(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	f.accept("te1", "accepted")
	ok := f.run("ok")
	manifest := f.manifestFile()
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	detail := "p/te_test.go is unowned: " + l4CannotJudge + "baseline manifest hash differs"
	f.l3Report("unreadable manifest", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/te_test.go", "TestPass", l3Module+"/p", "not_independent", detail, l4TR(ok, 1, 0, 0, 1, 0)))))
	v := f.verify()
	pre := "worker assignment te1 requirement R1 test "
	l3SameJSON(t, "verify R9 problems", l3WithPrefix(v.Problems, pre), []string{pre + "p/te_test.go TestPass" + l4NotIndependent + detail})
	l3SameJSON(t, "verify R16 problems", l4AuthorshipProblems(v), []string{l4CannotJudge + "baseline manifest hash differs"})
}

// TestTraceNotIndependentOnlyGated fails when a record with test rules whose
// plan is small judges authorship of a mapped test (R8: only in a gated
// record).
func TestTraceNotIndependentOnlyGated(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{small: true})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
	ok := f.run("ok")
	p := l3Module + "/p"
	f.l3Report("small plan", f.trace(), nil,
		l3TC("C1", "proven", l3TR("te1", "R1", "proven", l3TT("p/te_test.go", "TestPass", p, "passed", "", l4TR(ok, 1, 0, 0, 1, 0)))))
	if got := l4AuthorshipProblems(f.verify()); len(got) != 0 {
		t.Errorf("verify of a small plan added %v (R14-R16 apply to gated records)", got)
	}
}

// TestTraceSourceAwareIsNotCoverage fails when a mapped test whose file is
// source_aware counts as proven coverage, lacks the not_independent detail
// naming the citation, or makes verify add an R14 problem (R8, R9, R12.4).
func TestTraceSourceAwareIsNotCoverage(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	// The round also changes a source file, which excludes its agent: its
	// writes are not independent (Terms).
	owned := []string{"p/helper.go", "p/repro_test.go"}
	f.mustAssign(f.brief("te1", owned, l3Req("R1", "C1")))
	f.write("p/repro_test.go", l4Test("p", "TestRepro"))
	f.write("p/helper.go", "package p\n\nfunc helper() int { return 2 }\n")
	f.mustSubmit(f.result("te1", "completed", owned, l3Map("R1", l3Mapped("p/repro_test.go", "TestRepro"))))
	f.accept("te1", "accepted")
	ok := f.run("ok")
	f.citer("ev1", "evidence", l4Cite{"f1", "p/repro_test.go"})
	f.judge("ev1", "accepted", []string{"ok"}, "resolved")
	detail := "p/repro_test.go is source_aware: " + fmt.Sprintf(l4ReasonRepro, "f1", "ev1")
	f.l3Report("source-aware test", f.trace(), nil,
		l3TC("C1", "open", l3TR("te1", "R1", "unproven",
			l3TT("p/repro_test.go", "TestRepro", l3Module+"/p", "not_independent", detail, l4TR(ok, 1, 0, 0, 1, 0)))))
	v := f.verify()
	pre := "worker assignment te1 requirement R1 test "
	l3SameJSON(t, "verify R9 problems", l3WithPrefix(v.Problems, pre), []string{pre + "p/repro_test.go TestRepro" + l4NotIndependent + detail})
	if got := l4AuthorshipProblems(v); len(got) != 0 {
		t.Errorf("verify added %v for a source_aware file (R14: source_aware adds nothing)", got)
	}
	for _, x := range v.Problems {
		if strings.HasPrefix(x, "test file p/repro_test.go") {
			t.Errorf("verify reported the source-aware file: %q (R14)", x)
		}
	}
}

// l4SatMain makes the test binary write one forged run and PASS pair for
// TestX from TestMain and exit before m.Run, so a run with a huge -count
// still ends at once (observed go1.27.3 behaviour: one run and one pass event
// for TestX, exit 0).
const l4SatMain = `package p

import (
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	fmt.Fprint(os.Stdout, "\x16=== RUN   TestX\n\x16--- PASS: TestX (0.00s)\n")
	os.Exit(0)
}
`

// TestTraceRunLimitSaturates fails when a test's limit in a run (the run's
// limit times the test's declaration count) does not saturate at the largest
// int: with TestX declared in package p and in package p_test (declaration
// count 2) and a -count whose product with 2 exceeds the largest int, one run
// event and one PASS must prove TestX (R7, R7.a, R22).
func TestTraceRunLimitSaturates(t *testing.T) {
	t.Parallel()
	for _, count := range []string{"4611686018427387904", "9223372036854775807"} {
		t.Run("count="+count, func(t *testing.T) {
			t.Parallel()
			f := l4New(t, l4Opts{checks: []taskflow.Check{l3Check("sat", "go", "test", "-json", "-count="+count, "./p/...")}})
			files := map[string]string{
				"p/main_test.go":  l4SatMain,
				"p/x_test.go":     l4Test("p", "TestX"),
				"p/x_ext_test.go": l4Test("p_test", "TestX"),
			}
			owned := []string{"p/main_test.go", "p/x_ext_test.go", "p/x_test.go"}
			f.mustAssign(f.brief("te1", owned, l3Req("R1", "C1")))
			f.apply(files)
			f.mustSubmit(f.result("te1", "completed", owned, l3Map("R1", l3Mapped("p/x_test.go", "TestX"))))
			f.accept("te1", "accepted")
			sat := f.run("sat")
			if sat.Status != "passed" {
				t.Fatalf("fixture run %q, want passed", sat.Status)
			}
			f.l3Report("saturated limit", f.trace(), nil,
				l3TC("C1", "proven", l3TR("te1", "R1", "proven",
					l3TT("p/x_test.go", "TestX", l3Module+"/p", "passed", "", l4TR(sat, 1, 0, 0, 1, 0)))))
			if got := l3WithPrefix(f.verify().Problems, "worker assignment te1 requirement "); len(got) != 0 {
				t.Errorf("verify reports %v for a proven test (R9)", got)
			}
		})
	}
}
