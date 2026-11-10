package taskflow_test

// Test files and authorship (interface contract L4, third version): R11-R16,
// R19 and R23, with I4-I8, through taskflow.Authorship and taskflow.Verify.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// l4Unformatted parses but is not gofmt-formatted; l4Formatted is its
// gofmt form, so both have the same gofmt hash.
const (
	l4Unformatted = "package p\n\nimport \"testing\"\n\nfunc TestFmt(t *testing.T)    {   }\n"
	l4Formatted   = "package p\n\nimport \"testing\"\n\nfunc TestFmt(t *testing.T) {}\n"
)

// TestAuthorshipStatuses fails when a changed test file of a gated record
// gets another status or reason than R12 gives (exact, gofmt-equivalent and
// deletion writes of an independent round, lost, unowned without a write and
// unowned with a write that carries no contract), when a file that is not a
// test file or whose state did not change is listed, when the report's
// members differ from R19, or when verify's R14 problems differ from the
// report (R11, R12, R14, R19, I4).
func TestAuthorshipStatuses(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{files: map[string]string{
		"p/edit_test.go": l4Test("p", "TestEdit"),
		"p/gone_test.go": l4Test("p", "TestGone"),
		"p/del_test.go":  l4Test("p", "TestDel"),
		"p/mode_test.go": l4Test("p", "TestMode"),
		"p/keep_test.go": l4Test("p", "TestKeep"),
	}})
	if got, ok := l4Gofmt([]byte(l4Unformatted)); !ok || got != l3Sum([]byte(l4Formatted)) {
		t.Fatalf("fixture: the unformatted file's gofmt form is not l4Formatted")
	}
	f.te("te1", "", "completed", map[string]string{
		"p/a_test.go":        l4Test("p", "TestA"),
		"p/edit_test.go":     l4Test("p", "TestEdit", "TestEdit2"),
		"p/gone_test.go":     "",
		"p/fmt_test.go":      l4Unformatted,
		"p/lost_test.go":     l4Test("p", "TestLost"),
		"p/testdata/new.txt": "golden\n",
	})
	f.accept("te1", "accepted")
	f.plain("impl", "", map[string]string{
		"p/impl_test.go": l4Test("p", "TestImpl"),
		"p/p.go":         "package p\n\n// F is the contract symbol.\nfunc F() int { return 2 }\n",
	})
	// Changes no worker result records.
	f.write("p/fmt_test.go", l4Formatted)
	f.write("p/lost_test.go", l4Test("p", "TestLost", "TestMore"))
	f.write("p/stray_test.go", l4Test("p", "TestStray"))
	f.remove("p/del_test.go")
	if err := os.Chmod(f.abs("p/mode_test.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Changed files that are not test files (R10): no Go file in q, vendor
	// and underscore directories, a nested module, a source file.
	f.write("q/testdata/a", "data\n")
	f.write("vendor/v/v_test.go", l4Test("v", "TestV"))
	f.write("_tools/t_test.go", l4Test("t", "TestT"))
	f.write("nested/go.mod", "module example.com/nested\n\ngo 1.22\n")
	f.write("nested/n_test.go", l4Test("n", "TestN"))

	unowned := func(path, reason string) string { return "test file " + path + " is unowned: " + reason }
	rep := f.authorship()
	f.l4Report("statuses", rep, l4Rules, true, nil,
		f.l4File("p/a_test.go", false, "independent", fmt.Sprintf(l4ReasonBytes, "te1"), "te1", ""),
		f.l4File("p/del_test.go", true, "unowned", l4ReasonNoDelete, "", ""),
		f.l4File("p/edit_test.go", false, "independent", fmt.Sprintf(l4ReasonBytes, "te1"), "te1", ""),
		f.l4File("p/fmt_test.go", false, "independent", fmt.Sprintf(l4ReasonGofmt, "te1"), "te1", ""),
		f.l4File("p/gone_test.go", true, "independent", fmt.Sprintf(l4ReasonDeletion, "te1"), "te1", ""),
		f.l4File("p/impl_test.go", false, "unowned", fmt.Sprintf(l4FaultBytes, "impl", "it carries no contract"), "impl", ""),
		f.l4File("p/lost_test.go", false, "lost", fmt.Sprintf(l4ReasonLost, "te1"), "te1", ""),
		f.l4File("p/stray_test.go", false, "unowned", l4ReasonNoBytes, "", ""),
		f.l4File("p/testdata/new.txt", false, "independent", fmt.Sprintf(l4ReasonBytes, "te1"), "te1", ""),
	)
	v := f.verify()
	l3SameJSON(t, "verify R14 problems", l4AuthorshipProblems(v), []string{
		unowned("p/del_test.go", l4ReasonNoDelete),
		unowned("p/impl_test.go", fmt.Sprintf(l4FaultBytes, "impl", "it carries no contract")),
		"test file p/lost_test.go is lost: " + fmt.Sprintf(l4ReasonLost, "te1"),
		unowned("p/stray_test.go", l4ReasonNoBytes),
	})
	if v.Complete {
		t.Errorf("verify complete with lost and unowned test files (I5)")
	}
	// I4: every lost or unowned file of the report is exactly one R14
	// problem with the same status and reason.
	for _, x := range rep.Files {
		want := "test file " + x.Path + " is " + x.Status + ": " + x.Reason
		if (x.Status == "lost" || x.Status == "unowned") != l3Has(v.Problems, want) {
			t.Errorf("report entry %s and verify disagree: problem %q present %v (I4)", x.Path, want, l3Has(v.Problems, want))
		}
	}
}

// TestAuthorshipFaults fails when an unowned file's reason does not name the
// first R12.5 fault of the last write of its current state, when faults 6-9
// do not name the first such assignment in record order, when an accepted
// round later closed unused or an unused round loses authorship, when an
// independent write is not preferred over a later non-independent write of
// the same state, when the last independent write with the current state is
// not the one named, or when writes of test-side paths outside the test files
// exclude an agent (R12, Terms, I8).
func TestAuthorshipFaults(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	body := func(name string) map[string]string {
		return map[string]string{"p/" + strings.ToLower(name) + "_test.go": l4Test("p", "Test"+name)}
	}
	f.te("blk", "", "blocked", body("F2"))
	f.te("noacc", "", "completed", body("F3"))
	f.te("rej", "", "completed", body("F4"))
	f.accept("rej", "accepted")
	f.accept("rej", "rejected")
	f.te("rej2", "", "completed", body("F4b"))
	f.accept("rej2", "rejected")
	f.accept("rej2", "accepted")
	f.te("rej3", "", "completed", body("F4c"))
	f.accept("rej3", "rejected")
	f.accept("rej3", "unused")
	f.te("crd", l3Coordinator, "completed", body("F5"))
	f.accept("crd", "accepted")
	f.te("crd2", l3Coordinator, "completed", body("F5b"))
	// agent-six holds a verification assignment first, then an
	// implementation assignment without a contract: fault 6 comes first.
	f.mustAssign(f.roleBrief("ver6", "agent-six", "verification", []string{"ok"}))
	f.mustAssign(func() taskflow.WorkerBrief {
		b := f.plainBrief("plain6", []string{"p/unused6.go"})
		b.AgentID = "agent-six"
		return b
	}())
	f.mustAssign(func() taskflow.WorkerBrief {
		b := f.plainBrief("plain6b", []string{"p/unused6b.go"})
		b.AgentID = "agent-six"
		return b
	}())
	f.te("six", "agent-six", "completed", body("F6"))
	f.accept("six", "accepted")
	f.review("review7", "agent-seven")
	f.te("seven", "agent-seven", "completed", body("F7"))
	f.accept("seven", "accepted")
	f.mustAssign(f.roleBrief("ver8", "agent-eight", "verification", []string{"ok"}))
	f.te("eight", "agent-eight", "completed", body("F8"))
	f.accept("eight", "accepted")
	// The round itself changes a source file after its test file.
	f.te("nine", "", "completed", map[string]string{"p/f9_test.go": l4Test("p", "TestF9"), "p/f9.go": "package p\n\nfunc f9() {}\n"},
		"p/f9_test.go", "p/f9.go")
	f.accept("nine", "accepted")
	// Test-side paths that are not test files do not exclude the agent.
	f.te("td", "", "completed", map[string]string{
		"p/td_test.go": l4Test("p", "TestTD"), "vendor/v/v_test.go": l4Test("v", "TestV"), "q/testdata/blob": "x\n",
	})
	f.accept("td", "accepted")
	f.te("unu", "", "completed", body("U1"))
	f.accept("unu", "accepted")
	f.accept("unu", "unused")
	f.te("unu2", "", "completed", body("U2"))
	f.accept("unu2", "unused")
	// Two independent rounds: l_test.go returns to l1's bytes, m_test.go to
	// neither.
	v1 := map[string]string{"p/l_test.go": l4Test("p", "TestL"), "p/m_test.go": l4Test("p", "TestM")}
	v2 := map[string]string{"p/l_test.go": l4Test("p", "TestL", "TestL2"), "p/m_test.go": l4Test("p", "TestM", "TestM2")}
	f.te("l1", "", "completed", v1)
	f.accept("l1", "accepted")
	f.te("l2", "", "completed", v2)
	f.accept("l2", "accepted")
	f.write("p/l_test.go", v1["p/l_test.go"])
	f.write("p/m_test.go", l4Test("p", "TestM", "TestM3"))
	// An independent write, then a write of the same state by an
	// implementation round without a contract.
	f.te("ind", "", "completed", body("Same"))
	f.accept("ind", "accepted")
	f.plain("imp2", "", body("Same"))

	fault := func(asg, text string) string { return fmt.Sprintf(l4FaultBytes, asg, text) }
	file := func(path, status, reason, asg string) taskflow.AuthorshipFile {
		return f.l4File(path, false, status, reason, asg, "")
	}
	f.l4Report("faults", f.authorship(), l4Rules, true, nil,
		file("p/f2_test.go", "unowned", fault("blk", "its handoff is not completed"), "blk"),
		file("p/f3_test.go", "unowned", fault("noacc", "it has no acceptance"), "noacc"),
		file("p/f4_test.go", "unowned", fault("rej", "it has a rejected acceptance"), "rej"),
		file("p/f4b_test.go", "unowned", fault("rej2", "it has a rejected acceptance"), "rej2"),
		file("p/f4c_test.go", "unowned", fault("rej3", "it has a rejected acceptance"), "rej3"),
		file("p/f5_test.go", "unowned", fault("crd", "its agent "+l3Coordinator+" is the coordinator"), "crd"),
		file("p/f5b_test.go", "unowned", fault("crd2", "it has no acceptance"), "crd2"),
		file("p/f6_test.go", "unowned", fault("six", "its agent agent-six holds worker assignment plain6, an implementation assignment without a contract"), "six"),
		file("p/f7_test.go", "unowned", fault("seven", "its agent agent-seven holds worker assignment review7, a review assignment"), "seven"),
		file("p/f8_test.go", "unowned", fault("eight", "its agent agent-eight holds worker assignment ver8, a verification assignment"), "eight"),
		file("p/f9_test.go", "unowned", fault("nine", "its agent agent-nine holds worker assignment nine, whose result changed p/f9.go, which is neither a _test.go file nor under a testdata directory"), "nine"),
		file("p/l_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "l1"), "l1"),
		file("p/m_test.go", "lost", fmt.Sprintf(l4ReasonLost, "l2"), "l2"),
		file("p/same_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "ind"), "ind"),
		file("p/td_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "td"), "td"),
		file("p/u1_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "unu"), "unu"),
		file("p/u2_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "unu2"), "unu2"),
	)
	var want []string
	for _, x := range f.authorship().Files {
		if x.Status == "lost" || x.Status == "unowned" {
			want = append(want, "test file "+x.Path+" is "+x.Status+": "+x.Reason)
		}
	}
	l3SameJSON(t, "verify R14 problems (I4)", l4AuthorshipProblems(f.verify()), want)
}

// TestAuthorshipCitations fails when a reproduction test cited by a resolved
// finding of an accepted or unused review-type round that bound a check is
// not source_aware, when the first valid citation is not the one named, when
// an invalid citation (a pre-task file, other current bytes, no check run)
// does not make the file unowned with the citation's reason and finding, when
// a rejected disposition, a rejected round or a round that was ever rejected
// cites, or when a citation overrides an independent write (R12.4, R12.5,
// R14, R19, I6, I8).
func TestAuthorshipCitations(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{files: map[string]string{"p/base_test.go": l4Test("p", "TestBase")}})
	names := []string{"base", "r1", "r2", "r3", "r5", "r6", "r7", "r9", "r10"}
	impl := map[string]string{}
	for _, n := range names {
		impl["p/"+n+"_test.go"] = l4Test("p", "Test"+strings.ToUpper(n[:1])+n[1:], "TestNew"+n)
	}
	f.plain("impl", "", impl)
	f.accept("impl", "accepted")
	f.te("te1", "", "completed", map[string]string{"p/r8_test.go": l4Test("p", "TestR8")})
	f.accept("te1", "accepted")
	ok := f.run("ok")
	if ok.Status != "passed" {
		t.Fatalf("fixture run %q", ok.Status)
	}
	checks := []string{"ok"}
	f.citer("ev1", "evidence", l4Cite{"f1", "p/r1_test.go"}, l4Cite{"f3", "p/r3_test.go"}, l4Cite{"f4", "p/base_test.go"}, l4Cite{"f8", "p/r8_test.go"})
	f.judge("ev1", "accepted", checks, "resolved")
	f.citer("ev2", "evidence", l4Cite{"g1", "p/r2_test.go"}, l4Cite{"g5", "p/r5_test.go"})
	f.judge("ev2", "accepted", nil, "resolved")
	f.citer("ev3", "verification", l4Cite{"h5", "p/r5_test.go"})
	f.judge("ev3", "accepted", checks, "resolved")
	f.citer("ev4", "evidence", l4Cite{"k6", "p/r6_test.go"})
	f.judge("ev4", "rejected", checks, "resolved")
	f.citer("ev5", "evidence", l4Cite{"m7", "p/r7_test.go"})
	f.judge("ev5", "accepted", checks, "rejected")
	f.citer("ev6", "evidence", l4Cite{"n9", "p/r9_test.go"})
	f.judge("ev6", "rejected", checks, "resolved")
	f.judge("ev6", "accepted", checks, "resolved")
	f.citer("ev7", "evidence", l4Cite{"o10", "p/r10_test.go"})
	f.judge("ev7", "unused", checks, "resolved")
	// After the citation, r3 changes: the cited bytes are no longer current.
	f.write("p/r3_test.go", l4Test("p", "TestR3", "TestChanged"))

	noContract := fmt.Sprintf(l4FaultBytes, "impl", "it carries no contract")
	file := func(path, status, reason, asg, finding string) taskflow.AuthorshipFile {
		return f.l4File(path, false, status, reason, asg, finding)
	}
	rep := f.authorship()
	f.l4Report("citations", rep, l4Rules, true, nil,
		file("p/base_test.go", "unowned", fmt.Sprintf(l4CitationNoHold, "f4", "ev1"), "ev1", "f4"),
		file("p/r10_test.go", "source_aware", fmt.Sprintf(l4ReasonRepro, "o10", "ev7"), "ev7", "o10"),
		file("p/r1_test.go", "source_aware", fmt.Sprintf(l4ReasonRepro, "f1", "ev1"), "ev1", "f1"),
		file("p/r2_test.go", "unowned", fmt.Sprintf(l4CitationNoHold, "g1", "ev2"), "ev2", "g1"),
		file("p/r3_test.go", "unowned", fmt.Sprintf(l4CitationNoHold, "f3", "ev1"), "ev1", "f3"),
		file("p/r5_test.go", "source_aware", fmt.Sprintf(l4ReasonRepro, "h5", "ev3"), "ev3", "h5"),
		file("p/r6_test.go", "unowned", noContract, "impl", ""),
		file("p/r7_test.go", "unowned", noContract, "impl", ""),
		file("p/r8_test.go", "independent", fmt.Sprintf(l4ReasonBytes, "te1"), "te1", ""),
		file("p/r9_test.go", "unowned", noContract, "impl", ""),
	)
	var want []string
	for _, x := range rep.Files {
		if x.Status == "lost" || x.Status == "unowned" {
			want = append(want, "test file "+x.Path+" is "+x.Status+": "+x.Reason)
		}
	}
	l3SameJSON(t, "verify R14 problems (I4)", l4AuthorshipProblems(f.verify()), want)
}

// TestHandoffGofmt fails when a handoff of a contract-bearing result in a
// record with test rules does not check gofmt_sha256 by R13 (shape before
// ownership, bytes after the hash check, eligibility) or does not fill it
// with the gofmt hash of each eligible entry's bytes, and only of those
// (R13).
func TestHandoffGofmt(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{files: map[string]string{"p/gone_test.go": l4Test("p", "TestGone")}})
	owned := []string{"p/a_test.go", "p/b_test.go", "p/bad_test.go", "p/gone_test.go", "p/testdata/c_test.go"}
	f.mustAssign(f.brief("te1", owned, l3Req("R1", "C1")))
	f.write("p/a_test.go", l4Unformatted)
	f.write("p/b_test.go", l4Test("p", "TestB"))
	f.write("p/bad_test.go", "package p\n\nfunc TestBad(t *testing.T) {\n")
	f.write("p/testdata/c_test.go", l4Test("p", "TestC"))
	f.write("p/other_test.go", l4Test("p", "TestOther"))
	f.remove("p/gone_test.go")
	base := f.result("te1", "completed", owned, l3Uncovered("R1", l4UncoveredReason))
	fmtA, _ := l4Gofmt([]byte(l4Unformatted))
	fmtB, _ := l4Gofmt([]byte(l4Test("p", "TestB")))
	if _, ok := l4Gofmt([]byte("package p\n\nfunc TestBad(t *testing.T) {\n")); ok {
		t.Fatal("fixture: the broken file has a gofmt hash")
	}
	with := func(edit func(cf []taskflow.WorkerChangedFile) []taskflow.WorkerChangedFile) taskflow.WorkerResult {
		r := base
		r.ChangedFiles = edit(append([]taskflow.WorkerChangedFile{}, base.ChangedFiles...))
		return r
	}
	set := func(path, value string) func([]taskflow.WorkerChangedFile) []taskflow.WorkerChangedFile {
		return func(cf []taskflow.WorkerChangedFile) []taskflow.WorkerChangedFile {
			for i := range cf {
				if cf[i].Path == path {
					cf[i].GofmtSHA256 = value
				}
			}
			return cf
		}
	}
	other := fmtA[:63] + "0"
	if other == fmtA {
		other = fmtA[:63] + "1"
	}
	// R13.a shape.
	f.submitFails(with(set("p/a_test.go", fmtA[:63])), l4ShapeMsg)
	f.submitFails(with(set("p/a_test.go", strings.Repeat("z", 64))), l4ShapeMsg)
	f.submitFails(with(set("p/a_test.go", fmtA+"0")), l4ShapeMsg)
	f.submitFails(with(set("p/gone_test.go", fmtB)), l4ShapeMsg)
	// R13.a eligibility.
	f.submitFails(with(set("p/testdata/c_test.go", fmtB)), fmt.Sprintf(l4OnlyMsg, "p/testdata/c_test.go"))
	// R13.b: an eligible entry without a gofmt hash refuses an explicit one.
	f.submitFails(with(set("p/bad_test.go", fmtB)), fmt.Sprintf(l4OnlyMsg, "p/bad_test.go"))
	// R13.b: an explicit value that is not the gofmt hash.
	f.submitFails(with(set("p/a_test.go", other)), fmt.Sprintf(l4DiffersMsg, "p/a_test.go"))
	// R13.b runs after the hash check of the same entry.
	f.submitFails(with(func(cf []taskflow.WorkerChangedFile) []taskflow.WorkerChangedFile {
		cf = set("p/a_test.go", other)(cf)
		for i := range cf {
			if cf[i].Path == "p/a_test.go" {
				cf[i].SHA256 = strings.Repeat("0", 64)
			}
		}
		return cf
	}), fmt.Sprintf(l4HashDiffersMsg, "p/a_test.go"))
	// R13.a runs before the ownership check: p/other_test.go is not owned.
	data, _ := os.ReadFile(f.abs("p/other_test.go"))
	f.submitFails(with(func(cf []taskflow.WorkerChangedFile) []taskflow.WorkerChangedFile {
		return append(cf, taskflow.WorkerChangedFile{Path: "p/other_test.go", SHA256: l3Sum(data), GofmtSHA256: "abc"})
	}), l4ShapeMsg)
	// An explicit correct value for b, omitted elsewhere: cw fills a.
	f.mustSubmit(with(set("p/b_test.go", fmtB)))
	got := map[string]string{}
	present := map[string]bool{}
	for _, x := range f.storedResult("te1")["changed_files"].([]any) {
		m := x.(map[string]any)
		v, ok := m["gofmt_sha256"].(string)
		got[m["path"].(string)], present[m["path"].(string)] = v, ok
	}
	want := map[string]string{"p/a_test.go": fmtA, "p/b_test.go": fmtB}
	for _, p := range owned {
		if got[p] != want[p] || present[p] != (want[p] != "") {
			t.Errorf("stored gofmt_sha256 of %s = %q (member present %v), want %q (R13: filled for eligible entries with a gofmt hash only)", p, got[p], present[p], want[p])
		}
	}
	if fmtA == l3Sum([]byte(l4Unformatted)) {
		t.Fatal("fixture: the unformatted file is already gofmt-formatted")
	}
}

// TestHandoffGofmtNotContractBearing fails when a result without a contract
// in a record with test rules accepts or stores a gofmt_sha256 (R13).
func TestHandoffGofmtNotContractBearing(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	f.mustAssign(f.plainBrief("impl", []string{"p/a_test.go"}))
	f.write("p/a_test.go", l4Test("p", "TestA"))
	res := f.result("impl", "completed", []string{"p/a_test.go"})
	h, _ := l4Gofmt([]byte(l4Test("p", "TestA")))
	bad := res
	bad.ChangedFiles = []taskflow.WorkerChangedFile{{Path: "p/a_test.go", SHA256: res.ChangedFiles[0].SHA256, GofmtSHA256: h}}
	f.submitFails(bad, fmt.Sprintf(l4OnlyMsg, "p/a_test.go"))
	f.mustSubmit(res)
	if s := l3JSON(f.storedResult("impl")); strings.Contains(s, "gofmt_sha256") {
		t.Errorf("a result without a contract stored gofmt_sha256: %s (R13)", s)
	}
}

// TestVerifyPlanCannotProveTests fails when a gated record whose plan cannot
// prove tests does not add R15's problem once, before the R14 problems,
// while some changed test file is not source_aware, or adds it when every
// changed test file is source_aware or none changed (R15).
func TestVerifyPlanCannotProveTests(t *testing.T) {
	t.Parallel()
	unit := []taskflow.Check{l3Check("unit", l3PlainP...)}
	f := l4New(t, l4Opts{checks: unit})
	if got := l4AuthorshipProblems(f.verify()); len(got) != 0 {
		t.Errorf("no changed test file: verify added %v (R15)", got)
	}
	f.plain("impl", "", map[string]string{"p/x_test.go": l4Test("p", "TestX"), "p/y_test.go": l4Test("p", "TestY")})
	reason := fmt.Sprintf(l4FaultBytes, "impl", "it carries no contract")
	l3SameJSON(t, "R15 then R14", l4AuthorshipProblems(f.verify()), []string{
		l4R15,
		"test file p/x_test.go is unowned: " + reason,
		"test file p/y_test.go is unowned: " + reason,
	})

	g := l4New(t, l4Opts{checks: unit})
	g.plain("impl", "", map[string]string{"p/r_test.go": l4Test("p", "TestR")})
	if run := g.run("unit"); run.Status != "passed" {
		t.Fatalf("fixture run %q", run.Status)
	}
	g.citer("ev1", "evidence", l4Cite{"f1", "p/r_test.go"})
	g.judge("ev1", "accepted", []string{"unit"}, "resolved")
	if got := l4AuthorshipProblems(g.verify()); len(got) != 0 {
		t.Errorf("only source_aware changes: verify added %v (R15, R14)", got)
	}

	h := l4New(t, l4Opts{checks: unit, small: true})
	h.plain("impl", "", map[string]string{"p/x_test.go": l4Test("p", "TestX")})
	if got := l4AuthorshipProblems(h.verify()); len(got) != 0 {
		t.Errorf("small plan: verify added %v (R15 applies to gated records)", got)
	}
}

// TestVerifyUnreadableBaselineManifest fails when a gated record whose
// baseline manifest cannot be read lacks R16's problem with the exact error,
// still adds R14 or R15 problems, or when Authorship lists files (R16, R19).
func TestVerifyUnreadableBaselineManifest(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{checks: []taskflow.Check{l3Check("unit", l3PlainP...)}})
	f.plain("impl", "", map[string]string{"p/x_test.go": l4Test("p", "TestX")})
	if got := l4AuthorshipProblems(f.verify()); len(got) != 2 {
		t.Errorf("before the manifest is damaged, verify reports %v, want R15's and one R14 problem (R15, R14)", got)
	}
	manifest := f.manifestFile()
	orig, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	check := func(label, want string, prefix bool) {
		t.Helper()
		got := l4AuthorshipProblems(f.verify())
		if len(got) != 1 || (!prefix && got[0] != want) || (prefix && !strings.HasPrefix(got[0], want)) {
			t.Errorf("%s: verify authorship problems %q, want only %q%s (R16)", label, got, want, map[bool]string{true: "...", false: ""}[prefix])
		}
		rep, err := l4AuthorshipErr(f.ctx, f.dir)
		if err != nil {
			t.Errorf("%s: Authorship: %v, want a report (R19)", label, err)
			return
		}
		if len(rep.Files) != 0 || len(rep.Problems) != 1 || (!prefix && rep.Problems[0] != want) || (prefix && !strings.HasPrefix(rep.Problems[0], want)) {
			t.Errorf("%s: Authorship files %v problems %q, want no files and %q (R19)", label, rep.Files, rep.Problems, want)
		}
		if rep.Files == nil || rep.Problems == nil {
			t.Errorf("%s: Authorship lists are null (R19)", label)
		}
	}
	if err := os.WriteFile(manifest, append(append([]byte{}, orig...), ' '), 0o600); err != nil {
		t.Fatal(err)
	}
	check("bytes changed", l4CannotJudge+"baseline manifest hash differs", false)
	if err := os.Remove(manifest); err != nil {
		t.Fatal(err)
	}
	check("file missing", l4CannotJudge+"read baseline manifest: ", true)
	if err := os.WriteFile(manifest, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := l4AuthorshipProblems(f.verify()); len(got) != 2 {
		t.Errorf("with the manifest restored, verify reports %v, want R15's and one R14 problem (R15, R14)", got)
	}
	// Entry count differs from the record's.
	f.rewrite(func(_ int, doc map[string]any) {
		m, _ := doc["baseline_manifest"].(map[string]any)
		if n, ok := m["files"]; ok {
			m["files"] = json.Number(fmt.Sprint(n) + "0")
		}
	})
	if _, err := taskflow.Load(f.dir); err != nil {
		t.Fatalf("fixture: a record whose manifest entry count differs does not load: %v", err)
	}
	check("entry count differs", l4CannotJudge+"baseline manifest is invalid", false)
}

// l4WorkFiles is a selected path with go.work and no go.mod.
func l4WorkFiles() map[string]string {
	return map[string]string{
		"go.work":  "go 1.22\n\nuse ./m\n",
		"m/go.mod": "module example.com/m\n\ngo 1.22\n",
		"m/m.go":   "package m\n\n// F is the contract symbol.\nfunc F() int { return 1 }\n",
	}
}

// TestVerifyNoGoModAtSelectedPath fails when a gated record whose selected
// path has go.work or a .go file but no go.mod lacks R23's limitation right
// after the trace's limitations, adds a problem for it, or judges test files;
// or when a path with neither, or a small or legacy record, gets it from
// verify; or when Authorship does not report it in any record (R23, R19).
func TestVerifyNoGoModAtSelectedPath(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{bare: true, contract: "m", files: l4WorkFiles(), checks: []taskflow.Check{l3Check("ok", l3JSONAll...)}})
	f.te("te1", "", "completed", map[string]string{"m/te_test.go": l4Test("m", "TestTE")})
	f.plain("impl", "", map[string]string{"m/x_test.go": l4Test("m", "TestX")})
	v := f.verify()
	if got := l4AuthorshipProblems(v); len(got) != 0 {
		t.Errorf("go.work root: verify added problems %v; no file is a test file (R23)", got)
	}
	for _, p := range v.Problems {
		if strings.Contains(p, "go.mod") {
			t.Errorf("verify added a problem for the missing go.mod: %q (R23: a limitation, no problem)", p)
		}
	}
	uncovered := "worker assignment te1 requirement R1 uncovered: " + l4UncoveredReason
	i, j := -1, -1
	for k, l := range v.Limitations {
		if l == l4R23 {
			i = k
		}
		if l == uncovered {
			j = k
		}
	}
	switch {
	case i < 0:
		t.Errorf("verify limitations %q lack R23's", v.Limitations)
	case j < 0:
		t.Errorf("fixture: verify limitations %q lack the trace's uncovered entry", v.Limitations)
	case i != j+1:
		t.Errorf("R23's limitation is at %d, want right after the trace's limitation at %d: %q (R23 ordering)", i, j, v.Limitations)
	}
	g := l4New(t, l4Opts{bare: true, contract: "m", checks: []taskflow.Check{l3Check("unit", "go", "version")},
		files: map[string]string{"main.go": "package main\n\nfunc main() {}\n", "m/m.go": "package m\n\nfunc F() int { return 1 }\n"}})
	if !l3Has(g.verify().Limitations, l4R23) {
		t.Errorf("a .go file and no go.mod: verify lacks R23's limitation (R23)")
	}
	var others []*l3Fix
	for _, o := range []l4Opts{{small: true}, {legacy: true}} {
		o.bare, o.contract, o.files, o.checks = true, "m", l4WorkFiles(), []taskflow.Check{l3Check("ok", l3JSONAll...)}
		x := l4New(t, o)
		if l3Has(x.verify().Limitations, l4R23) {
			t.Errorf("small %v legacy %v: verify added R23's limitation (R23 applies to gated records)", o.small, o.legacy)
		}
		others = append(others, x)
	}
	// Authorship reports R23 in any record whose baseline manifest can be
	// read, and lists no file (no go.mod: no test file).
	f.l4Report("go.work root", f.authorship(), l4Rules, true, []string{l4R23})
	g.l4Report(".go file at the root", g.authorship(), l4Rules, true, []string{l4R23})
	others[0].l4Report("small plan at a go.work root", others[0].authorship(), l4Rules, false, []string{l4R23})
	others[1].l4Report("legacy record at a go.work root", others[1].authorship(), "", false, []string{l4R23})
}

// TestVerifyNoR23WithoutGoFiles fails when a gated record whose selected path
// has neither go.mod, go.work nor a .go file gets R23's limitation from
// verify or R23's problem from Authorship (R23, R19).
func TestVerifyNoR23WithoutGoFiles(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{bare: true, noContract: true, checks: []taskflow.Check{l3Check("unit", "go", "version")},
		files: map[string]string{"README.md": "docs\n", "docs/a.md": "more docs\n", "m/f.go.txt": "package m\n"}})
	f.write("m/x_test.go.txt", "not Go\n")
	if v := f.verify(); l3Has(v.Limitations, l4R23) || len(l4AuthorshipProblems(v)) != 0 {
		t.Errorf("no Go files: verify limitations %q problems %q; want no R23 limitation (R23)", v.Limitations, l4AuthorshipProblems(v))
	}
	f.l4Report("no Go files", f.authorship(), l4Rules, true, nil)
}

// TestAuthorshipReportMembers fails when an empty report's lists are null,
// the module prefix is not kept in paths, a file outside the selected path is
// listed, Authorship writes the task or the project, fails on an abandoned
// task, or returns a report for a task it cannot load (R11, R19, I7).
func TestAuthorshipReportMembers(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{sub: true})
	f.l4Report("no changes", f.authorship(), l4Rules, true, nil)
	data := l3JSON(f.authorship())
	if !strings.Contains(data, `"files":[]`) || !strings.Contains(data, `"problems":[]`) {
		t.Errorf("empty report JSON %s, want files and problems as [] (R19)", data)
	}
	f.te("te1", "", "completed", map[string]string{f.p("p/a_test.go"): l4Test("p", "TestA")})
	f.write("tools/t_test.go", l4Test("tools", "TestTools")) // outside the selected path
	f.write(f.p("p/b_test.go"), l4Test("p", "TestB"))
	// I7: only Authorship runs between the two snapshots. l4Report calls
	// Verify, which records its result as a revision, so it runs after them.
	before, beforeRepo := l3Tree(t, f.dir), l3Tree(t, f.repo)
	rep := f.authorship()
	afterTask, afterRepo := l3Tree(t, f.dir), l3Tree(t, f.repo)
	l3SameTree(t, "Authorship (task)", before, afterTask)
	l3SameTree(t, "Authorship (project)", beforeRepo, afterRepo)
	f.l4Report("module below the root", rep, l4Rules, true, nil,
		f.l4File("mod/p/a_test.go", false, "unowned", fmt.Sprintf(l4FaultBytes, "te1", "it has no acceptance"), "te1", ""),
		f.l4File("mod/p/b_test.go", false, "unowned", l4ReasonNoBytes, "", ""),
	)
	if len(rep.Files) != 2 {
		t.Fatalf("report lists %d files, want 2 (R11)", len(rep.Files))
	}
	raw := l3JSON(rep.Files[1])
	if !strings.Contains(raw, `"deleted":false`) || strings.Contains(raw, "assignment_id") || strings.Contains(raw, "agent_id") ||
		strings.Contains(raw, "contract") || strings.Contains(raw, "finding_id") {
		t.Errorf("file entry JSON %s: want deleted present and the assignment members omitted (R19)", raw)
	}
	if _, err := taskflow.Abandon(f.dir, "fixture abandoned"); err != nil {
		t.Fatalf("Abandon: %v", err)
	}
	abandoned, abandonedRepo := l3Tree(t, f.dir), l3Tree(t, f.repo)
	after := f.authorship()
	l3SameTree(t, "Authorship of an abandoned task (task)", abandoned, l3Tree(t, f.dir))
	l3SameTree(t, "Authorship of an abandoned task (project)", abandonedRepo, l3Tree(t, f.repo))
	l3SameJSON(t, "abandoned task files", after.Files, rep.Files)
	if r, err := l4AuthorshipErr(f.ctx, filepath.Join(f.root, "no-such-task")); err == nil {
		t.Errorf("Authorship of a missing task returned no error (R19)")
	} else if r.TaskID != "" || r.Files != nil || r.Problems != nil {
		t.Errorf("Authorship of a missing task returned a report %+v (R19)", r)
	}
}

// TestAuthorshipSourceCannotBeCaptured fails when Authorship returns a
// report, or no error, for a task whose project directory is gone (R19).
func TestAuthorshipSourceCannotBeCaptured(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	if err := os.Rename(f.repo, f.repo+" moved"); err != nil {
		t.Fatal(err)
	}
	rep, err := l4AuthorshipErr(f.ctx, f.dir)
	if err == nil || rep.TaskID != "" || rep.Files != nil || rep.Problems != nil {
		t.Errorf("Authorship without the project: %+v, %v; want an error and no report (R19)", rep, err)
	}
}

// TestAuthorshipSmallAndLegacyRecords fails when the report of a record with
// test rules and a small plan, or of a legacy record with a baseline
// manifest, differs from R19: gated false, the record's test_rules, and the
// R11 and R12 rules applied though verify does not apply them (R19).
func TestAuthorshipSmallAndLegacyRecords(t *testing.T) {
	t.Parallel()
	small := l4New(t, l4Opts{small: true})
	small.te("te1", "", "completed", map[string]string{"p/a_test.go": l4Test("p", "TestA")})
	small.l4Report("small plan", small.authorship(), l4Rules, false, nil,
		small.l4File("p/a_test.go", false, "unowned", fmt.Sprintf(l4FaultBytes, "te1", "it has no acceptance"), "te1", ""))
	if got := l4AuthorshipProblems(small.verify()); len(got) != 0 {
		t.Errorf("small plan: verify added %v; the report applies R11 and R12 that verify does not (R19)", got)
	}

	legacy := l4New(t, l4Opts{legacy: true})
	legacy.te("te1", "", "completed", map[string]string{"p/a_test.go": l4Test("p", "TestA")})
	legacy.accept("te1", "accepted")
	legacy.l4Report("legacy record", legacy.authorship(), "", false, nil,
		legacy.l4File("p/a_test.go", false, "independent", fmt.Sprintf(l4ReasonBytes, "te1"), "te1", ""))

}

// TestAuthorshipLegacyWithoutManifest fails when a legacy record without a
// baseline manifest does not report R19's "task predates the baseline
// manifest" problem with no files (R19).
func TestAuthorshipLegacyWithoutManifest(t *testing.T) {
	t.Parallel()
	old := l4New(t, l4Opts{legacy: true})
	old.write("p/a_test.go", l4Test("p", "TestA"))
	old.rewrite(func(_ int, doc map[string]any) { delete(doc, "baseline_manifest") })
	if _, err := taskflow.Load(old.dir); err != nil {
		t.Fatalf("fixture: a legacy record without a baseline manifest does not load: %v", err)
	}
	old.l4Report("legacy record without a baseline manifest", old.authorship(), "", false, []string{l4NoManifest})
}

// l4BreakChain sets the newest revision's previous_sha256 to a value that is
// not the SHA-256 of the previous revision file, so the chain does not hold.
func (f *l3Fix) l4BreakChain() {
	f.t.Helper()
	names, err := filepath.Glob(filepath.Join(f.dir, "record-*.json"))
	if err != nil || len(names) < 2 {
		f.t.Fatalf("fixture: need two revisions to break the chain, have %d (%v)", len(names), err)
	}
	sort.Strings(names)
	last := names[len(names)-1]
	data, err := os.ReadFile(last)
	if err != nil {
		f.t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		f.t.Fatal(err)
	}
	doc["previous_sha256"] = strings.Repeat("0", 64)
	out, err := json.Marshal(doc)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(last, out, 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// TestAuthorshipRecordDoesNotLoad fails when Authorship returns a report, no
// error or panics for an existing task directory whose record does not load
// (an invalid stored value with the chain kept, or a broken chain), or writes
// to that directory (R19).
func TestAuthorshipRecordDoesNotLoad(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		corrupt func(f *l3Fix)
	}{
		{"invalid stored test rules", func(f *l3Fix) {
			f.rewrite(func(_ int, doc map[string]any) { doc["test_rules"] = "bogus" })
		}},
		{"broken revision chain", func(f *l3Fix) { f.l4BreakChain() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := l4New(t, l4Opts{})
			f.te("te1", "", "completed", map[string]string{"p/a_test.go": l4Test("p", "TestA")})
			c.corrupt(f)
			if _, err := taskflow.Load(f.dir); err == nil {
				t.Fatalf("fixture: the record still loads after %s", c.name)
			}
			before := l3Tree(t, f.dir)
			var rep taskflow.AuthorshipReport
			var err error
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Authorship panicked on a record that does not load: %v (R19: it returns an error)", r)
						err = fmt.Errorf("panic")
					}
				}()
				rep, err = taskflow.Authorship(f.ctx, f.dir)
			}()
			if err == nil {
				t.Errorf("Authorship of a record that does not load returned no error (R19)")
			}
			if !reflect.DeepEqual(rep, taskflow.AuthorshipReport{}) {
				t.Errorf("Authorship of a record that does not load returned a report %s, want none (R19)", l3JSON(rep))
			}
			l3SameTree(t, "Authorship of a record that does not load", before, l3Tree(t, f.dir))
		})
	}
}

