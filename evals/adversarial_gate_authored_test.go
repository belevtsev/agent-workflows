package evals

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Authored tests for loadAdversarialCases, keptTestNames,
// adversarialReport.err and runAdversarial (adversarial-gate revision 1,
// AG1-AG4). Every expectation comes from that contract and the cases'
// interface contracts; the fixtures live in
// testdata/adversarial-selftest/<case>/.

// advAuthoredKept is the brief's owned kept-test path in every real case (AG1).
const advAuthoredKept = "artefacts/adversarial_test.go"

// advAuthoredReal is the expected loadAdversarialCases result on the real
// repository, sorted by name (AG1), plus the test names in each case's
// self-test fixtures (AG4). extraFail is the renamed seeded-defect probe in
// extra-passing_test.go and extraPass the test there that passes on the seeded
// source; extraPass is a prefix of extraFail on purpose.
var advAuthoredReal = []struct {
	name, module, pkg    string
	goodTest             string
	extraFail, extraPass string
}{
	{"frame-length-limit", "example.com/frame", "frame", "TestRefHugeLengthIsTooLarge", "TestDecodeHugeLengthIsTooLarge", "TestDecode"},
	{"loader-once-race", "example.com/memo", "memo", "TestRefOverlappingGetsLoadOnce", "TestGetOverlappingLoadsOnce", "TestGet"},
	{"path-prefix-escape", "example.com/jail", "jail", "TestRefSiblingPrefixIsOutside", "TestResolveSiblingPrefixIsOutside", "TestResolve"},
}

// advAuthoredNames reports whether msg mentions name as a whole identifier:
// an occurrence not followed by a letter, digit or underscore. It keeps a
// message that names only "TestGetOverlappingLoadsOnce" from counting as
// naming "TestGet".
func advAuthoredNames(msg, name string) bool {
	for i := 0; ; {
		j := strings.Index(msg[i:], name)
		if j < 0 {
			return false
		}
		end := i + j + len(name)
		if end == len(msg) {
			return true
		}
		c := msg[end]
		if !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return true
		}
		i = i + j + 1
	}
}

// advAuthoredPrompt is a case prompt whose brief's "Owned paths:" bullet lists
// owned, shaped like the real adversarial-tester prompts.
func advAuthoredPrompt(owned string) string {
	return "---\ndescription: Synthetic adversarial-tester case.\ntags: [adversarial-tester]\n---\n" +
		"Dispatch the `adversarial-tester` agent once with the brief below.\n\n" +
		"Brief for the adversarial tester:\n" +
		"- Goal: try to break the package.\n" +
		"- Artefact directory: `artefacts/`. Owned paths:\n  " + owned + ".\n" +
		"- Stop condition: every requirement has been probed.\n"
}

const (
	advAuthoredOwnedOne  = "`artefacts/adversarial-findings.md`, `artefacts/adversarial_test.go`,\n  `artefacts/handoff.json`"
	advAuthoredOwnedNone = "`artefacts/adversarial-findings.md`, `artefacts/handoff.json`"
	advAuthoredOwnedTwo  = "`artefacts/adversarial-findings.md`, `artefacts/adversarial_test.go`,\n  `artefacts/second_test.go`, `artefacts/handoff.json`"
	advAuthoredFixPatch  = "Fix: return two.\n--- a/x.go\n+++ b/x.go\n@@ -1,3 +1,3 @@\n package x\n \n-func F() int { return 1 }\n+func F() int { return 2 }\n"
)

// advAuthoredContract is an interface contract naming module mod, with
// sep between the word Module and the backticked path.
func advAuthoredContract(sep, mod string) string {
	return "# Interface contract: package `x`\n\nA synthetic package. Language: Go.\nModule" + sep + "`" + mod + "`, package `x` at the module root.\n\n## Requirements\n\n- R1: F returns 2.\n"
}

// advAuthoredCaseFiles is a complete synthetic case: contract, work tree,
// prompt and fix.patch.
func advAuthoredCaseFiles(contract, owned string, withFix bool) map[string]string {
	files := map[string]string{
		"testdata/contract/interface-contract.md": contract,
		"testdata/work/x.go":                      "package x\n\nfunc F() int { return 1 }\n",
		"testdata/work/x_test.go":                 "package x\n\nimport \"testing\"\n\nfunc TestF(t *testing.T) {}\n",
		"prompt.md":                               advAuthoredPrompt(owned),
	}
	if withFix {
		files["fix.patch"] = advAuthoredFixPatch
	}
	return files
}

