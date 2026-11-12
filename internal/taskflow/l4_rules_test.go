package taskflow_test

// Test rules and stored results (interface contract L4, third version): R1,
// R2 and R6. A record without test rules is refused at load
// (legacy_record_test.go).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	agentworkflows "github.com/belevtsev/agent-workflows/v3"
	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// l4StoredTestRules returns, per stored revision, whether it has a
// test_rules member and its value.
func l4StoredTestRules(t *testing.T, dir string) []any {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, "record-*.json"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no record files: %v", err)
	}
	sort.Strings(names)
	var out []any
	for _, n := range names {
		data, err := os.ReadFile(n)
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		v, ok := doc["test_rules"]
		if !ok {
			v = nil
		}
		out = append(out, v)
	}
	return out
}

// TestStartRecordsTestRules fails when StartWithPacks does not record
// test_rules go.test-evidence/v1 whatever the selection and the packs
// applied, or when it is not kept by later revisions (R1).
func TestStartRecordsTestRules(t *testing.T) {
	t.Parallel()
	if taskflow.TestRulesV1 != "go.test-evidence/v1" {
		t.Fatalf("TestRulesV1 = %q, want %q", taskflow.TestRulesV1, "go.test-evidence/v1")
	}
	builtIn, err := langpack.LoadManifests(agentworkflows.Packs)
	if err != nil {
		t.Fatalf("built-in packs: %v", err)
	}
	ctx := context.Background()
	cases := []struct {
		name, selection string
		files           map[string]string
		selected        []langpack.Manifest
	}{
		{"default selection, go pack applied", taskflow.SelectionDefault, map[string]string{"go.mod": "module m\n"}, langpack.DefaultSelection(builtIn)},
		{"default selection, no pack detected", taskflow.SelectionDefault, map[string]string{"README.md": "x\n"}, langpack.DefaultSelection(builtIn)},
		{"explicit empty selection", taskflow.SelectionExplicit, map[string]string{"go.mod": "module m\n"}, nil},
		{"explicit other pack", taskflow.SelectionExplicit, map[string]string{"go.mod": "module m\n"},
			[]langpack.Manifest{spManifest("other", "r.other/v1", "go.mod")}},
	}
	for _, c := range cases {
		repo, store := spRepo(t, c.files)
		rec, err := taskflow.StartWithPacks(ctx, repo, store, spPlan("true"), nil, c.selection, c.selected)
		if err != nil {
			t.Fatalf("%s: StartWithPacks: %v", c.name, err)
		}
		if rec.TestRules != l4Rules {
			t.Errorf("%s: returned record test_rules %q, want %q (R1)", c.name, rec.TestRules, l4Rules)
		}
		if _, err := taskflow.Run(ctx, rec.Directory, "unit"); err != nil {
			t.Fatalf("%s: Run: %v", c.name, err)
		}
		loaded, err := taskflow.Load(rec.Directory)
		if err != nil || loaded.TestRules != l4Rules || loaded.Revision < 2 {
			t.Errorf("%s: loaded test_rules %q at revision %d (%v), want %q kept by later revisions (R1, R2)", c.name, loaded.TestRules, loaded.Revision, err, l4Rules)
		}
		for i, v := range l4StoredTestRules(t, rec.Directory) {
			if v != l4Rules {
				t.Errorf("%s: stored revision %d test_rules = %v, want %q (R1)", c.name, i+1, v, l4Rules)
			}
		}
	}
}