// l4Normalize replaces the fixture's temporary root (as given and with
// symlinks resolved) and its task ID, so refusals of two fixtures compare.
func (f *l3Fix) l4Normalize(s string) string {
	f.t.Helper()
	if real, err := filepath.EvalSymlinks(f.root); err == nil {
		s = strings.ReplaceAll(s, real, "<root>")
	}
	s = strings.ReplaceAll(s, f.root, "<root>")
	return strings.ReplaceAll(s, f.load().ID, "<task>")
}

// TestHandoffGofmtAfterExistingFileCheck fails when a gofmt-eligible entry
// with an explicit sha256 and no gofmt_sha256, whose file is absent without
// being marked deleted or is a symlink, is not refused by the existing
// changed-file check: the refusal must be the one a legacy record (where
// R13.b does not apply) gives for the same handoff, name no gofmt rule, and
// record no revision (R13.b ordering, R3f).
func TestHandoffGofmtAfterExistingFileCheck(t *testing.T) {
	t.Parallel()
	const path = "p/x_test.go"
	body := l4Test("p", "TestX")
	refuse := func(t *testing.T, legacy bool, kind string) string {
		f := l4New(t, l4Opts{legacy: legacy})
		f.mustAssign(f.brief("te1", []string{path}, l3Req("R1", "C1")))
		f.write(path, body)
		// An explicit sha256 of the bytes written, no gofmt_sha256, and
		// evidence that stays current.
		res := f.fastResult("te1", "completed", []string{path}, l3Uncovered("R1", l4UncoveredReason))
		res.Evidence = []taskflow.SourceRef{f.goModRef()}
		if len(res.ChangedFiles) != 1 || res.ChangedFiles[0].SHA256 != l3Sum([]byte(body)) || res.ChangedFiles[0].GofmtSHA256 != "" {
			t.Fatalf("fixture: changed files %+v", res.ChangedFiles)
		}
		f.remove(path)
		if kind == "symlink" {
			f.write("p/target.txt", body)
			if err := os.Symlink("target.txt", f.abs(path)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
		}
		before := f.revision()
		err := f.submit(res)
		if err == nil {
			t.Fatalf("legacy %v, %s: the handoff succeeded, want the existing changed-file refusal", legacy, kind)
		}
		if after := f.revision(); after != before {
			t.Errorf("legacy %v, %s: the refused handoff moved the record from revision %d to %d (R13)", legacy, kind, before, after)
		}
		for _, r := range f.load().WorkerResults {
			if r.Result.AssignmentID == "te1" {
				t.Errorf("legacy %v, %s: the refused handoff stored a result", legacy, kind)
			}
		}
		t.Logf("legacy %v, %s: refusal %q", legacy, kind, err)
		return f.l4Normalize(err.Error())
	}
	for _, kind := range []string{"absent", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			gated := refuse(t, false, kind)
			legacy := refuse(t, true, kind)
			if strings.Contains(strings.ToLower(gated), "gofmt") {
				t.Errorf("%s: refusal %q names a gofmt rule; the existing changed-file check comes first (R13.b)", kind, gated)
			}
			if gated != legacy {
				t.Errorf("%s: refusal with test rules %q differs from the existing refusal %q (R13.b runs after the existing checks)", kind, gated, legacy)
			}
		})
	}
}