// advAuthoredSyntheticRoot writes each case under <root>/evals/adversarial-tester/.
func advAuthoredSyntheticRoot(t *testing.T, cases map[string]map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, files := range cases {
		authoredWriteFiles(t, filepath.Join(root, "evals", "adversarial-tester", name), files)
	}
	return root
}

// AG1: the real repository holds exactly the three adversarial-tester cases,
// sorted, each with its module, the owned kept path and an existing fix.patch.
// Fails when a case is missing, extra, unsorted or carries a wrong field.
func TestAuthoredLoadAdversarialCasesReal(t *testing.T) {
	root := authoredRealRoot(t)
	got, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases: %v", err)
	}
	if len(got) != len(advAuthoredReal) {
		t.Fatalf("loadAdversarialCases returned %d cases, want %d: %+v", len(got), len(advAuthoredReal), got)
	}
	for i, want := range advAuthoredReal {
		c := got[i]
		wantDir := filepath.Join(root, "evals", "adversarial-tester", want.name)
		if c.Name != want.name || c.Module != want.module || c.KeptPath != advAuthoredKept || c.Dir != wantDir {
			t.Fatalf("case %d = %+v, want {Name %q Dir %q Module %q KeptPath %q}", i, c, want.name, wantDir, want.module, advAuthoredKept)
		}
		if !filepath.IsAbs(c.Fix) || c.Fix != filepath.Join(wantDir, "fix.patch") {
			t.Fatalf("case %s Fix = %q, want %q", c.Name, c.Fix, filepath.Join(wantDir, "fix.patch"))
		}
		if fi, err := os.Stat(c.Fix); err != nil || !fi.Mode().IsRegular() {
			t.Fatalf("case %s Fix %q is not an existing file: %v", c.Name, c.Fix, err)
		}
	}
}

// AG1 (idempotency): loading twice gives the same cases. Fails when the
// loader depends on state left by an earlier call.
func TestAuthoredLoadAdversarialCasesRepeat(t *testing.T) {
	root := authoredRealRoot(t)
	first, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases: %v", err)
	}
	second, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases (second call): %v", err)
	}
	if !slices.Equal(first, second) {
		t.Fatalf("second call = %+v, first = %+v", second, first)
	}
}

// AG1 (synthetic root): cases with fix.patch are returned sorted by name with
// fields taken from their own files; a case without fix.patch is skipped.
// Fails when a case without fix.patch is returned, the order follows anything
// but the name, the module is not read across a line break, or KeptPath is
// not the brief's owned *_test.go.
func TestAuthoredLoadAdversarialCasesSynthetic(t *testing.T) {
	root := advAuthoredSyntheticRoot(t, map[string]map[string]string{
		"zeta-case": advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/zeta"), advAuthoredOwnedOne, true),
		"alpha-case": advAuthoredCaseFiles(advAuthoredContract("\n", "example.com/alpha"),
			"`artefacts/adversarial-findings.md`, `artefacts/kept/probe_test.go`,\n  `artefacts/helper.go`, `artefacts/handoff.json`", true),
		"mid-case":    advAuthoredCaseFiles(advAuthoredContract("  \n\t", "example.com/mid/v2"), advAuthoredOwnedOne, true),
		"no-fix-case": advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/nofix"), advAuthoredOwnedOne, false),
	})
	got, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases: %v", err)
	}
	want := []struct{ name, module, kept string }{
		{"alpha-case", "example.com/alpha", "artefacts/kept/probe_test.go"},
		{"mid-case", "example.com/mid/v2", advAuthoredKept},
		{"zeta-case", "example.com/zeta", advAuthoredKept},
	}
	if len(got) != len(want) {
		t.Fatalf("loadAdversarialCases returned %d cases, want %d (no-fix-case skipped): %+v", len(got), len(want), got)
	}
	for i, w := range want {
		c := got[i]
		dir := filepath.Join(root, "evals", "adversarial-tester", w.name)
		if c.Name != w.name || c.Dir != dir || c.Module != w.module || c.KeptPath != w.kept || c.Fix != filepath.Join(dir, "fix.patch") {
			t.Fatalf("case %d = %+v, want {Name %q Dir %q Module %q KeptPath %q Fix %q}", i, c, w.name, dir, w.module, w.kept, filepath.Join(dir, "fix.patch"))
		}
	}
}

