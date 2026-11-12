package taskflow_test

// Brief requirements and the requirement map: R7-R14, R44 and R38
// (requirements, requirement_map).

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

const (
	l3ReqShape   = "worker brief requirements need 1..256 distinct requirement IDs, each with 1..256 distinct criteria"
	l3ReqNoCon   = "only a brief with a contract declares requirements"
	l3MustDeclar = "a brief with a contract must declare requirements: each contract requirement with the plan requirements it implements"
	l3NoCredit   = "brief declares requirements, but the plan has no go test check whose argv earns per-test credit (it needs -json in its own argv and only flags the Go evidence rule allows: no -args, -run, -skip or other refused flag)"
	l3Carrier    = "only a completed result of an assignment whose brief declares requirements carries requirement_map"
	l3PathRule   = "path must be a _test.go file inside the brief's owned_paths"
	l3NameRule   = "name must be a Go test name (a Test, Fuzz or Example function, then optional /subtest parts)"
)

func l3Reqs(n int, criteria ...string) []taskflow.ContractRequirement {
	var out []taskflow.ContractRequirement
	for i := 1; i <= n; i++ {
		out = append(out, l3Req(fmt.Sprintf("R%03d", i), criteria...))
	}
	return out
}

func l3IDs(prefix string, n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("%s%03d", prefix, i))
	}
	return out
}

// TestAssignRequirementsShape fails when a malformed requirements list is
// accepted, when one at the bounds is refused, or when requirements are
// accepted on a brief without a contract (R7).
func TestAssignRequirementsShape(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2"}, checks: []taskflow.Check{l3Check("ok", l3JSONAll...)}})
	n := 0
	refuse := func(label string, reqs []taskflow.ContractRequirement, contract bool, want string) {
		t.Helper()
		n++
		id := fmt.Sprintf("bad%d", n)
		var b taskflow.WorkerBrief
		if contract {
			b = f.brief(id, []string{"p/" + id + "_test.go"}, reqs...)
		} else {
			b = f.plainBrief(id, []string{"p/" + id + "_test.go"})
			b.Requirements = reqs
		}
		b.Goal = label
		_ = f.assignFails(b, want)
	}
	refuse("no contract", []taskflow.ContractRequirement{l3Req("R1", "C1")}, false, l3ReqNoCon)
	refuse("257 requirements", l3Reqs(257, "C1"), true, l3ReqShape)
	for _, bad := range []string{"", "1R", "R 1", "R1!", "R/1", "Ré", "R" + strings.Repeat("a", 64)} {
		refuse("id "+bad, []taskflow.ContractRequirement{l3Req(bad, "C1")}, true, l3ReqShape)
	}
	refuse("repeated requirement", []taskflow.ContractRequirement{l3Req("R1", "C1"), l3Req("R1", "C2")}, true, l3ReqShape)
	refuse("no criteria", []taskflow.ContractRequirement{l3Req("R1")}, true, l3ReqShape)
	refuse("repeated criterion", []taskflow.ContractRequirement{l3Req("R1", "C1", "C1")}, true, l3ReqShape)
	refuse("unknown criterion", []taskflow.ContractRequirement{l3Req("R1", "C1"), l3Req("R2", "C1", "C9")}, true,
		"brief requirement R2 names unknown plan requirement C9")

	accept := func(id string, reqs []taskflow.ContractRequirement) {
		t.Helper()
		if err := f.assign(f.brief(id, []string{"p/" + id + "_test.go"}, reqs...)); err != nil {
			t.Errorf("%s: AssignWorker refused a valid requirements list: %v (R7)", id, err)
		}
	}
	accept("max", l3Reqs(256, "C1"))
	accept("ids", []taskflow.ContractRequirement{
		l3Req("R"+strings.Repeat("a", 63), "C1"),
		l3Req("R1.a-b_c", "C2", "C1"),
		l3Req("z", "C2"),
	})

	// 256 distinct criteria need a plan with 256 requirements (a plan holds
	// at most 256, so 257 distinct known criteria cannot be built).
	g := l3New(t, l3Opts{reqs: l3IDs("Q", 256), checks: []taskflow.Check{l3Check("ok", l3JSONAll...)}})
	if err := g.assign(g.brief("crit256", []string{"p/b_test.go"}, l3Req("R1", l3IDs("Q", 256)...))); err != nil {
		t.Errorf("256 criteria refused: %v (R7 bound)", err)
	}
}