// TestAuthorshipCitationOrder fails when a new test file with two valid
// citations is not source_aware naming the first in assignment order (record
// order, not ID order), then in finding order within the acceptance; or when
// a file with only invalid citations is not unowned naming the first such
// citation (R12.4, R12.5, R19).
func TestAuthorshipCitationOrder(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{})
	f.plain("impl", "", map[string]string{
		"p/valid_test.go":   l4Test("p", "TestValid"),
		"p/invalid_test.go": l4Test("p", "TestInvalid"),
	})
	if ok := f.run("ok"); ok.Status != "passed" {
		t.Fatalf("fixture run %q", ok.Status)
	}
	checks := []string{"ok"}
	// Valid: both acceptances bind a check run. zeta is assigned before
	// alpha; within zeta, z2 comes before z1.
	f.citer("zeta", "evidence", l4Cite{"z2", "p/valid_test.go"}, l4Cite{"z1", "p/valid_test.go"})
	f.citer("alpha", "verification", l4Cite{"a1", "p/valid_test.go"})
	// Invalid: neither acceptance binds a check run.
	f.citer("yank", "evidence", l4Cite{"y2", "p/invalid_test.go"}, l4Cite{"y1", "p/invalid_test.go"})
	f.citer("bravo", "evidence", l4Cite{"b1", "p/invalid_test.go"})
	f.judge("alpha", "accepted", checks, "resolved")
	f.judge("zeta", "accepted", checks, "resolved")
	f.judge("bravo", "accepted", nil, "resolved")
	f.judge("yank", "accepted", nil, "resolved")

	rep := f.authorship()
	f.l4Report("citation order", rep, l4Rules, true, nil,
		f.l4File("p/invalid_test.go", false, "unowned", fmt.Sprintf(l4CitationNoHold, "y2", "yank"), "yank", "y2"),
		f.l4File("p/valid_test.go", false, "source_aware", fmt.Sprintf(l4ReasonRepro, "z2", "zeta"), "zeta", "z2"),
	)
	l3SameJSON(t, "verify R14 problems (I4)", l4AuthorshipProblems(f.verify()), []string{
		"test file p/invalid_test.go is unowned: " + fmt.Sprintf(l4CitationNoHold, "y2", "yank"),
	})
}