// AG1 (skip comes first): a case without fix.patch is skipped even when its
// contract names no module and its brief owns no test file. Fails when the
// loader validates cases it does not return.
func TestAuthoredLoadAdversarialCasesSkipsMalformedWithoutFix(t *testing.T) {
	root := advAuthoredSyntheticRoot(t, map[string]map[string]string{
		"good-case":     advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/good"), advAuthoredOwnedOne, true),
		"no-fix-broken": advAuthoredCaseFiles("# Interface contract\n\nNo module here.\n", advAuthoredOwnedNone, false),
	})
	got, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good-case" {
		t.Fatalf("loadAdversarialCases = %+v, want only good-case", got)
	}
}

// AG1 (failure modes): a case with fix.patch whose contract names no module,
// or whose brief owns zero or two *_test.go paths, makes the load fail. Fails
// when such a case is returned or silently dropped.
func TestAuthoredLoadAdversarialCasesRejects(t *testing.T) {
	bad := map[string]map[string]string{
		"contract without module": advAuthoredCaseFiles(
			"# Interface contract: package `x`\n\nLanguage: Go. Package `x` at the module root.\n", advAuthoredOwnedOne, true),
		"module word without a backticked path": advAuthoredCaseFiles(
			"# Interface contract: package `x`\n\nLanguage: Go. Module path is unknown.\n", advAuthoredOwnedOne, true),
		"brief owns no test file":   advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/x"), advAuthoredOwnedNone, true),
		"brief owns two test files": advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/x"), advAuthoredOwnedTwo, true),
	}
	for name, files := range bad {
		t.Run(name, func(t *testing.T) {
			root := advAuthoredSyntheticRoot(t, map[string]map[string]string{
				"good-case": advAuthoredCaseFiles(advAuthoredContract(" ", "example.com/good"), advAuthoredOwnedOne, true),
				"bad-case":  files,
			})
			if got, err := loadAdversarialCases(root); err == nil {
				t.Fatalf("loadAdversarialCases = %+v, nil; want an error for bad-case", got)
			}
		})
	}
}

// advAuthoredNamesSrc exercises every form named in keptTestNames' doc
// comment. Listed, in source order: TestB, TestA1_x, Test, Test_under,
// Test9, TestAnon, TestZ.
const advAuthoredNamesSrc = `package p

import "testing"

type s struct{}

func TestB(t *testing.T) {}

func TestA1_x(t *testing.T) {}

func Test(t *testing.T) {}

func Testify(t *testing.T) {}

func TestMain(m *testing.M) {}

func (s) TestX(t *testing.T) {}

func (x *s) TestPtrRecv(t *testing.T) {}

func BenchmarkX(b *testing.B) {}

func FuzzX(f *testing.F) {}

func ExampleX() {}

func testX(t *testing.T) {}

func TestTwo(t *testing.T, n int) {}

func TestPair(a, b *testing.T) {}

func TestNone() {}

func TestValue(t testing.T) {}

func TestBench(b *testing.B) {}

func TestGeneric[T any](t *testing.T) {}

var TestVar = func(t *testing.T) {}

func Test_under(t *testing.T) {}

func Test9(t *testing.T) {}

func TestAnon(*testing.T) {}

func TestZ(t *testing.T) {
	func() {}()
}
`

// AG2: keptTestNames lists exactly the top-level TestXxx(t *testing.T)
// functions in source order. Fails when a listed form is missing, an excluded
// form (Testify, TestMain(m *testing.M), methods, Benchmark/Fuzz/Example,
// helpers, extra or missing parameters, type parameters, a var) is listed, or
// the order is changed.
func TestAuthoredAdversarialKeptTestNames(t *testing.T) {
	got, err := keptTestNames([]byte(advAuthoredNamesSrc))
	if err != nil {
		t.Fatalf("keptTestNames: %v", err)
	}
	want := []string{"TestB", "TestA1_x", "Test", "Test_under", "Test9", "TestAnon", "TestZ"}
	if !slices.Equal(got, want) {
		t.Fatalf("keptTestNames = %q, want %q", got, want)
	}
	again, err := keptTestNames([]byte(advAuthoredNamesSrc))
	if err != nil || !slices.Equal(again, want) {
		t.Fatalf("keptTestNames (second call) = %q, %v; want %q, nil", again, err, want)
	}
}