// TestAssignRequirementsFollowPlan fails when the presence of requirements
// does not follow whether the plan can prove tests by the full credit rule, or
// when the rule is checked before the contract itself (R8, R7 empty list).
func TestAssignRequirementsFollowPlan(t *testing.T) {
	t.Parallel()
	proving := map[string][][]string{
		"-json first":       {l3JSONAll},
		"-json last":        {{"go", "test", "./...", "-json"}},
		"--json":            {{"go", "test", "--json", "./..."}},
		"allowed flags":     {{"go", "test", "-race", "-count=1", "-json", "./p/..."}},
		"second check only": {l3PlainP, l3JSONP},
	}
	notProving := map[string][][]string{
		"no -json":       {{"go", "test", "./..."}},
		"-v":             {{"go", "test", "-v", "./..."}},
		"-json with run": {{"go", "test", "-json", "-run", "TestPass", "./..."}},
		"-json=true":     {{"go", "test", "-json=true", "./..."}},
		"-args":          {{"go", "test", "-json", "./...", "-args", "-x"}},
		"go vet":         {{"go", "vet", "-json", "./..."}},
		"tags value":     {{"go", "test", "-tags", "-json", "./..."}},
	}
	plan := func(argvs [][]string) []taskflow.Check {
		var out []taskflow.Check
		for i, a := range argvs {
			out = append(out, l3Check(fmt.Sprintf("c%d", i), a...))
		}
		return out
	}
	for label, argvs := range proving {
		f := l3New(t, l3Opts{checks: plan(argvs)})
		b := f.brief("none", []string{"p/none_test.go"})
		b.Goal = label
		_ = f.assignFails(b, l3MustDeclar)
		b = f.brief("empty", []string{"p/empty_test.go"})
		b.Requirements = []taskflow.ContractRequirement{}
		b.Goal = label + ", empty list"
		_ = f.assignFails(b, l3MustDeclar)
		if err := f.assign(f.brief("with", []string{"p/with_test.go"}, l3Req("R1", "C1"))); err != nil {
			t.Errorf("%s: brief with requirements refused: %v (R8)", label, err)
		}
	}
	for label, argvs := range notProving {
		// Only a small plan takes a contract brief its checks cannot prove; a
		// substantial one refuses it first (L4 R17).
		f := l4New(t, l4Opts{small: true, checks: plan(argvs)})
		b := f.brief("with", []string{"p/with_test.go"}, l3Req("R1", "C1"))
		b.Goal = label
		_ = f.assignFails(b, l3NoCredit)
		if err := f.assign(f.brief("none", []string{"p/none_test.go"})); err != nil {
			t.Errorf("%s: contract brief without requirements refused: %v (R8: accepted as before)", label, err)
		}
	}

	// The contract is validated first: a bad contract reports its own error.
	f := l3New(t, l3Opts{checks: plan([][]string{l3JSONAll})})
	b := f.brief("badcontract", []string{"p/a_test.go"})
	b.Contract.SHA256 = l3Sum([]byte("not the extract"))
	if err := f.assign(b); err == nil || strings.Contains(err.Error(), l3MustDeclar) {
		t.Errorf("invalid contract without requirements: error %v, want the contract's own error, not R8's", err)
	}
	g := l3New(t, l3Opts{checks: plan([][]string{l3PlainP})})
	b = g.brief("badcontract", []string{"p/bad_test.go"}, l3Req("R1", "C1"))
	b.Contract.SHA256 = l3Sum([]byte("not the extract"))
	if err := g.assign(b); err == nil || strings.Contains(err.Error(), "brief declares requirements, but the plan") {
		t.Errorf("invalid contract with requirements: error %v, want the contract's own error, not R8's", err)
	}
}