// TestVerifyGoWorkWithoutGoFiles fails when a gated record whose selected
// path has go.work, no go.mod and no entry ending in .go lacks R23's
// limitation from verify or R23's problem from Authorship, or adds a
// problem for it (R23, R19).
func TestVerifyGoWorkWithoutGoFiles(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{bare: true, noContract: true, checks: []taskflow.Check{l3Check("unit", "go", "version")},
		files: map[string]string{
			"go.work":     "go 1.22\n\nuse ./m\n",
			"m/go.mod":    "module example.com/m\n\ngo 1.22\n",
			"m/README.md": "member module without Go files\n",
		}})
	v := f.verify()
	if !l3Has(v.Limitations, l4R23) {
		t.Errorf("go.work and no .go file: verify limitations %q lack R23's (R23)", v.Limitations)
	}
	if got := l4AuthorshipProblems(v); len(got) != 0 {
		t.Errorf("go.work root: verify added authorship problems %v (R23: a limitation, no problem)", got)
	}
	for _, p := range v.Problems {
		if strings.Contains(p, "go.mod") {
			t.Errorf("verify added a problem for the missing go.mod: %q (R23: a limitation, no problem)", p)
		}
	}
	f.l4Report("go.work without .go files", f.authorship(), l4Rules, true, []string{l4R23})
}