// AG2 (boundary): source that parses but has no test function lists nothing
// and is not an error. Fails when an empty result is reported as a parse
// error or a non-test function is listed.
func TestAuthoredAdversarialKeptTestNamesNone(t *testing.T) {
	for name, src := range map[string]string{
		"package only": "package p\n",
		"helpers only": "package p\n\nimport \"testing\"\n\nfunc helper(t *testing.T) {}\n\nfunc BenchmarkY(b *testing.B) {}\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := keptTestNames([]byte(src))
			if err != nil || len(got) != 0 {
				t.Fatalf("keptTestNames = %q, %v; want no names and nil", got, err)
			}
		})
	}
}

// AG2 (failure and security): source that does not parse as Go is an error.
// Fails when malformed or empty input yields names or a nil error.
func TestAuthoredAdversarialKeptTestNamesUnparsable(t *testing.T) {
	for name, src := range map[string]string{
		"empty":            "",
		"not go":           "this is not Go source\n",
		"unclosed body":    "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) {\n",
		"missing package":  "import \"testing\"\n\nfunc TestA(t *testing.T) {}\n",
		"binary garbage":   "package p\n\x00\xff\xfe func TestA(",
		"oversized broken": "package p\n\n" + strings.Repeat("func TestA(t *testing.T) {}\n", 5000) + "func TestB(t *testing.T) {\n",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := keptTestNames([]byte(src)); err == nil {
				t.Fatalf("keptTestNames = %q, nil; want a parse error", got)
			}
		})
	}
}

// AG2 (property over the name domain): for randomly generated files, the
// listed names are exactly the test-shaped declarations whose name is "Test"
// alone or "Test" followed by a non-lower-case character, in source order.
// The expected list is computed independently from the generator. Fails when
// any name or signature form is classified against the doc comment.
func TestAuthoredAdversarialKeptTestNamesProperty(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261108, 1))
	firsts := []string{"", "a", "z", "m", "A", "Z", "Q", "0", "9", "_"}
	for iter := 0; iter < 300; iter++ {
		var b strings.Builder
		b.WriteString("package p\n\nimport \"testing\"\n\ntype recv struct{}\n\n")
		var want []string
		usedBare := false
		n := rng.IntN(12)
		for i := 0; i < n; i++ {
			first := firsts[rng.IntN(len(firsts))]
			name := "Test" + first + fmt.Sprintf("q%d", i)
			if first == "" {
				if usedBare {
					first = "B"
					name = "TestBq" + fmt.Sprint(i)
				} else {
					usedBare = true
					name = "Test"
				}
			}
			listedName := first == "" || !(first[0] >= 'a' && first[0] <= 'z')
			switch rng.IntN(8) {
			case 0, 1, 2:
				fmt.Fprintf(&b, "func %s(t *testing.T) {}\n\n", name)
				if listedName {
					want = append(want, name)
				}
			case 3:
				fmt.Fprintf(&b, "func (r recv) %s(t *testing.T) {}\n\n", name)
			case 4:
				fmt.Fprintf(&b, "func %s(t *testing.T, n int) {}\n\n", name)
			case 5:
				fmt.Fprintf(&b, "func %s[T any](t *testing.T) {}\n\n", name)
			case 6:
				fmt.Fprintf(&b, "func %s(b *testing.B) {}\n\n", name)
			case 7:
				fmt.Fprintf(&b, "func Benchmark%s(b *testing.B) {}\n\n", strings.TrimPrefix(name, "Test"))
			}
		}
		src := b.String()
		got, err := keptTestNames([]byte(src))
		if err != nil {
			t.Fatalf("iteration %d: keptTestNames: %v\nsource:\n%s", iter, err, src)
		}
		if !slices.Equal(got, want) {
			t.Fatalf("iteration %d: keptTestNames = %q, want %q\nsource:\n%s", iter, got, want, src)
		}
	}
}