// TestLoadChecksTestRules fails when a stored test_rules that is invalid in
// revision 1, absent from revision 1, or changed or removed in a later
// revision loads, or fails with another message than R2 or the start-member
// refusal gives, or when a rewritten but unchanged chain does not load (R2).
func TestLoadChecksTestRules(t *testing.T) {
	t.Parallel()
	set := func(v any) func(int, map[string]any) {
		return func(_ int, doc map[string]any) { doc["test_rules"] = v }
	}
	cases := []struct {
		name string
		edit func(rev int, doc map[string]any)
		want string // "" loads
	}{
		{"unchanged chain loads", func(int, map[string]any) {}, ""},
		{"invalid value in every revision", set("go.test-evidence/v2"), l4InvalidRules},
		{"test rules without a baseline manifest", func(_ int, doc map[string]any) { delete(doc, "baseline_manifest") }, "task record has no baseline_manifest" + l4NewTask},
		{"absent from revision 1, added later", func(rev int, doc map[string]any) {
			if rev == 1 {
				delete(doc, "test_rules")
			}
		}, "task record has no test_rules" + l4NewTask},
		{"removed in a later revision", func(rev int, doc map[string]any) {
			if rev >= 2 {
				delete(doc, "test_rules")
			}
		}, l4Immutable},
		{"changed to an invalid value in a later revision", func(rev int, doc map[string]any) {
			if rev >= 2 {
				doc["test_rules"] = "bogus"
			}
		}, l4Immutable},
		{"changed only in revision 3", func(rev int, doc map[string]any) {
			if rev == 3 {
				doc["test_rules"] = "go.test-evidence/v2"
			}
		}, l4Immutable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := l4New(t, l4Opts{checks: []taskflow.Check{l3Check("unit", "go", "version")}})
			for i := 0; i < 2; i++ {
				if run, err := taskflow.Run(f.ctx, f.dir, "unit"); run.ID == "" {
					t.Fatalf("Run: %v", err)
				}
			}
			if rev := f.revision(); rev < 3 {
				t.Fatalf("fixture has only %d revisions", rev)
			}
			f.rewrite(c.edit)
			_, err := taskflow.Load(f.dir)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("Load of %s: %v, want it to load", c.name, err)
			case c.want != "" && err == nil:
				t.Errorf("Load of %s succeeded, want an error containing %q (R2)", c.name, c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Errorf("Load of %s: %v, want an error containing %q (R2)", c.name, err, c.want)
			}
			if c.want == l4Immutable && err != nil && strings.Contains(err.Error(), l4InvalidRules) {
				t.Errorf("Load of %s reported %q; a later revision's change is %q (R2)", c.name, l4InvalidRules, l4Immutable)
			}
		})
	}
}

// l4SetStoredGofmt returns a rewrite edit that sets gofmt_sha256 of the
// stored changed-file entry of path, in every revision that holds it.
func l4SetStoredGofmt(path, value string) func(int, map[string]any) {
	return func(_ int, doc map[string]any) {
		results, _ := doc["worker_results"].([]any)
		for _, x := range results {
			m, _ := x.(map[string]any)
			res, _ := m["result"].(map[string]any)
			files, _ := res["changed_files"].([]any)
			for _, y := range files {
				if cf, _ := y.(map[string]any); cf["path"] == path {
					cf["gofmt_sha256"] = value
				}
			}
		}
	}
}

