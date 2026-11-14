package taskflow_test

// Per-test results of check runs: R15, R16, R17, R38 (runs) and R45
// (taskflow.ReadTestResults).

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

func l3Result(pkg, test string, pass, fail, skip int) langpack.GoTestResult {
	return langpack.GoTestResult{Package: pkg, Test: test, Pass: pass, Fail: fail, Skip: skip}
}

// l3RunResult is l3Result as one real go test -json run stores it: the test
// ran once, so it has one run event (L4 R6).
func l3RunResult(pkg, test string, pass, fail, skip int) langpack.GoTestResult {
	result := l3Result(pkg, test, pass, fail, skip)
	result.Run = 1
	return result
}

// l3ResultsFile checks the R15 file contract for run and returns its decoded
// results: check-<run id>.tests.json, a private regular file in the task
// directory, hashing to test_results_sha256 and decoding strictly.
func (f *l3Fix) l3ResultsFile(run taskflow.CheckRun) l3Results {
	f.t.Helper()
	if want := "check-" + run.ID + ".tests.json"; run.TestResults != want {
		f.t.Fatalf("run %s test_results = %q, want %q (R15)", run.ID, run.TestResults, want)
	}
	name := filepath.Join(f.dir, run.TestResults)
	info, err := os.Lstat(name)
	if err != nil {
		f.t.Fatalf("results file: %v (R15)", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		f.t.Errorf("results file mode = %v, want a regular 0600 file (R15)", info.Mode())
	}
	data, err := os.ReadFile(name)
	if err != nil {
		f.t.Fatal(err)
	}
	if got := l3Sum(data); got != run.TestResultsSHA256 {
		f.t.Errorf("results file hashes to %s, test_results_sha256 = %s (R15)", got, run.TestResultsSHA256)
	}
	var res l3Results
	if err := taskflow.Decode(strings.NewReader(string(data)), &res); err != nil {
		f.t.Fatalf("results file does not decode strictly into GoTestResults: %v (R15)", err)
	}
	return res
}

// l3LogIntact checks that the stored log still matches the run's own hashes
// and size, so recording results changed none of them (R16).
func (f *l3Fix) l3LogIntact(run taskflow.CheckRun) {
	f.t.Helper()
	data, err := os.ReadFile(filepath.Join(f.dir, run.Log))
	if err != nil {
		f.t.Fatalf("log: %v", err)
	}
	if got := l3Sum(data); got != run.LogSHA256 {
		f.t.Errorf("log hashes to %s, log_sha256 = %s (R16)", got, run.LogSHA256)
	}
	if !run.Truncated {
		if run.OutputSHA256 != run.LogSHA256 || run.OutputBytes != int64(len(data)) {
			f.t.Errorf("untruncated run: output_sha256 %s / output_bytes %d, want the log's %s / %d (R16)",
				run.OutputSHA256, run.OutputBytes, run.LogSHA256, len(data))
		}
	}
}

// TestRunRecordsTestResultsOfPassedAndFailedRuns fails when a terminal go
// test -json run does not store its per-test results as R15 says, when
// recording them changes the run's status, failure or log (R16), or when
// ReadTestResults does not return them (R45).
func TestRunRecordsTestResultsOfPassedAndFailedRuns(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{
		files: map[string]string{"p/te_test.go": l3PTests, "p/never_test.go": l3NeverTests, "q/te_test.go": l3QTests},
		checks: []taskflow.Check{
			l3Check("ok", l3JSONP...),
			l3Check("bad", l3JSONQ...),
		},
	})
	p, q := l3Module+"/p", l3Module+"/q"

	ok := f.run("ok")
	if ok.Status != "passed" || ok.ExitCode == nil || *ok.ExitCode != 0 || ok.Failure != "" {
		t.Fatalf("ok run status %q failure %q, want passed with exit 0 (R16: results change no status)", ok.Status, ok.Failure)
	}
	want := l3Results{Tests: []langpack.GoTestResult{
		l3RunResult(p, "TestPass", 1, 0, 0),
		l3RunResult(p, "TestSkip", 0, 0, 1),
		l3RunResult(p, "TestSub", 1, 0, 0),
		l3RunResult(p, "TestSub/a", 1, 0, 0),
	}}
	if got := f.l3ResultsFile(ok); !reflect.DeepEqual(got, want) {
		t.Errorf("ok run results = %s, want %s (R15)", l3JSON(got), l3JSON(want))
	}
	if got, err := l3ReadResults(f.dir, ok); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadTestResults(ok) = %s, %v; want %s (R45)", l3JSON(got), err, l3JSON(want))
	}
	f.l3LogIntact(ok)

	bad := f.run("bad")
	if bad.Status != "failed" || bad.Failure != "command_failed" {
		t.Errorf("bad run status %q failure %q, want failed command_failed (R16)", bad.Status, bad.Failure)
	}
	wantBad := l3Results{Tests: []langpack.GoTestResult{
		l3RunResult(q, "TestFail", 0, 1, 0),
		l3RunResult(q, "TestQPass", 1, 0, 0),
	}}
	if got := f.l3ResultsFile(bad); !reflect.DeepEqual(got, wantBad) {
		t.Errorf("bad run results = %s, want %s (R15)", l3JSON(got), l3JSON(wantBad))
	}
	f.l3LogIntact(bad)
}