// TestAssignStoresRequirementsAsGiven fails when assign reorders or rewrites
// the requirements, or stores an absent or empty list as a member (R9, R7,
// R38).
func TestAssignStoresRequirementsAsGiven(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{reqs: []string{"C1", "C2", "C3"}, checks: []taskflow.Check{l3Check("ok", l3JSONAll...)}})
	given := []taskflow.ContractRequirement{l3Req("R9", "C3", "C1"), l3Req("R2", "C2"), l3Req("A1", "C1", "C2", "C3")}
	f.mustAssign(f.brief("te1", []string{"p/a_test.go"}, given...))
	if got := f.assignment("te1").Brief.Requirements; !reflect.DeepEqual(got, given) {
		t.Errorf("stored requirements %s, want %s exactly as given (R9)", l3JSON(got), l3JSON(given))
	}
	stored := f.storedAssignment("te1")["brief"].(map[string]any)
	var got []taskflow.ContractRequirement
	if err := json.Unmarshal([]byte(l3JSON(stored["requirements"])), &got); err != nil || !reflect.DeepEqual(got, given) {
		t.Errorf("stored record holds requirements %s (%v), want %s (R9)", l3JSON(stored["requirements"]), err, l3JSON(given))
	}

	// A small plan: a substantial one refuses these briefs (L4 R17).
	g := l4New(t, l4Opts{small: true, checks: []taskflow.Check{l3Check("plain", l3PlainP...)}})
	b := g.brief("empty", []string{"p/a_test.go"})
	b.Requirements = []taskflow.ContractRequirement{}
	g.mustAssign(b)
	g.mustAssign(g.brief("absent", []string{"p/b_test.go"}))
	for _, id := range []string{"empty", "absent"} {
		brief := g.storedAssignment(id)["brief"]
		if l3Has(l3Members(brief), "requirements") || l3Has(l3Members(brief), "inputs") {
			t.Errorf("brief %s stored members %v, want no requirements or inputs member (R7, R38)", id, l3Members(brief))
		}
	}
}