// TestLoadChecksStoredGofmt fails when a stored worker result that breaks
// R13.a loads, or fails with another message than the handoff gives: a
// gofmt_sha256 on a result without a contract, on a testdata path, on a
// deleted entry or with a malformed value; or when a stored value rewritten
// unchanged does not load (R13.a).
func TestLoadChecksStoredGofmt(t *testing.T) {
	t.Parallel()
	test := l4Test("p", "TestA")
	h, ok := l4Gofmt([]byte(test))
	if !ok {
		t.Fatal("fixture does not parse")
	}
	cases := []struct {
		name  string
		setup func(f *l3Fix) string // records a result, returns the edited path
		value string
		want  string // "" loads
	}{
		{"result without a contract", func(f *l3Fix) string {
			f.plain("impl", "", map[string]string{"p/a_test.go": test})
			return "p/a_test.go"
		}, h, fmt.Sprintf(l4OnlyMsg, "p/a_test.go")},
		{"testdata path", func(f *l3Fix) string {
			f.te("te1", "", "completed", map[string]string{"p/testdata/a_test.go": test})
			return "p/testdata/a_test.go"
		}, h, fmt.Sprintf(l4OnlyMsg, "p/testdata/a_test.go")},
		{"deleted entry", func(f *l3Fix) string {
			f.te("te1", "", "completed", map[string]string{"p/base_test.go": ""})
			return "p/base_test.go"
		}, h, l4ShapeMsg},
		{"63 characters", func(f *l3Fix) string {
			f.te("te1", "", "completed", map[string]string{"p/a_test.go": test})
			return "p/a_test.go"
		}, h[:63], l4ShapeMsg},
		{"not hexadecimal", func(f *l3Fix) string {
			f.te("te1", "", "completed", map[string]string{"p/a_test.go": test})
			return "p/a_test.go"
		}, strings.Repeat("z", 64), l4ShapeMsg},
		{"the value cw stored, rewritten unchanged", func(f *l3Fix) string {
			f.te("te1", "", "completed", map[string]string{"p/a_test.go": test})
			return "p/a_test.go"
		}, h, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f := l4New(t, l4Opts{files: map[string]string{"p/base_test.go": l4Test("p", "TestBase")}})
			path := c.setup(f)
			f.rewrite(l4SetStoredGofmt(path, c.value))
			if got := l3JSON(f.latestRecordJSON()); !strings.Contains(got, `"gofmt_sha256":"`+c.value+`"`) {
				t.Fatalf("fixture: the rewritten record holds no gofmt_sha256 %q for %s", c.value, path)
			}
			_, err := taskflow.Load(f.dir)
			switch {
			case c.want == "" && err != nil:
				t.Errorf("Load: %v, want it to load", err)
			case c.want != "" && err == nil:
				t.Errorf("Load succeeded, want an error containing %q (R13.a: a stored result that breaks it fails to load)", c.want)
			case c.want != "" && !strings.Contains(err.Error(), c.want):
				t.Errorf("Load: %v, want an error containing %q (R13.a)", err, c.want)
			}
		})
	}
}

// l4ForgeTests is a test file whose TestForge writes test2json framing for
// other tests of the package: a PASS of TestGhost (declared only in a
// build-constrained file) and of TestSkipped, an extra RUN and PASS pair of
// TestTwice and TestBoth, and, only when -count is 1, of TestOnce. TestMarked
// and TestBoth write a raw ESC 0x16 line, which go test -json reports as
// output beginning with 0x16 (observed go1.27.3 behaviour).
const l4ForgeTests = `package p

import (
	"flag"
	"fmt"
	"os"
	"testing"
)

func TestPass(t *testing.T) {}

func TestForge(t *testing.T) {
	fmt.Fprint(os.Stdout, "\x16--- PASS: TestGhost (0.00s)\n")
	fmt.Fprint(os.Stdout, "\x16--- PASS: TestSkipped (0.00s)\n")
	fmt.Fprint(os.Stdout, "\x16=== RUN   TestTwice\n\x16--- PASS: TestTwice (0.00s)\n")
	fmt.Fprint(os.Stdout, "\x16=== RUN   TestBoth\n\x16--- PASS: TestBoth (0.00s)\n")
	if flag.Lookup("test.count").Value.String() == "1" {
		fmt.Fprint(os.Stdout, "\x16=== RUN   TestOnce\n\x16--- PASS: TestOnce (0.00s)\n")
	}
}

func TestTwice(t *testing.T) {}

func TestOnce(t *testing.T) {}

func TestSkipped(t *testing.T) { t.Skip("skipped by the fixture") }

func TestMarked(t *testing.T) { fmt.Fprint(os.Stdout, "\x1b\x16marked\n") }

func TestBoth(t *testing.T) { fmt.Fprint(os.Stdout, "\x1b\x16marked\n") }
`

const l4GhostTests = `//go:build never

package p

import "testing"

func TestGhost(t *testing.T) {}
`