// TestRunWithoutTestEventsRecordsNoResults fails when a run whose output held
// no go test -json event gains test_results members (R16), or when a stored
// run without results carries the members at all (R38).
func TestRunWithoutTestEventsRecordsNoResults(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{
		files: map[string]string{"p/te_test.go": l3PTests},
		checks: []taskflow.Check{
			l3Check("plain", l3PlainP...),
			l3Check("verbose", "go", "test", "-v", "./p/..."),
			l3Check("vet", "go", "vet", "./p/..."),
		},
	})
	for _, check := range []string{"plain", "verbose", "vet"} {
		run := f.run(check)
		if run.Status != "passed" {
			t.Fatalf("%s run status %q, want passed", check, run.Status)
		}
		if run.TestResults != "" || run.TestResultsSHA256 != "" {
			t.Errorf("%s run recorded test_results %q / %q, want neither: its output has no -json event (R16)", check, run.TestResults, run.TestResultsSHA256)
		}
		f.l3LogIntact(run)
		stored := f.storedRun(run.ID)
		for _, m := range []string{"test_results", "test_results_sha256"} {
			if _, has := stored[m]; has {
				t.Errorf("stored %s run has member %q, want it omitted (R38)", check, m)
			}
		}
		if _, err := l3ReadResults(f.dir, run); err == nil {
			t.Errorf("ReadTestResults of a run without test_results succeeded, want an error (R45)")
		}
	}
}