// TestHandoffRequirementMapCarrier fails when a requirement_map is accepted on
// a result that is not a completed result of a map-bearing assignment, or
// when an empty map is not treated as absent (R10, R38).
func TestHandoffRequirementMapCarrier(t *testing.T) {
	t.Parallel()
	// A small plan: a substantial one refuses the contract brief without
	// requirements (L4 R17).
	f := l4New(t, l4Opts{small: true, files: map[string]string{"p/te_test.go": l3PTests}, checks: []taskflow.Check{l3Check("plain", l3PlainP...)}})
	m := l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))
	f.mustAssign(f.brief("contract", []string{"p/a_test.go"}))
	f.write("p/a_test.go", "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {}\n")
	f.submitFails(f.result("contract", "completed", []string{"p/a_test.go"}, m), l3Carrier)
	f.mustAssign(f.plainBrief("plain", []string{"p/b_test.go"}))
	f.write("p/b_test.go", "package p\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) {}\n")
	f.submitFails(f.result("plain", "completed", []string{"p/b_test.go"}, m), l3Carrier)

	res := f.result("contract", "completed", []string{"p/a_test.go"})
	res.RequirementMap = []taskflow.RequirementMapping{}
	f.mustSubmit(res)
	if l3Has(l3Members(f.storedResult("contract")), "requirement_map") {
		t.Errorf("result with an empty map stored a requirement_map member (R10, R38)")
	}

	g := l3New(t, l3Opts{files: map[string]string{"p/te_test.go": l3PTests}})
	g.mustAssign(g.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	g.write("p/te_test.go", l3PTests+"\nfunc TestMore(t *testing.T) {}\n")
	g.submitFails(g.result("te1", "blocked", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestMore"))), l3Carrier)
}

// TestHandoffRequirementMapEntries fails when a malformed requirement_map of a
// completed map-bearing result is accepted, or when its errors are reported
// in another order than R10-R13, then R44, then R14 (R11, R12, R13).
func TestHandoffRequirementMapEntries(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	owned := []string{"p/te_test.go", "p/helper.go", "p/other_test.go", "p/sub"}
	f.mustAssign(f.brief("te1", owned, l3Req("R1", "C1"), l3Req("R2", "C1")))
	f.write("p/te_test.go", l3PTests)
	base := f.result("te1", "completed", []string{"p/te_test.go"})
	pass := l3Mapped("p/te_test.go", "TestPass")
	r2 := l3Map("R2", l3Mapped("p/te_test.go", "TestSkip"))
	try := func(want string, rm ...taskflow.RequirementMapping) {
		t.Helper()
		res := base
		res.RequirementMap = rm
		f.submitFails(res, want)
	}

	// R11: entries in order.
	try("requirement_map names R3, which the brief does not declare", l3Map("R1", pass), l3Map("R3", pass), r2)
	try("requirement_map repeats R1", l3Map("R1", pass), l3Map("R1", pass), r2)
	shape := "requirement_map entry R1 needs 1..256 tests or an uncovered reason, not both"
	both := taskflow.RequirementMapping{Requirement: "R1", Tests: []taskflow.MappedTest{pass}, Uncovered: "both"}
	// Two faulty entries: the first one in map order is reported, whichever
	// rule it breaks.
	try(shape, both, l3Map("R3", pass), r2)
	try("requirement_map names R3, which the brief does not declare", l3Map("R3", pass), both, r2)
	try(shape, r2, both, r2)
	try("requirement_map repeats R2", r2, l3Map("R1", pass), r2, l3Map("R4", pass))
	try(shape, both, r2)
	try(shape, taskflow.RequirementMapping{Requirement: "R1"}, r2)
	try(shape, l3Uncovered("R1", " \t\n "), r2)
	try(shape, l3Uncovered("R1", strings.Repeat("u", 16385)), r2)
	var many []taskflow.MappedTest
	for i := 0; i < 257; i++ {
		many = append(many, l3Mapped("p/te_test.go", fmt.Sprintf("TestSub/s%03d", i)))
	}
	try(shape, l3Map("R1", many...), r2)

	// R12: path and name of each test.
	for _, bad := range []string{
		f.abs("p/te_test.go"), "", "../p/te_test.go", "p/../p/te_test.go", "./p/te_test.go", "p//te_test.go",
		"p/te_test.go/", `p\te_test.go`, "p/te:x_test.go", "p/te\x01_test.go", "p/.git/x_test.go",
		"p/helper.go", "q/te_test.go", "p/te_test.goo",
	} {
		try("requirement_map entry R1 test "+bad+" TestPass: "+l3PathRule, l3Map("R1", l3Mapped(bad, "TestPass")), r2)
	}
	for _, bad := range []string{"testPass", "TestPass/", "Testpass", "BenchmarkPass", ""} {
		try("requirement_map entry R1 test p/te_test.go "+bad+": "+l3NameRule, l3Map("R1", l3Mapped("p/te_test.go", bad)), r2)
	}
	try("requirement_map entry R1 repeats test p/te_test.go TestPass", l3Map("R1", pass, l3Mapped("p/te_test.go", "TestSub"), pass), r2)

	// R13: every declared requirement, the first missing one in brief order.
	try("completed result's requirement_map has no entry for R2", l3Map("R1", pass))
	try("completed result's requirement_map has no entry for R1", r2)
	try("completed result's requirement_map has no entry for R1")

	// Order: R13 before R44, R44 before R14.
	absent := l3Mapped("p/other_test.go", "TestOther")
	try("completed result's requirement_map has no entry for R2", l3Map("R1", absent))
	try("requirement_map entry R1 test p/other_test.go TestOther: path is not a file this result or an earlier test-engineer result changed",
		l3Map("R1", absent), r2)

	// A valid map is recorded as given.
	want := []taskflow.RequirementMapping{l3Map("R1", l3Mapped("p/te_test.go", "TestSub/a"), pass), r2}
	res := base
	res.RequirementMap = want
	f.mustSubmit(res)
	for _, r := range f.load().WorkerResults {
		if r.Result.AssignmentID == "te1" && !reflect.DeepEqual(r.Result.RequirementMap, want) {
			t.Errorf("recorded map %s, want %s (R14: recorded as given)", l3JSON(r.Result.RequirementMap), l3JSON(want))
		}
	}
}

// TestHandoffRequirementMapBounds fails when a map at its bounds (256 tests,
// a 16384-byte uncovered reason) is refused (R11).
func TestHandoffRequirementMapBounds(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	var many []taskflow.MappedTest
	for i := 0; i < 256; i++ {
		many = append(many, l3Mapped("p/te_test.go", fmt.Sprintf("TestSub/s%03d", i)))
	}
	if err := f.submit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", many...))); err != nil {
		t.Errorf("256 tests refused: %v (R11 bound)", err)
	}
	f.mustAssign(f.brief("te2", []string{"p/te_test.go"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests+"\nfunc TestMore(t *testing.T) {}\n")
	if err := f.submit(f.result("te2", "completed", []string{"p/te_test.go"}, l3Uncovered("R1", strings.Repeat("u", 16384)))); err != nil {
		t.Errorf("16384-byte uncovered reason refused: %v (R11 bound)", err)
	}
}

// TestHandoffMapsTestsTestEngineersWrote fails when a mapped test's file must
// be changed by this result only, when a rejected, non-contract or deleted
// change counts, or when an unused earlier result does not count (R44).
func TestHandoffMapsTestsTestEngineersWrote(t *testing.T) {
	t.Parallel()
	r44 := func(path, name string) string {
		return "requirement_map entry R1 test " + path + " " + name + ": path is not a file this result or an earlier test-engineer result changed"
	}
	extra := func(name string) string {
		return "package p\n\nimport \"testing\"\n\nfunc " + name + "(t *testing.T) {}\n"
	}
	// first writes p/te_test.go as te1, judges it, then lets te2 map that file
	// while changing only p/extra_test.go.
	first := func(judgment string) *l3Fix {
		f := l3New(t, l3Opts{})
		f.mustAssign(f.brief("te1", []string{"p/te_test.go", "p/extra_test.go"}, l3Req("R1", "C1")))
		f.write("p/te_test.go", l3PTests)
		f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))))
		if judgment != "" {
			f.accept("te1", judgment)
		}
		f.mustAssign(f.brief("te2", []string{"p/te_test.go", "p/extra_test.go"}, l3Req("R1", "C1")))
		f.write("p/extra_test.go", extra("TestExtra"))
		return f
	}
	for _, judgment := range []string{"", "accepted", "unused"} {
		f := first(judgment)
		res := f.result("te2", "completed", []string{"p/extra_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass")))
		if err := f.submit(res); err != nil {
			t.Errorf("earlier result judged %q: mapping its file refused: %v (R44)", judgment, err)
		}
	}
	f := first("rejected")
	f.submitFails(f.result("te2", "completed", []string{"p/extra_test.go"}, l3Map("R1", l3Mapped("p/te_test.go", "TestPass"))),
		r44("p/te_test.go", "TestPass"))

	// An earlier result of an assignment without a contract does not count.
	g := l3New(t, l3Opts{})
	g.mustAssign(g.plainBrief("impl", []string{"p/impl_test.go"}))
	g.write("p/impl_test.go", extra("TestImpl"))
	g.mustSubmit(g.result("impl", "completed", []string{"p/impl_test.go"}))
	g.mustAssign(g.brief("te1", []string{"p/impl_test.go", "p/te_test.go"}, l3Req("R1", "C1")))
	g.write("p/te_test.go", l3PTests)
	g.submitFails(g.result("te1", "completed", []string{"p/te_test.go"}, l3Map("R1", l3Mapped("p/impl_test.go", "TestImpl"))),
		r44("p/impl_test.go", "TestImpl"))

	// A deleted entry of this result does not count.
	h := l3New(t, l3Opts{files: map[string]string{"p/old_test.go": extra("TestOld")}})
	h.mustAssign(h.brief("te1", []string{"p/te_test.go", "p/old_test.go"}, l3Req("R1", "C1")))
	h.write("p/te_test.go", l3PTests)
	h.remove("p/old_test.go")
	h.submitFails(h.result("te1", "completed", []string{"p/te_test.go", "p/old_test.go"}, l3Map("R1", l3Mapped("p/old_test.go", "TestOld"))),
		r44("p/old_test.go", "TestOld"))
}