// AG2 (security): arbitrary bytes never panic, and every name listed for
// source that parses starts with "Test" and occurs in the source. Fails on a
// panic or an invented name.
func FuzzAuthoredAdversarialKeptTestNames(f *testing.F) {
	f.Add([]byte(advAuthoredNamesSrc))
	f.Add([]byte(""))
	f.Add([]byte("package p\nfunc Test(t *testing.T){}"))
	f.Add([]byte("package p\n\x00"))
	f.Fuzz(func(t *testing.T, src []byte) {
		names, err := keptTestNames(src)
		if err != nil {
			return
		}
		for _, n := range names {
			if !strings.HasPrefix(n, "Test") || !strings.Contains(string(src), n) {
				t.Fatalf("keptTestNames listed %q, which is not a Test name in the source", n)
			}
		}
	})
}

// AG3: the gate verdict. Fails when err accepts a passing seeded test, an
// empty Seeded or a failing Fixed, rejects the all-failed-plus-fixed-passed
// report, or does not name the case and each passing seeded test.
func TestAuthoredAdversarialReportErr(t *testing.T) {
	failed := func(name string) keptOutcome {
		return keptOutcome{Name: name, runOutcome: runOutcome{Passed: false, Output: "--- FAIL: " + name}}
	}
	passed := func(name string) keptOutcome {
		return keptOutcome{Name: name, runOutcome: runOutcome{Passed: true, Output: "ok"}}
	}
	fixedOK := runOutcome{Passed: true, Output: "ok"}
	fixedBad := runOutcome{Passed: false, Output: "FAIL"}

	t.Run("all seeded failed and fixed passed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{failed("TestKeptOne"), failed("TestKeptTwo")}, Fixed: fixedOK}
		if err := r.err(); err != nil {
			t.Fatalf("err() = %v, want nil", err)
		}
	})
	t.Run("one seeded failed and fixed passed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{failed("TestKeptOne")}, Fixed: fixedOK}
		if err := r.err(); err != nil {
			t.Fatalf("err() = %v, want nil", err)
		}
	})
	t.Run("seeded tests passed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{passed("TestAlive"), failed("TestDead"), passed("TestAliveToo")}, Fixed: fixedOK}
		err := r.err()
		if err == nil {
			t.Fatal("err() = nil, want an error for the passing seeded tests")
		}
		for _, name := range []string{"demo-case", "TestAlive", "TestAliveToo"} {
			if !advAuthoredNames(err.Error(), name) {
				t.Fatalf("err() %q does not name %s", err, name)
			}
		}
	})
	t.Run("only seeded test passed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{passed("TestOnly")}, Fixed: fixedOK}
		err := r.err()
		if err == nil || !advAuthoredNames(err.Error(), "TestOnly") || !strings.Contains(err.Error(), "demo-case") {
			t.Fatalf("err() = %v, want an error naming demo-case and TestOnly", err)
		}
	})
	for name, seeded := range map[string][]keptOutcome{"nil Seeded": nil, "empty Seeded": {}} {
		t.Run(name, func(t *testing.T) {
			r := adversarialReport{Case: "demo-case", Seeded: seeded, Fixed: fixedOK}
			if err := r.err(); err == nil || !strings.Contains(err.Error(), "demo-case") {
				t.Fatalf("err() = %v, want an error naming demo-case", err)
			}
		})
	}
	t.Run("fixed failed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{failed("TestKeptOne")}, Fixed: fixedBad}
		if err := r.err(); err == nil || !strings.Contains(err.Error(), "demo-case") {
			t.Fatalf("err() = %v, want an error naming demo-case", err)
		}
	})
	t.Run("fixed failed and a seeded test passed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Seeded: []keptOutcome{passed("TestAlive")}, Fixed: fixedBad}
		if err := r.err(); err == nil || !strings.Contains(err.Error(), "demo-case") {
			t.Fatalf("err() = %v, want an error naming demo-case", err)
		}
	})
	t.Run("empty Seeded and fixed failed", func(t *testing.T) {
		r := adversarialReport{Case: "demo-case", Fixed: fixedBad}
		if err := r.err(); err == nil {
			t.Fatal("err() = nil, want an error")
		}
	})
}