// l4ForgeFiles are the files of the forging fixture, by worker path.
// TestTwice is also declared in a file go test ignores (a leading _) and in a
// test file of the subdirectory p/sub: neither is among R7.a's sources for a
// test of p/te_test.go, so neither raises its declaration count.
func l4ForgeFiles() map[string]string {
	return map[string]string{
		"p/te_test.go":        l4ForgeTests,
		"p/ghost_test.go":     l4GhostTests,
		"p/dup_test.go":       l4Test("p", "TestDup"),
		"p/dup_ext_test.go":   l4Test("p_test", "TestDup"),
		"p/_twice_test.go":    l4Test("p_test", "TestTwice"),
		"p/sub/twice_test.go": l4Test("sub", "TestTwice"),
	}
}

// TestRunStoresResultsWithRuns fails when a terminal run of a record with
// test rules does not store ResultsWithRuns of its output (run and marked
// counts, omitted when 0), for a passed and a failed run, or writes a results
// file for output without test events (R6).
func TestRunStoresResultsWithRuns(t *testing.T) {
	t.Parallel()
	f := l4New(t, l4Opts{checks: []taskflow.Check{
		l3Check("ok", l3JSONP...), l3Check("bad", l3JSONQ...), l3Check("none", "go", "version"),
	}})
	f.mustAssign(f.brief("te1", []string{"p/te_test.go", "q/te_test.go"}, l3Req("R1", "C1")))
	marked := l3PTests + "\nfunc TestMarked(t *testing.T) { fmt.Fprint(os.Stdout, \"\\x1b\\x16marked\\n\") }\n"
	marked = strings.Replace(marked, "import \"testing\"", "import (\n\t\"fmt\"\n\t\"os\"\n\t\"testing\"\n)", 1)
	f.write("p/te_test.go", marked)
	f.write("q/te_test.go", l3QTests)
	f.mustSubmit(f.result("te1", "completed", []string{"p/te_test.go", "q/te_test.go"}, l3Uncovered("R1", l4UncoveredReason)))
	ok, bad, none := f.run("ok"), f.run("bad"), f.run("none")
	if ok.Status != "passed" || bad.Status != "failed" || none.Status != "passed" {
		t.Fatalf("fixture runs %q %q %q", ok.Status, bad.Status, none.Status)
	}
	p, q := l3Module+"/p", l3Module+"/q"
	want := map[string]langpack.GoTestResults{
		"ok": {Tests: []langpack.GoTestResult{
			{Package: p, Test: "TestMarked", Pass: 1, Run: 1, Marked: 1},
			{Package: p, Test: "TestPass", Pass: 1, Run: 1},
			{Package: p, Test: "TestSkip", Skip: 1, Run: 1},
			{Package: p, Test: "TestSub", Pass: 1, Run: 1},
			{Package: p, Test: "TestSub/a", Pass: 1, Run: 1},
		}},
		"bad": {Tests: []langpack.GoTestResult{
			{Package: q, Test: "TestFail", Fail: 1, Run: 1},
			{Package: q, Test: "TestQPass", Pass: 1, Run: 1},
		}},
	}
	for id, run := range map[string]taskflow.CheckRun{"ok": ok, "bad": bad} {
		got, err := l3ReadResults(f.dir, run)
		if err != nil {
			t.Fatalf("ReadTestResults(%s): %v", id, err)
		}
		if !reflect.DeepEqual(got, want[id]) {
			t.Errorf("stored results of %s:\n got  %s\n want %s (R6)", id, l3JSON(got), l3JSON(want[id]))
		}
		raw, err := os.ReadFile(f.resultsFile(run))
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Tests []map[string]any `json:"tests"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("results file of %s: %v", id, err)
		}
		for _, e := range doc.Tests {
			if _, has := e["run"]; !has {
				t.Errorf("results file entry %v of %s lacks run (R6)", e, id)
			}
			if _, has := e["marked"]; has != (e["test"] == "TestMarked") {
				t.Errorf("results file entry %v of %s: marked member present %v, want it only when non-zero (R6)", e, id, has)
			}
		}
	}
	if none.TestResults != "" || none.TestResultsSHA256 != "" {
		t.Errorf("run without test events stored results %q (R6: written exactly when L3 R15 and R16 write it)", none.TestResults)
	}
}