// TestHandoffMappedTestsExistAtCurrentSource fails when a mapped test that is
// missing at the current source is accepted at handoff, or when the error does
// not carry R29's first missing detail (R14).
func TestHandoffMappedTestsExistAtCurrentSource(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	f.mustAssign(f.brief("te0", []string{"p/gone_test.go"}, l3Req("R1", "C1")))
	f.write("p/gone_test.go", "package p\n\nimport \"testing\"\n\nfunc TestGone(t *testing.T) {}\n")
	f.mustSubmit(f.result("te0", "completed", []string{"p/gone_test.go"}, l3Map("R1", l3Mapped("p/gone_test.go", "TestGone"))))
	f.remove("p/gone_test.go")

	f.mustAssign(f.brief("te1", []string{"p/gone_test.go", "p/te_test.go", "p/testdata", "n"}, l3Req("R1", "C1")))
	f.write("p/te_test.go", l3PTests)
	f.write("p/testdata/td_test.go", "package p\n\nimport \"testing\"\n\nfunc TestTD(t *testing.T) {}\n")
	f.write("n/go.mod", "module example.com/nested\n\ngo 1.22\n")
	f.write("n/n_test.go", "package n\n\nimport \"testing\"\n\nfunc TestN(t *testing.T) {}\n")
	base := f.result("te1", "completed", []string{"p/te_test.go", "p/testdata/td_test.go", "n/go.mod", "n/n_test.go"})
	missing := func(path, name, detail string) {
		t.Helper()
		res := base
		res.RequirementMap = []taskflow.RequirementMapping{l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped(path, name))}
		f.submitFails(res, "requirement_map entry R1 test "+path+" "+name+" is missing at the current source: "+detail)
	}
	notInPackage := " is not in a package that go test ./... runs at the selected path"
	missing("p/te_test.go", "TestNope", "p/te_test.go declares no func TestNope")
	missing("p/te_test.go", "TestNope/sub", "p/te_test.go declares no func TestNope")
	missing("p/testdata/td_test.go", "TestTD", "p/testdata/td_test.go"+notInPackage)
	missing("n/n_test.go", "TestN", "n/n_test.go"+notInPackage)
	missing("p/gone_test.go", "TestGone", "p/gone_test.go is absent or not a regular file")

	res := base
	res.RequirementMap = []taskflow.RequirementMapping{l3Map("R1", l3Mapped("p/te_test.go", "TestPass"), l3Mapped("p/te_test.go", "TestSub/not_declared_as_subtest"))}
	if err := f.submit(res); err != nil {
		t.Errorf("tests whose top function is declared refused: %v (R14)", err)
	}
}