// advAuthoredFixture reads a self-test kept file for case name.
func advAuthoredFixture(t *testing.T, root, name, file string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "evals", "testdata", "adversarial-selftest", name, file))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// advAuthoredSnapshot reads every file under dir, keyed by slash path.
func advAuthoredSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// advAuthoredRealCases loads the real cases and pairs each with its row in
// advAuthoredReal.
func advAuthoredRealCases(t *testing.T, root string) []adversarialCase {
	t.Helper()
	cases, err := loadAdversarialCases(root)
	if err != nil {
		t.Fatalf("loadAdversarialCases: %v", err)
	}
	if len(cases) != len(advAuthoredReal) {
		t.Fatalf("loadAdversarialCases returned %d cases, want %d", len(cases), len(advAuthoredReal))
	}
	for i, c := range cases {
		if c.Name != advAuthoredReal[i].name {
			t.Fatalf("case %d is %q, want %q", i, c.Name, advAuthoredReal[i].name)
		}
	}
	return cases
}

// advAuthoredWantSeeded checks the Seeded entries' names (source order) and
// outcomes. A failing entry must show go test's own failure line for that
// test, so a build failure does not pass for a failing probe.
func advAuthoredWantSeeded(t *testing.T, r adversarialReport, names []string, passed []bool) {
	t.Helper()
	if len(r.Seeded) != len(names) {
		t.Fatalf("report has %d Seeded entries, want %d (%q): %+v", len(r.Seeded), len(names), names, r.Seeded)
	}
	for i, s := range r.Seeded {
		if s.Name != names[i] {
			t.Fatalf("Seeded[%d].Name = %q, want %q (source order)", i, s.Name, names[i])
		}
		if s.Passed != passed[i] {
			t.Fatalf("Seeded[%d] (%s) Passed = %v, want %v; output:\n%s", i, s.Name, s.Passed, passed[i], s.Output)
		}
		if !s.Passed && !strings.Contains(s.Output, "--- FAIL: "+s.Name) {
			t.Fatalf("Seeded[%d] (%s) failed without go test reporting that test as failed; output:\n%s", i, s.Name, s.Output)
		}
	}
}

// AG4: runAdversarial on each real case with the known-good kept file, a kept
// file adding a test that already passes on the seeded source, and a kept
// file that fails for a reason fix.patch does not repair. Fails when the
// gate accepts a passing kept test or a broken fixed package, rejects a good
// kept file, runs a kept test with another one (-run not anchored), reports
// outcomes out of source order, or touches the case's testdata/work.
func TestAuthoredRunAdversarialSelfTest(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test against every case")
	}
	root := authoredRealRoot(t)
	cases := advAuthoredRealCases(t, root)
	for i, c := range cases {
		row := advAuthoredReal[i]
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			work := filepath.Join(c.Dir, "testdata", "work")
			before := advAuthoredSnapshot(t, work)
			t.Cleanup(func() {
				after := advAuthoredSnapshot(t, work)
				if len(after) != len(before) {
					t.Errorf("testdata/work changed: files %v, before %v", slices.Sorted(maps.Keys(after)), slices.Sorted(maps.Keys(before)))
					return
				}
				for p, body := range before {
					if after[p] != body {
						t.Errorf("runAdversarial changed %s in the case's testdata/work", p)
					}
				}
			})

			t.Run("good", func(t *testing.T) {
				t.Parallel()
				r, err := runAdversarial(t.Context(), c, advAuthoredFixture(t, root, c.Name, "good_test.go"), t.TempDir())
				if err != nil {
					t.Fatalf("runAdversarial: harness error %v", err)
				}
				if r.Case != c.Name {
					t.Fatalf("report.Case = %q, want %q", r.Case, c.Name)
				}
				advAuthoredWantSeeded(t, r, []string{row.goodTest}, []bool{false})
				if !r.Fixed.Passed {
					t.Fatalf("good kept file: package fails after fix.patch:\n%s", r.Fixed.Output)
				}
				if err := r.err(); err != nil {
					t.Fatalf("good kept file: err() = %v, want nil", err)
				}
			})

			t.Run("extra-passing", func(t *testing.T) {
				t.Parallel()
				r, err := runAdversarial(t.Context(), c, advAuthoredFixture(t, root, c.Name, "extra-passing_test.go"), t.TempDir())
				if err != nil {
					t.Fatalf("runAdversarial: harness error %v", err)
				}
				advAuthoredWantSeeded(t, r, []string{row.extraFail, row.extraPass}, []bool{false, true})
				if !r.Fixed.Passed {
					t.Fatalf("extra-passing kept file: package fails after fix.patch:\n%s", r.Fixed.Output)
				}
				gateErr := r.err()
				if gateErr == nil {
					t.Fatalf("extra-passing kept file: err() = nil, want an error naming %s", row.extraPass)
				}
				if !advAuthoredNames(gateErr.Error(), row.extraPass) || !strings.Contains(gateErr.Error(), c.Name) {
					t.Fatalf("err() %q does not name %s and %s", gateErr, row.extraPass, c.Name)
				}
			})

			t.Run("unrelated-fail", func(t *testing.T) {
				t.Parallel()
				r, err := runAdversarial(t.Context(), c, advAuthoredFixture(t, root, c.Name, "unrelated-fail_test.go"), t.TempDir())
				if err != nil {
					t.Fatalf("runAdversarial: harness error %v", err)
				}
				advAuthoredWantSeeded(t, r, []string{"TestAlways"}, []bool{false})
				if r.Fixed.Passed {
					t.Fatal("unrelated-fail kept file: Fixed.Passed = true, want the package to fail after fix.patch")
				}
				if gateErr := r.err(); gateErr == nil || !strings.Contains(gateErr.Error(), c.Name) {
					t.Fatalf("unrelated-fail kept file: err() = %v, want an error naming %s", gateErr, c.Name)
				}
			})
		})
	}
}