// TestRunRecordsResultsOfCancelledRun fails when a running run already names
// results (R16) or when a cancelled run loses the results its output held
// before cancellation (R15: any terminal status).
func TestRunRecordsResultsOfCancelledRun(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	marker := filepath.Join(root, "hang-started")
	hang := fmt.Sprintf(`package h

import (
	"os"
	"testing"
	"time"
)

func TestQuick(t *testing.T) {}

func TestHang(t *testing.T) {
	if err := os.WriteFile(%q, []byte("started"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(time.Hour)
}
`, marker)
	f := l3New(t, l3Opts{
		files:  map[string]string{"h/h_test.go": hang},
		checks: []taskflow.Check{l3Check("hang", "go", "test", "-json", "./h/...")},
	})
	ctx, cancel := context.WithCancel(f.ctx)
	defer cancel()
	type outcome struct {
		run taskflow.CheckRun
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		run, err := taskflow.RunCheck(ctx, f.dir, "hang")
		done <- outcome{run, err}
	}()

	// Wait, with a deadline, until TestHang has started and the run's log
	// holds TestQuick's pass event, so cancelling cannot race that event.
	deadline := time.Now().Add(l3Wait)
	var running taskflow.CheckRun
	for {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("TestHang never started or its pass event never reached the log")
		}
		select {
		case o := <-done:
			t.Fatalf("run ended before cancellation: %q %v", o.run.Status, o.err)
		default:
		}
		if _, err := os.Stat(marker); err == nil {
			if rec, err := taskflow.Load(f.dir); err == nil && len(rec.Checks) > 0 {
				running = rec.Checks[len(rec.Checks)-1]
				log, _ := os.ReadFile(filepath.Join(f.dir, running.Log))
				if regexp.MustCompile(`"Action":"pass"[^\n]*"Test":"TestQuick"`).Match(log) {
					break
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if running.Status != "running" || running.TestResults != "" || running.TestResultsSHA256 != "" {
		t.Errorf("running run status %q test_results %q / %q, want running with neither member (R16)",
			running.Status, running.TestResults, running.TestResultsSHA256)
	}
	cancel()
	var o outcome
	select {
	case o = <-done:
	case <-time.After(l3Wait):
		t.Fatal("RunCheck did not return after cancellation")
	}
	run := f.recordedRun(running.ID)
	if run.Status != "cancelled" {
		t.Fatalf("run status %q (%v), want cancelled", run.Status, o.err)
	}
	res := f.l3ResultsFile(run)
	found := false
	for _, r := range res.Tests {
		if r.Package == l3Module+"/h" && r.Test == "TestQuick" {
			found = true
			if r.Pass != 1 || r.Fail != 0 || r.Skip != 0 {
				t.Errorf("TestQuick counts %d/%d/%d, want 1/0/0 (R15)", r.Pass, r.Fail, r.Skip)
			}
		}
	}
	if !found {
		t.Errorf("cancelled run results %s lack TestQuick (R15: every terminal status records results)", l3JSON(res))
	}
}

// TestRunRecordsResultsOfSourceChangedRun fails when a run whose test changed
// the source loses its results (R15: source_changed is terminal too).
func TestRunRecordsResultsOfSourceChangedRun(t *testing.T) {
	t.Parallel()
	touch := `package s

import (
	"os"
	"testing"
)

func TestTouch(t *testing.T) {
	if err := os.WriteFile("../touched.txt", []byte("changed by a check\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}
`
	f := l3New(t, l3Opts{
		files:  map[string]string{"s/s_test.go": touch},
		checks: []taskflow.Check{l3Check("touch", "go", "test", "-json", "./s/...")},
	})
	run := f.run("touch")
	if run.Status != "source_changed" {
		t.Fatalf("run status %q, want source_changed", run.Status)
	}
	want := l3Results{Tests: []langpack.GoTestResult{l3RunResult(l3Module+"/s", "TestTouch", 1, 0, 0)}}
	if got := f.l3ResultsFile(run); !reflect.DeepEqual(got, want) {
		t.Errorf("source_changed run results = %s, want %s (R15)", l3JSON(got), l3JSON(want))
	}
}

// TestRunRecordsResultsOfTimedOutRun fails when a run stopped by its check's
// own timeout loses the results its output held (R15: timed_out is terminal
// too; R16: the status stays timed_out). The prebuilt emitter prints both
// events before it sleeps, so nothing races the 2-second timeout.
func TestRunRecordsResultsOfTimedOutRun(t *testing.T) {
	t.Parallel()
	emit := l3Emitter(t)
	f := l3New(t, l3Opts{checks: []taskflow.Check{{ID: "slow", TimeoutSeconds: 2, Argv: []string{emit,
		`out:{"Action":"pass","Package":"t","Test":"TestQuick"}`,
		`out:{"Action":"fail","Package":"t","Test":"TestSlow"}`,
		"sleep"}}}})
	run := f.run("slow")
	if run.Status != "timed_out" {
		t.Fatalf("run status %q, want timed_out", run.Status)
	}
	want := l3Results{Tests: []langpack.GoTestResult{
		l3Result("t", "TestQuick", 1, 0, 0),
		l3Result("t", "TestSlow", 0, 1, 0),
	}}
	if got := f.l3ResultsFile(run); !reflect.DeepEqual(got, want) {
		t.Errorf("timed_out run results = %s, want %s (R15)", l3JSON(got), l3JSON(want))
	}
	if got, err := l3ReadResults(f.dir, run); err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadTestResults(timed_out) = %s, %v; want %s (R45)", l3JSON(got), err, l3JSON(want))
	}
}

// TestRunRecordsResultsFromStderr fails when only stdout feeds the results
// (R15: every byte the check wrote to stdout and stderr).
func TestRunRecordsResultsFromStderr(t *testing.T) {
	t.Parallel()
	emit := l3Emitter(t)
	f := l3New(t, l3Opts{checks: []taskflow.Check{l3Check("streams", emit,
		`out:{"Action":"pass","Package":"t","Test":"TestOut"}`,
		`err:{"Action":"fail","Package":"t","Test":"TestErr"}`,
		`out:{"Action":"skip","Package":"t","Test":"TestOut2"}`)}})
	run := f.run("streams")
	if run.Status != "passed" {
		t.Fatalf("run status %q, want passed", run.Status)
	}
	want := l3Results{Tests: []langpack.GoTestResult{
		l3Result("t", "TestErr", 0, 1, 0),
		l3Result("t", "TestOut", 1, 0, 0),
		l3Result("t", "TestOut2", 0, 0, 1),
	}}
	if got := f.l3ResultsFile(run); !reflect.DeepEqual(got, want) {
		t.Errorf("results = %s, want %s (R15: stderr events count)", l3JSON(got), l3JSON(want))
	}
}

// TestRunRecordsResultsBeyondLogLimit fails when results come only from the
// stored 128 KiB log instead of the complete output (R17).
func TestRunRecordsResultsBeyondLogLimit(t *testing.T) {
	t.Parallel()
	big := `package big

import (
	"fmt"
	"strings"
	"testing"
)

func TestBig(t *testing.T) {
	line := strings.Repeat("x", 100)
	for i := 0; i < 3000; i++ {
		fmt.Println(line)
	}
}

func TestAfter(t *testing.T) {}
`
	f := l3New(t, l3Opts{
		files:  map[string]string{"big/big_test.go": big},
		checks: []taskflow.Check{l3Check("big", "go", "test", "-json", "./big/...")},
	})
	run := f.run("big")
	if run.Status != "passed" || !run.Truncated || run.OutputBytes <= taskflow.MaxLogBytes {
		t.Fatalf("run status %q truncated %v output %d bytes, want a passed run past the %d-byte log", run.Status, run.Truncated, run.OutputBytes, taskflow.MaxLogBytes)
	}
	want := l3Results{Tests: []langpack.GoTestResult{
		l3RunResult(l3Module+"/big", "TestAfter", 1, 0, 0),
		l3RunResult(l3Module+"/big", "TestBig", 1, 0, 0),
	}}
	if got := f.l3ResultsFile(run); !reflect.DeepEqual(got, want) {
		t.Errorf("results = %s, want %s (R17: TestAfter is reported after the first 128 KiB)", l3JSON(got), l3JSON(want))
	}
	f.l3LogIntact(run)
}

// TestReadTestResultsChecksTheStoredFile fails when ReadTestResults returns
// results from a file that is not a private regular file in the task
// directory hashing to test_results_sha256 and decoding strictly (R45).
func TestReadTestResultsChecksTheStoredFile(t *testing.T) {
	t.Parallel()
	f := l3New(t, l3Opts{})
	valid := `{"tests":[{"package":"example.com/fix/p","test":"TestA","pass":2,"fail":1,"skip":0},{"package":"example.com/fix/p","test":"TestB","pass":0,"fail":0,"skip":3}],"truncated":true}`
	want := l3Results{Truncated: true, Tests: []langpack.GoTestResult{
		l3Result(l3Module+"/p", "TestA", 2, 1, 0),
		l3Result(l3Module+"/p", "TestB", 0, 0, 3),
	}}
	put := func(name, content string, mode os.FileMode) taskflow.CheckRun {
		t.Helper()
		full := filepath.Join(f.dir, name)
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(full, mode); err != nil {
			t.Fatal(err)
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "check-"), ".tests.json")
		return taskflow.CheckRun{ID: id, CheckID: "ok", Status: "passed", TestResults: name, TestResultsSHA256: l3Sum([]byte(content))}
	}

	good := put("check-good.tests.json", valid, 0o600)
	if got, err := l3ReadResults(f.dir, good); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadTestResults(valid) = %s, %v; want %s", l3JSON(got), err, l3JSON(want))
	}

	cases := map[string]taskflow.CheckRun{}
	cases["no test_results"] = taskflow.CheckRun{ID: "none", CheckID: "ok", Status: "passed"}
	r := good
	r.TestResultsSHA256 = l3Sum([]byte(valid + " "))
	cases["hash differs"] = r
	cases["missing file"] = taskflow.CheckRun{ID: "gone", CheckID: "ok", Status: "passed", TestResults: "check-gone.tests.json", TestResultsSHA256: l3Sum([]byte(valid))}
	cases["group readable"] = put("check-open.tests.json", valid, 0o644)
	cases["unknown member"] = put("check-extra.tests.json", strings.TrimSuffix(valid, "}")+`,"extra":1}`, 0o600)
	cases["not json"] = put("check-text.tests.json", "PASS\n", 0o600)
	if err := os.Symlink(filepath.Join(f.dir, good.TestResults), filepath.Join(f.dir, "check-link.tests.json")); err != nil {
		t.Fatal(err)
	}
	cases["symlink"] = taskflow.CheckRun{ID: "link", CheckID: "ok", Status: "passed", TestResults: "check-link.tests.json", TestResultsSHA256: good.TestResultsSHA256}
	outside := filepath.Join(f.root, "check-out.tests.json")
	if err := os.WriteFile(outside, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(f.dir, outside)
	if err != nil {
		t.Fatal(err)
	}
	cases["outside the task directory"] = taskflow.CheckRun{ID: "out", CheckID: "ok", Status: "passed", TestResults: filepath.ToSlash(rel), TestResultsSHA256: l3Sum([]byte(valid))}
	if err := os.Mkdir(filepath.Join(f.dir, "check-dir.tests.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases["directory"] = taskflow.CheckRun{ID: "dir", CheckID: "ok", Status: "passed", TestResults: "check-dir.tests.json", TestResultsSHA256: l3Sum(nil)}

	for name, run := range cases {
		if got, err := l3ReadResults(f.dir, run); err == nil {
			t.Errorf("ReadTestResults(%s) = %s, nil; want an error (R45)", name, l3JSON(got))
		}
	}
}
