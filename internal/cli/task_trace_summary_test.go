package cli_test

// cw task trace --summary (R37) and the failing tests of cw task run
// --summary (R45), interface contract L3, revision 3.

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/cli"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// l3cObject decodes stdout as exactly one JSON object and nothing else.
func l3cObject(t *testing.T, label, out string) map[string]any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(out))
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		t.Fatalf("%s: stdout is not one JSON object: %v\n%s", label, err, out)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		t.Errorf("%s: stdout holds more than one JSON value: %q", label, out)
	}
	return obj
}

func l3cKeys(m map[string]any) []string {
	var keys []string
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// TestTaskTraceCLISummary fails when the summary has other members than
// task, task_id, criteria, gaps and problems, when criteria does not map each
// plan requirement to its status, or when gaps differ from R37 in content,
// order or omitted members.
func TestTaskTraceCLISummary(t *testing.T) {
	t.Parallel()
	f := l3cNew(t, []string{"C1", "C2", "C3", "C4", "C5"},
		l3cCheck("ok", "go", "test", "-json", "./p/..."), l3cCheck("bad", "go", "test", "-json", "./q/..."))
	f.assign("te0", []string{"p/old_test.go"}, l3cReq("R0", "C3"))
	f.write("p/old_test.go", "package p\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n")
	f.submit("te0", []string{"p/old_test.go"}, l3cMap("R0", l3cTest("p/old_test.go", "TestOld")))
	f.accept("te0", "unused")

	f.assign("te1", []string{"p/te_test.go", "p/never_test.go", "q/te_test.go"},
		l3cReq("R1", "C1"), l3cReq("R2", "C2"), l3cReq("R3", "C2"))
	f.write("p/te_test.go", l3cPTests)
	f.write("p/never_test.go", "//go:build never\n\npackage p\n\nimport \"testing\"\n\nfunc TestNever(t *testing.T) {}\n")
	f.write("q/te_test.go", l3cQTests)
	f.submit("te1", []string{"p/te_test.go", "p/never_test.go", "q/te_test.go"},
		l3cMap("R1", l3cTest("p/te_test.go", "TestPass")),
		l3cMap("R2", l3cTest("p/te_test.go", "TestPass"), l3cTest("q/te_test.go", "TestFail"),
			l3cTest("p/te_test.go", "TestSkip"), l3cTest("p/never_test.go", "TestNever")),
		l3cUncovered("R3", "needs a fault seam"))
	f.assign("te2", []string{"r/r_test.go"}, l3cReq("R4", "C2"))
	ok := f.run("ok")
	bad := f.run("bad")

	out, stderr, err := f.execute("task", "trace", "--task", f.dir, "--summary")
	if err != nil || cli.ExitCode(err) != 0 {
		t.Fatalf("cw task trace --summary: %v (exit %d), stderr %q; want exit 0 (R37)", err, cli.ExitCode(err), stderr)
	}
	got := l3cObject(t, "summary", out)
	if keys := l3cKeys(got); !reflect.DeepEqual(keys, []string{"criteria", "gaps", "problems", "task", "task_id"}) {
		t.Errorf("summary members %v, want exactly criteria, gaps, problems, task, task_id (R37)", keys)
	}
	want := map[string]any{
		"task":    f.dir,
		"task_id": f.id,
		"criteria": map[string]any{
			"C1": "proven", "C2": "open", "C3": "lost", "C4": "unmapped", "C5": "unmapped",
		},
		"gaps": []any{
			map[string]any{"criterion": "C2", "assignment_id": "te1", "requirement": "R2", "status": "unproven",
				"path": "q/te_test.go", "name": "TestFail", "test_status": "failed",
				"detail": fmt.Sprintf("run %s of check bad recorded a FAIL", bad.ID)},
			map[string]any{"criterion": "C2", "assignment_id": "te1", "requirement": "R2", "status": "unproven",
				"path": "p/te_test.go", "name": "TestSkip", "test_status": "skipped",
				"detail": fmt.Sprintf("run %s of check ok recorded only SKIP", ok.ID)},
			map[string]any{"criterion": "C2", "assignment_id": "te1", "requirement": "R2", "status": "unproven",
				"path": "p/never_test.go", "name": "TestNever", "test_status": "not_run"},
			map[string]any{"criterion": "C2", "assignment_id": "te1", "requirement": "R3", "status": "uncovered"},
			map[string]any{"criterion": "C2", "assignment_id": "te2", "requirement": "R4", "status": "pending"},
			map[string]any{"criterion": "C3", "assignment_id": "te0", "requirement": "R0", "status": "dropped", "detail": "closed unused"},
		},
		"problems": []any{},
	}
	for _, k := range []string{"task", "task_id", "criteria", "gaps", "problems"} {
		if !reflect.DeepEqual(got[k], want[k]) {
			g, _ := json.Marshal(got[k])
			w, _ := json.Marshal(want[k])
			t.Errorf("summary %s:\n got  %s\n want %s (R37)", k, g, w)
		}
	}

	// With a damaged results file, summary and report carry the same,
	// non-empty problems.
	var stored string
	for _, r := range f.load().Checks {
		if r.ID == ok.ID {
			stored = r.TestResults
		}
	}
	if stored == "" {
		t.Fatalf("run %s recorded no test_results (R15)", ok.ID)
	}
	if err := os.Remove(filepath.Join(f.dir, stored)); err != nil {
		t.Fatal(err)
	}
	wantProblems := []any{"check run " + ok.ID + " test results: stored file cannot be read or differs from its sha256"}
	out, _, err = f.execute("task", "trace", "--task", f.dir, "--summary")
	if err != nil {
		t.Fatalf("cw task trace --summary with a damaged run: %v (R37: same exit codes as the report)", err)
	}
	if sum := l3cObject(t, "summary with problems", out); !reflect.DeepEqual(sum["problems"], wantProblems) {
		t.Errorf("summary problems %v, want %v (R37)", sum["problems"], wantProblems)
	}
	full, _, err := f.execute("task", "trace", "--task", f.dir)
	if err != nil {
		t.Fatalf("cw task trace with a damaged run: %v", err)
	}
	if rep := l3cObject(t, "full report", full); !reflect.DeepEqual(rep["problems"], wantProblems) {
		t.Errorf("report problems %v, want %v, the same as the summary's (R37)", rep["problems"], wantProblems)
	}
}

// TestTaskRunSummaryFailingTestsFromResults fails when cw task run --summary
// takes failing_tests from the stored log instead of the run's results:
// distinct names of entries with fail > 0, in results order, at most 20, and
// no member when none failed (R45).
func TestTaskRunSummaryFailingTestsFromResults(t *testing.T) {
	t.Parallel()
	var src strings.Builder
	src.WriteString("package f\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\n")
	// Printed text that imitates a result line is output, not a result.
	src.WriteString("func TestPrintsFake(t *testing.T) { fmt.Println(\"--- FAIL: TestFake (0.00s)\") }\n\n")
	// Declared (and so run and logged) in reverse of results order.
	for i := 25; i >= 1; i-- {
		fmt.Fprintf(&src, "func TestF%02d(t *testing.T) { t.Fail() }\n\n", i)
	}
	src.WriteString("func TestFPass(t *testing.T) {}\n")
	f := l3cNew(t, []string{"C1"}, l3cCheck("ok", "go", "test", "-json", "./p/..."), l3cCheck("bad", "go", "test", "-json", "./f/..."))
	f.write("f/f_test.go", src.String())
	f.write("p/te_test.go", l3cPTests)

	out, _, err := f.execute("task", "run", "--task", f.dir, "--check", "bad", "--summary")
	if err == nil {
		t.Errorf("cw task run of a failing check returned no error")
	}
	sum := l3cObject(t, "failed run summary", out)
	if sum["status"] != "failed" {
		t.Fatalf("summary status %v, want failed", sum["status"])
	}
	var want []any
	for i := 1; i <= 20; i++ {
		want = append(want, fmt.Sprintf("TestF%02d", i))
	}
	if !reflect.DeepEqual(sum["failing_tests"], want) {
		g, _ := json.Marshal(sum["failing_tests"])
		w, _ := json.Marshal(want)
		t.Errorf("failing_tests %s, want %s: from the results, in results order, at most 20 (R45)", g, w)
	}

	out, _, err = f.execute("task", "run", "--task", f.dir, "--check", "ok", "--summary")
	if err != nil {
		t.Fatalf("cw task run ok: %v", err)
	}
	if sum := l3cObject(t, "passed run summary", out); sum["failing_tests"] != nil {
		t.Errorf("passed run summary has failing_tests %v, want the member omitted (R45)", sum["failing_tests"])
	}
}

// TestTaskRunSummaryFailingTestsOfTimedOutRun fails when a run stopped by its
// check's timeout does not list the failing tests of its results (R45). The
// prebuilt emitter prints the events before it sleeps, so nothing races the
// timeout; its output holds no --- FAIL: line, so the log names nothing.
func TestTaskRunSummaryFailingTestsOfTimedOutRun(t *testing.T) {
	t.Parallel()
	emit := l3cEmitter(t)
	f := l3cNew(t, []string{"C1"}, taskflow.Check{ID: "slow", TimeoutSeconds: 2, Argv: []string{emit,
		`out:{"Action":"pass","Package":"t","Test":"TestQuick"}`,
		`out:{"Action":"fail","Package":"t","Test":"TestSlow"}`,
		"sleep"}})
	out, _, err := f.execute("task", "run", "--task", f.dir, "--check", "slow", "--summary")
	if err == nil {
		t.Errorf("cw task run of a timed-out check returned no error")
	}
	sum := l3cObject(t, "timed-out run summary", out)
	if sum["status"] != "timed_out" {
		t.Fatalf("summary status %v, want timed_out", sum["status"])
	}
	if want := []any{"TestSlow"}; !reflect.DeepEqual(sum["failing_tests"], want) {
		t.Errorf("failing_tests %v, want %v from the results (R45)", sum["failing_tests"], want)
	}
}