// AG4 (idempotency): two runs of the good kept file in fresh work
// directories give the same outcomes. Fails when a run depends on state left
// by an earlier one. Uses path-prefix-escape, whose probe is deterministic.
func TestAuthoredRunAdversarialRepeat(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test")
	}
	root := authoredRealRoot(t)
	var c adversarialCase
	for _, x := range advAuthoredRealCases(t, root) {
		if x.Name == "path-prefix-escape" {
			c = x
		}
	}
	kept := advAuthoredFixture(t, root, c.Name, "good_test.go")
	var outcomes [2][]bool
	for i := range outcomes {
		r, err := runAdversarial(t.Context(), c, kept, t.TempDir())
		if err != nil {
			t.Fatalf("run %d: runAdversarial: %v", i, err)
		}
		for _, s := range r.Seeded {
			outcomes[i] = append(outcomes[i], s.Passed)
		}
		outcomes[i] = append(outcomes[i], r.Fixed.Passed)
		if err := r.err(); err != nil {
			t.Fatalf("run %d: err() = %v, want nil", i, err)
		}
	}
	if !slices.Equal(outcomes[0], outcomes[1]) {
		t.Fatalf("outcomes differ between runs: %v vs %v", outcomes[0], outcomes[1])
	}
}

// AG4 (harness errors): a kept file with no test function, a kept file that
// does not parse, and a fix.patch that does not apply are errors, not
// reports. Fails when any of them yields a nil error.
func TestAuthoredRunAdversarialHarnessErrors(t *testing.T) {
	if testing.Short() {
		t.Skip("runs go test")
	}
	root := authoredRealRoot(t)
	cases := advAuthoredRealCases(t, root)
	for i, c := range cases {
		pkg := advAuthoredReal[i].pkg
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			kept := map[string]string{
				"no test functions": "package " + pkg + "\n\nimport \"testing\"\n\nfunc helper(t *testing.T) {}\n\nfunc BenchmarkKept(b *testing.B) {}\n",
				"empty package":     "package " + pkg + "\n",
				"does not parse":    "package " + pkg + "\n\nimport \"testing\"\n\nfunc TestBroken(t *testing.T) {\n",
			}
			for name, src := range kept {
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					if r, err := runAdversarial(t.Context(), c, []byte(src), t.TempDir()); err == nil {
						t.Fatalf("runAdversarial = %+v, nil; want an error", r)
					}
				})
			}
			t.Run("fix.patch does not apply", func(t *testing.T) {
				t.Parallel()
				bogus := filepath.Join(t.TempDir(), "fix.patch")
				patch := "Fix: targets a file that does not exist.\n--- a/no/such/file.go\n+++ b/no/such/file.go\n@@ -1,1 +1,1 @@\n-package x\n+package y\n"
				if err := os.WriteFile(bogus, []byte(patch), 0o644); err != nil {
					t.Fatal(err)
				}
				bad := c
				bad.Fix = bogus
				r, err := runAdversarial(t.Context(), bad, advAuthoredFixture(t, root, c.Name, "good_test.go"), t.TempDir())
				if err == nil {
					t.Fatalf("runAdversarial with an unappliable fix.patch = %+v, nil; want an error", r)
				}
			})
		})
	}
}
