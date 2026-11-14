package langpack_test

// Go test output with run and marked counts, run limits and declaring
// packages (interface contract L4, third version): R4, R5 (R5.a-R5.c), R22
// and the GoTestDeclaringPackages rule of R7.a. Expected values are derived
// by hand from the contract, or computed independently in the test.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// l4R is an expected ResultsWithRuns entry.
func l4R(pkg, test string, pass, fail, skip, run, marked int) langpack.GoTestResult {
	return langpack.GoTestResult{Package: pkg, Test: test, Pass: pass, Fail: fail, Skip: skip, Run: run, Marked: marked}
}

// l4Out writes chunks to a fresh GoTestOutput and returns it; a panic of an
// unimplemented stub becomes a test failure.
func l4Out(t *testing.T, chunks ...[]byte) *langpack.GoTestOutput {
	t.Helper()
	o := &langpack.GoTestOutput{}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GoTestOutput.Write panicked: %v", r)
		}
	}()
	for _, c := range chunks {
		n, err := o.Write(c)
		if n != len(c) || err != nil {
			t.Fatalf("Write(%d bytes) = %d, %v; want %d, nil", len(c), n, err, len(c))
		}
	}
	return o
}

func l4WithRuns(t *testing.T, o *langpack.GoTestOutput) langpack.GoTestResults {
	t.Helper()
	return l3Safe(t, o.ResultsWithRuns)
}

func l4Results(t *testing.T, o *langpack.GoTestOutput) langpack.GoTestResults {
	t.Helper()
	return l3Safe(t, o.Results)
}

// l4Marked is an output event whose Output is out.
func l4Output(pkg, test, out string) string {
	return l3Ev("Time", "2026-11-08T10:00:00.000000Z", "Action", "output", "Package", pkg, "Test", test, "Output", out)
}

const l4P = "example.com/m/p"

// TestResultsWithRunsCountsEvents fails when ResultsWithRuns does not count a
// run event or a marked output event for its (package, test), counts a line
// R5.a excludes, reports a key that has no entry in Results, or changes the
// pass, fail and skip counts or order of Results (R5, R5.a).
func TestResultsWithRunsCountsEvents(t *testing.T) {
	t.Parallel()
	pass := l3Event("pass", l4P, "TestA")
	cases := []struct {
		name  string
		lines []string
		want  langpack.GoTestResults
	}{
		{"run then pass", []string{l3Event("run", l4P, "TestA"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0))},
		{"pass then run (counted wherever it occurs)", []string{pass, l3Event("run", l4P, "TestA")}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0))},
		{"two runs, two results", []string{l3Event("run", l4P, "TestA"), pass, l3Event("run", l4P, "TestA"), l3Event("fail", l4P, "TestA")},
			l3Want(l4R(l4P, "TestA", 1, 1, 0, 2, 0))},
		{"forged pass without run", []string{pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"skip with run", []string{l3Event("run", l4P, "TestA"), l3Event("skip", l4P, "TestA")}, l3Want(l4R(l4P, "TestA", 0, 0, 1, 1, 0))},
		{"run without Package member", []string{l3Ev("Action", "run", "Test", "TestA"), l3Ev("Action", "pass", "Test", "TestA")},
			l3Want(l4R("", "TestA", 1, 0, 0, 1, 0))},
		{"run of another package is another key", []string{l3Event("run", "example.com/m/q", "TestA"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"run of a key without results is not reported", []string{l3Event("run", l4P, "TestB"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"only run events: no entry", []string{l3Event("run", l4P, "TestA")}, l3Want()},
		{"marked output", []string{l3Event("run", l4P, "TestA"), l4Output(l4P, "TestA", "\u0016marked\n"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 1))},
		{"marked output after the result", []string{l3Event("run", l4P, "TestA"), pass, l4Output(l4P, "TestA", "\u0016x"), l4Output(l4P, "TestA", "\u0016")},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 2))},
		{"marked output escaped as \\u0016 in the line", []string{`{"Action":"output","Package":"p","Test":"TestA","Output":"\u0016y"}`, `{"Action":"pass","Package":"p","Test":"TestA"}`},
			l3Want(l4R("p", "TestA", 1, 0, 0, 0, 1))},
		{"output whose 0x16 is not the first byte", []string{l4Output(l4P, "TestA", " \u0016x"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"output beginning with ESC", []string{l4Output(l4P, "TestA", "\u001b\u0016x"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"output without Output", []string{l3Event("output", l4P, "TestA"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"Output not a string", []string{`{"Action":"output","Package":"p","Test":"TestA","Output":22}`, `{"Action":"pass","Package":"p","Test":"TestA"}`},
			l3Want(l4R("p", "TestA", 1, 0, 0, 0, 0))},
		{"Output duplicated", []string{`{"Action":"output","Package":"p","Test":"TestA","Output":"\u0016a","Output":"\u0016b"}`, `{"Action":"pass","Package":"p","Test":"TestA"}`},
			l3Want(l4R("p", "TestA", 1, 0, 0, 0, 0))},
		{"marked Output on a run event counts only the run", []string{l3Ev("Action", "run", "Package", l4P, "Test", "TestA", "Output", "\u0016x"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0))},
		{"marked Output on a pass event is not marked", []string{l3Ev("Action", "pass", "Package", l4P, "Test", "TestA", "Output", "\u0016x")},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"marked output without Test", []string{l3Ev("Action", "output", "Package", l4P, "Output", "\u0016x"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"marked output with empty Test", []string{l4Output(l4P, "", "\u0016x"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"run with empty Test", []string{l3Event("run", l4P, ""), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"uppercase RUN", []string{l3Event("RUN", l4P, "TestA"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"uppercase OUTPUT", []string{l3Ev("Action", "OUTPUT", "Package", l4P, "Test", "TestA", "Output", "\u0016x"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"run with duplicate Action", []string{`{"Action":"pass","Action":"run","Package":"p","Test":"TestA"}`, `{"Action":"pass","Package":"p","Test":"TestA"}`},
			l3Want(l4R("p", "TestA", 1, 0, 0, 0, 0))},
		{"run with a control character in Test", []string{`{"Action":"run","Package":"p","Test":"TestA\u0007"}`, `{"Action":"pass","Package":"p","Test":"TestA"}`},
			l3Want(l4R("p", "TestA", 1, 0, 0, 0, 0))},
		{"run with text after the object", []string{l3Event("run", l4P, "TestA") + "x", pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"run with leading space", []string{" " + l3Event("run", l4P, "TestA"), pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"run with trailing whitespace and CR", []string{l3Event("run", l4P, "TestA") + " \r", pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0))},
		{"pause, cont, bench and start count nothing", []string{l3Event("pause", l4P, "TestA"), l3Event("cont", l4P, "TestA"), l3Event("bench", l4P, "TestA"), l3Event("start", l4P, "TestA"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"verbose framing text counts nothing", []string{"=== RUN   TestA", "\x16=== RUN   TestA", pass}, l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0))},
		{"subtest keys are their own", []string{l3Event("run", l4P, "TestA"), l3Event("run", l4P, "TestA/b"), l3Event("pass", l4P, "TestA/b"), pass},
			l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0), l4R(l4P, "TestA/b", 1, 0, 0, 1, 0))},
		{"order is Results' order", []string{l3Event("run", "z", "TestA"), l3Event("pass", "z", "TestA"), l3Event("run", "a", "TestB"), l3Event("pass", "a", "TestB"), l3Event("pass", "a", "TestA")},
			l3Want(l4R("a", "TestA", 1, 0, 0, 0, 0), l4R("a", "TestB", 1, 0, 0, 1, 0), l4R("z", "TestA", 1, 0, 0, 1, 0))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			o := l4Out(t, l3Lines(c.lines...))
			l3Same(t, fmt.Sprintf("ResultsWithRuns of %q", c.lines), l4WithRuns(t, o), c.want)
		})
	}
}

// TestResultsUnchangedByRunEvents fails when Results reports a Run or Marked
// count, or when its JSON encoding holds a run or marked member, for output
// full of run and marked events (R4, I2).
func TestResultsUnchangedByRunEvents(t *testing.T) {
	t.Parallel()
	o := l4Out(t, l3Lines(
		l3Event("run", l4P, "TestA"), l4Output(l4P, "TestA", "\u0016x"), l3Event("pass", l4P, "TestA"),
		l3Event("run", l4P, "TestB"), l3Event("run", l4P, "TestB"), l3Event("fail", l4P, "TestB"), l3Event("skip", l4P, "TestB"),
	))
	got := l4Results(t, o)
	l3Same(t, "Results", got, l3Want(l3R(l4P, "TestA", 1, 0, 0), l3R(l4P, "TestB", 0, 1, 1)))
	data, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(`"run"`)) || bytes.Contains(data, []byte(`"marked"`)) {
		t.Errorf("Results JSON holds a run or marked member (R4): %s", data)
	}
	// The same output's ResultsWithRuns carries both counts, and its JSON
	// names them; zero counts stay omitted.
	with := l4WithRuns(t, o)
	l3Same(t, "ResultsWithRuns", with, l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 1), l4R(l4P, "TestB", 0, 1, 1, 2, 0)))
	data, err = json.Marshal(with)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"tests":[{"package":"example.com/m/p","test":"TestA","pass":1,"fail":0,"skip":0,"run":1,"marked":1},` +
		`{"package":"example.com/m/p","test":"TestB","pass":0,"fail":1,"skip":1,"run":2}],"truncated":false}`
	if string(data) != want {
		t.Errorf("ResultsWithRuns JSON:\n got  %s\n want %s (R5, R6: run and marked omitted when 0)", data, want)
	}
	// Results after ResultsWithRuns is still unchanged.
	l3Same(t, "Results after ResultsWithRuns", l4Results(t, o), got)
}

// TestResultsWithRunsSplitInvariance fails when writing the same bytes split
// at any boundary, or calling Results and ResultsWithRuns between writes,
// changes ResultsWithRuns (R5.c).
func TestResultsWithRunsSplitInvariance(t *testing.T) {
	t.Parallel()
	corpus := l3Lines(
		l3Event("run", l4P, "TestA"),
		"noise line",
		l4Output(l4P, "TestA", "\u0016x\n"),
		l3Event("pass", l4P, "TestA"),
		l3Event("run", "q", "TestB")+"\r",
		l3Event("fail", "q", "TestB"),
		l4Output("q", "TestB", "plain"),
		l3Event("run", l4P, "TestA"),
	)
	corpus = append(corpus, []byte(l4Output("q", "TestB", "\u0016tail"))...) // unterminated final line
	want := l3Want(l4R(l4P, "TestA", 1, 0, 0, 2, 1), l4R("q", "TestB", 0, 1, 0, 1, 1))
	l3Same(t, "one write", l4WithRuns(t, l4Out(t, corpus)), want)
	for cut := 0; cut <= len(corpus); cut++ {
		o := l4Out(t, corpus[:cut])
		_ = l4Results(t, o)
		_ = l4WithRuns(t, o)
		if n, err := o.Write(corpus[cut:]); n != len(corpus)-cut || err != nil {
			t.Fatalf("Write = %d, %v", n, err)
		}
		if got := l4WithRuns(t, o); !reflect.DeepEqual(got, want) {
			l3Same(t, fmt.Sprintf("cut at %d with reads between", cut), got, want)
			return
		}
	}
	var o langpack.GoTestOutput
	for i := range corpus {
		if _, err := o.Write(corpus[i : i+1]); err != nil {
			t.Fatal(err)
		}
		if i%7 == 0 {
			_ = l3Safe(t, o.ResultsWithRuns)
		}
	}
	l3Same(t, "byte-by-byte", l4WithRuns(t, &o), want)
}

// TestResultsWithRunsUnterminatedLine fails when the bytes after the last
// newline are not read as a final line for the returned value, or when that
// read is kept so that a later write continuing the line counts it twice or
// counts a line that the continuation made invalid (R5.c, L3 R20-R21).
func TestResultsWithRunsUnterminatedLine(t *testing.T) {
	t.Parallel()
	pass := []byte(l3Event("pass", l4P, "TestA") + "\n")
	run := []byte(l3Event("run", l4P, "TestA"))
	o := l4Out(t, pass, run)
	l3Same(t, "unterminated run event", l4WithRuns(t, o), l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0)))
	if _, err := o.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	l3Same(t, "after its newline", l4WithRuns(t, o), l3Want(l4R(l4P, "TestA", 1, 0, 0, 1, 0)))

	o = l4Out(t, pass, run)
	_ = l4WithRuns(t, o)
	if _, err := o.Write([]byte("x\n")); err != nil {
		t.Fatal(err)
	}
	l3Same(t, "continued into an invalid line", l4WithRuns(t, o), l3Want(l4R(l4P, "TestA", 1, 0, 0, 0, 0)))
}

// l4Key is a (package, test) key of the independent model below.
type l4Key struct{ pkg, test string }

// TestResultsWithRunsMatchesModel fails when, for random sequences of
// events, ResultsWithRuns differs from an independent count: entries are the
// keys with a pass, fail or skip event, sorted by package then test, with Run
// the number of run events and Marked the number of output events whose
// Output begins with 0x16 (R5, R5.a; a property over generated inputs).
func TestResultsWithRunsMatchesModel(t *testing.T) {
	t.Parallel()
	keys := []l4Key{{"a", "TestA"}, {"a", "TestA/x"}, {"b", "TestA"}, {"", "TestC"}}
	rng := rand.New(rand.NewPCG(4, 22))
	for round := 0; round < 300; round++ {
		type counts struct{ pass, fail, skip, run, marked int }
		model := map[l4Key]*counts{}
		var lines []string
		for i := rng.IntN(40); i > 0; i-- {
			k := keys[rng.IntN(len(keys))]
			c := model[k]
			if c == nil {
				c = &counts{}
				model[k] = c
			}
			base := []any{"Action", "", "Test", k.test}
			if k.pkg != "" {
				base = append(base, "Package", k.pkg)
			}
			ev := func(action string, extra ...any) string {
				m := append([]any{}, base...)
				m[1] = action
				return l3Ev(append(m, extra...)...)
			}
			switch rng.IntN(9) {
			case 0:
				lines = append(lines, ev("pass"))
				c.pass++
			case 1:
				lines = append(lines, ev("fail"))
				c.fail++
			case 2:
				lines = append(lines, ev("skip"))
				c.skip++
			case 3, 4:
				lines = append(lines, ev("run"))
				c.run++
			case 5:
				lines = append(lines, ev("output", "Output", "\u0016--- PASS"))
				c.marked++
			case 6:
				lines = append(lines, ev("output", "Output", "    log line\n"))
			case 7:
				lines = append(lines, ev("output"))
			default:
				lines = append(lines, ev("pause"))
			}
		}
		var want []langpack.GoTestResult
		for k, c := range model {
			if c.pass+c.fail+c.skip > 0 {
				want = append(want, l4R(k.pkg, k.test, c.pass, c.fail, c.skip, c.run, c.marked))
			}
		}
		sort.Slice(want, func(i, j int) bool {
			if want[i].Package != want[j].Package {
				return want[i].Package < want[j].Package
			}
			return want[i].Test < want[j].Test
		})
		var input []byte
		if len(lines) > 0 {
			input = l3Lines(lines...)
		}
		got := l4WithRuns(t, l4Out(t, input))
		if !reflect.DeepEqual(got, l3Want(want...)) {
			l3Same(t, fmt.Sprintf("round %d input %q", round, lines), got, l3Want(want...))
			return
		}
	}
}

// FuzzResultsWithRunsAgreesWithResults checks I2 and R5.c over arbitrary
// bytes: ResultsWithRuns has Results' entries, in order, differing only in
// Run and Marked; Results has no Run or Marked; Truncated of Results implies
// Truncated of ResultsWithRuns; and a split write gives the same value.
func FuzzResultsWithRunsAgreesWithResults(f *testing.F) {
	f.Add(l3Lines(l3Event("run", l4P, "TestA"), l3Event("pass", l4P, "TestA")), 10)
	f.Add(l3Lines(l4Output(l4P, "TestA", "\u0016x"), l3Event("fail", l4P, "TestA"), "junk"), 3)
	f.Add([]byte(`{"Action":"run","Test":"T"}`+"\n"+`{"Action":"skip","Test":"T"}`), 0)
	f.Fuzz(func(t *testing.T, data []byte, cut int) {
		o := l4Out(t, data)
		r := l4Results(t, o)
		w := l4WithRuns(t, o)
		if len(r.Tests) != len(w.Tests) {
			t.Fatalf("Results has %d entries, ResultsWithRuns %d (I2)", len(r.Tests), len(w.Tests))
		}
		for i := range r.Tests {
			if r.Tests[i].Run != 0 || r.Tests[i].Marked != 0 {
				t.Fatalf("Results entry %+v has run or marked counts (R4)", r.Tests[i])
			}
			x := w.Tests[i]
			if x.Run < 0 || x.Marked < 0 {
				t.Fatalf("negative counts in %+v", x)
			}
			x.Run, x.Marked = 0, 0
			if x != r.Tests[i] {
				t.Fatalf("entry %d: ResultsWithRuns %+v, Results %+v (I2)", i, w.Tests[i], r.Tests[i])
			}
		}
		if r.Truncated && !w.Truncated {
			t.Fatalf("Results truncated but ResultsWithRuns not (R5)")
		}
		if len(data) == 0 {
			return
		}
		if cut < 0 {
			cut = -cut
		}
		cut %= len(data) + 1
		o2 := l4Out(t, data[:cut])
		_ = l4WithRuns(t, o2)
		if _, err := o2.Write(data[cut:]); err != nil {
			t.Fatal(err)
		}
		if got := l4WithRuns(t, o2); !reflect.DeepEqual(got, w) {
			t.Fatalf("split at %d: %+v, want %+v (R5.c)", cut, got, w)
		}
	})
}

// TestResultsWithRunsCountBound fails when run and marked counts are not
// kept for at most MaxGoTestResults keys, separately from Results' bound:
// the key past the bound must read Run 0 and set Truncated of
// ResultsWithRuns only, while kept keys keep counting (R5.b).
func TestResultsWithRunsCountBound(t *testing.T) {
	t.Parallel()
	// 49,999 keys with only a run event (no results), then key "00000"
	// with a run and a pass: exactly MaxGoTestResults keys are kept. Each
	// key costs at most 2*(0+6)+128 = 140 bytes, so 50,000 keys stay below
	// the byte bound.
	var buf bytes.Buffer
	for i := 1; i < langpack.MaxGoTestResults; i++ {
		fmt.Fprintf(&buf, `{"Action":"run","Test":"r%04d"}`+"\n", i)
	}
	fmt.Fprintf(&buf, `{"Action":"run","Test":"00000"}`+"\n"+`{"Action":"pass","Test":"00000"}`+"\n")
	at := l4WithRuns(t, l4Out(t, buf.Bytes()))
	l3Same(t, "exactly MaxGoTestResults keys", at, l3Want(l4R("", "00000", 1, 0, 0, 1, 0)))

	// One more key with a run, a marked output and a pass: its counts are
	// refused, its results are not, and the first key keeps counting.
	more := []byte(`{"Action":"run","Test":"00001"}` + "\n" + `{"Action":"output","Test":"00001","Output":"\u0016x"}` + "\n" +
		`{"Action":"pass","Test":"00001"}` + "\n" + `{"Action":"run","Test":"00000"}` + "\n" +
		`{"Action":"output","Test":"00000","Output":"\u0016y"}` + "\n")
	o := l4Out(t, buf.Bytes(), more)
	want := l3Want(l4R("", "00000", 1, 0, 0, 2, 1), l4R("", "00001", 1, 0, 0, 0, 0))
	want.Truncated = true
	l3Same(t, "one key past the bound", l4WithRuns(t, o), want)
	if r := l4Results(t, o); r.Truncated || len(r.Tests) != 2 {
		t.Errorf("Results = %+v; its own bound was not hit, so it must not be truncated (R5.b keeps the bounds separate)", r)
	}
}

// TestResultsWithRunsByteBound fails when the byte bound on kept keys is off
// by one, omits the factor 2 or the 128-byte overhead, or is not decided per
// new key (R5.b).
func TestResultsWithRunsByteBound(t *testing.T) {
	t.Parallel()
	// Package "p" and 4095-byte names cost 2*(1+4095)+128 = 8320 bytes:
	// 882 run-only keys use 7,338,240 and leave 1,792 = 2*(1+831)+128.
	name := func(i, n int) string { return fmt.Sprintf("T%05d", i) + strings.Repeat("x", n-6) }
	var buf bytes.Buffer
	for i := 0; i < 882; i++ {
		buf.WriteString(l3Ev("Action", "run", "Package", "p", "Test", name(i, 4095)) + "\n")
	}
	base := buf.Bytes()
	runPass := func(test string) []byte {
		return []byte(l3Ev("Action", "run", "Package", "p", "Test", test) + "\n" + l3Ev("Action", "pass", "Package", "p", "Test", test) + "\n")
	}
	fits := name(882, 831)
	got := l4WithRuns(t, l4Out(t, base, runPass(fits)))
	l3Same(t, "keys summing to exactly 7 MiB", got, l3Want(l4R("p", fits, 1, 0, 0, 1, 0)))

	over := name(882, 832)
	want := l3Want(l4R("p", over, 1, 0, 0, 0, 0))
	want.Truncated = true
	o := l4Out(t, base, runPass(over))
	l3Same(t, "a key 2 bytes past 7 MiB", l4WithRuns(t, o), want)
	if r := l4Results(t, o); r.Truncated {
		t.Errorf("Results truncated although its own bound was not hit (R5.b)")
	}

	// After the 1794-byte key is refused, "U" (2*(1+1)+128 = 132 bytes)
	// still fits in the 1,792 left.
	later := l4WithRuns(t, l4Out(t, base, runPass(over), runPass("U")))
	want = l3Want(l4R("p", over, 1, 0, 0, 0, 0), l4R("p", "U", 1, 0, 0, 1, 0))
	want.Truncated = true
	l3Same(t, "a smaller key after a refused one", later, want)
}

// TestResultsWithRunsKeepsResultsTruncation fails when Truncated of
// ResultsWithRuns is not true once Results is truncated (R5).
func TestResultsWithRunsKeepsResultsTruncation(t *testing.T) {
	t.Parallel()
	name := func(i int) string { return fmt.Sprintf("T%05d", i) + strings.Repeat("x", 4089) }
	var buf bytes.Buffer
	for i := 0; i < 883; i++ {
		buf.WriteString(l3Ev("Action", "pass", "Package", "p", "Test", name(i)) + "\n")
	}
	o := l4Out(t, buf.Bytes())
	if r := l4Results(t, o); !r.Truncated {
		t.Fatalf("fixture: 883 pass keys of 8320 bytes did not truncate Results")
	}
	if w := l4WithRuns(t, o); !w.Truncated || len(w.Tests) != 882 {
		t.Errorf("ResultsWithRuns: %d entries, truncated %v; want 882, true (R5: Results().Truncated)", len(w.Tests), w.Truncated)
	}
}

// TestGoTestRunLimit fails when the run limit differs from R22's examples or
// its count, cpu, GOFLAGS, saturation and credit rules.
func TestGoTestRunLimit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv, goflags string
		want          int
	}{
		// R22's examples.
		{"go test -json ./...", "", 1},
		{"go test -json -count=3 ./...", "", 3},
		{"go test -json -count 2 -cpu=1,2,4 ./...", "", 6},
		{"go test -json -test.count=4 ./...", "", 4},
		{"go test -json -count=1 -count=3 ./...", "", 3},
		{"go test -json -cpu=1,,2 ./...", "", 2},
		{"go test -json ./...", "-count=5", 5},
		{"go test -json -count=2 ./...", "-count=5", 2},
		{"go test -json -count=010 ./...", "", 8},
		{"go test -json -count=+3 ./...", "", 3},
		{"go test -json -count=+010 ./...", "", 8},
		{"go test -json -count=08 ./...", "", 0},
		{"go test ./...", "", 0},
		// Derived: uncredited runs are 0 whatever their count.
		{"go test -json -run X -count=3 ./...", "", 0},
		{"go test -json -count=0x10 ./...", "", 0},
		{"go test -json -count=1_0 ./...", "", 0},
		{"go test -json -count=0 ./...", "", 0},
		{"go test -json -count=-1 ./...", "", 0},
		{"go test -count=3 ./...", "", 0},
		// Derived: the last count flag of GOFLAGS then argv wins.
		{"go test -json -count=08 -count=3 ./...", "", 3},
		{"go test -json -count=3 -count=08 ./...", "", 0},
		{"go test -json ./...", "-count=5 -count=2", 2},
		{"go test -json -count=3 ./...", "-cpu=1,2", 6},
		{"go test -json -cpu=1,2,4 ./...", "-cpu=1,2", 3},
		{"go test -json -cpu=4 -count=2 ./...", "", 2},
		{"go test -json -count=2 -cpu=1,2,4,8 ./...", "", 8},
		// Derived: saturation at the largest int.
		{"go test -json -count=9223372036854775807 -cpu=1,2 ./...", "", math.MaxInt},
		{"go test -json -count=4611686018427387904 -cpu=1,2 ./...", "", math.MaxInt},
		{"go test -json -count=4611686018427387903 -cpu=1,2 ./...", "", 9223372036854775806},
		{"go test -json -count=9223372036854775807 ./...", "", math.MaxInt},
	}
	for _, c := range cases {
		run := langpack.Run{Argv: strings.Fields(c.argv), GOFLAGS: c.goflags}
		if got := l3Safe(t, func() int { return langpack.GoTestRunLimit(run) }); got != c.want {
			t.Errorf("GoTestRunLimit(%q, GOFLAGS %q) = %d, want %d (R22)", c.argv, c.goflags, got, c.want)
		}
	}
}

// TestGoTestRunLimitDerivedCPUForms fails when cpus is not the number of
// non-empty, space-trimmed comma fields of the last cpu flag (1 when none),
// or when a test. prefix on cpu is not removed (R22). Each expectation
// applies only when the L3 credit rule credits the run; otherwise the limit
// is 0.
func TestGoTestRunLimitDerivedCPUForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv []string
		want int
	}{
		{[]string{"go", "test", "-json", "-cpu=1, ,2", "./..."}, 2},
		{[]string{"go", "test", "-json", "-cpu= 1 , 2 ,3 ", "./..."}, 3},
		{[]string{"go", "test", "-json", "-cpu=,,,", "./..."}, 1},
		{[]string{"go", "test", "-json", "-cpu=", "./..."}, 1},
		{[]string{"go", "test", "-json", "-test.cpu=1,2", "-count=3", "./..."}, 6},
		{[]string{"go", "test", "-json", "-cpu=1,2", "-cpu=8", "./..."}, 1},
		{[]string{"go", "test", "-json", "-cpu", "1,2", "./..."}, 2},
		{[]string{"go", "test", "-json", "-test.count", "4", "./..."}, 4},
	}
	for _, c := range cases {
		run := langpack.Run{Argv: c.argv}
		want := c.want
		if !l3Safe(t, func() bool { return langpack.GoTestsCredited(run) }) {
			want = 0
		}
		if got := l3Safe(t, func() int { return langpack.GoTestRunLimit(run) }); got != want {
			t.Errorf("GoTestRunLimit(%q) = %d, want %d (R22)", c.argv, got, want)
		}
	}
}

// TestGoTestRunLimitZeroWithoutCredit fails when any uncredited run has a
// non-zero limit, or a credited run a limit below 1 other than for an
// unparsable count (R22; a property over generated flag sequences).
func TestGoTestRunLimitZeroWithoutCredit(t *testing.T) {
	t.Parallel()
	flags := []string{"-json", "-count=2", "-count=0", "-count=08", "-cpu=1,2", "-run=X", "-v", "-race", "-count", "3", "-args", "-short", "-cpu=", "-test.count=2"}
	rng := rand.New(rand.NewPCG(22, 4))
	for i := 0; i < 2000; i++ {
		argv := []string{"go", "test"}
		for n := rng.IntN(5); n > 0; n-- {
			argv = append(argv, flags[rng.IntN(len(flags))])
		}
		argv = append(argv, "./...")
		run := langpack.Run{Argv: argv}
		credited := l3Safe(t, func() bool { return langpack.GoTestsCredited(run) })
		got := l3Safe(t, func() int { return langpack.GoTestRunLimit(run) })
		if !credited && got != 0 {
			t.Fatalf("GoTestRunLimit(%q) = %d for an uncredited run, want 0 (R22)", argv, got)
		}
		if got < 0 {
			t.Fatalf("GoTestRunLimit(%q) = %d, negative", argv, got)
		}
		if credited && got == 0 && !strings.Contains(strings.Join(argv, " "), "08") {
			t.Fatalf("GoTestRunLimit(%q) = 0 for a credited run whose count parses (R22)", argv)
		}
	}
}

// TestGoTestDeclaringPackages fails when the declaration count differs from
// R7.a: distinct package clauses among the sources that parse and declare a
// top-level function without a receiver named the part of name before its
// first "/".
func TestGoTestDeclaringPackages(t *testing.T) {
	t.Parallel()
	src := func(pkg, body string) []byte {
		return []byte("package " + pkg + "\n\nimport \"testing\"\n\n" + body + "\n")
	}
	internal := src("p", "func TestX(t *testing.T) {}")
	external := src("p_test", "func TestX(t *testing.T) {}")
	constrained := []byte("//go:build never\n\npackage p_test\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) {}\n")
	other := src("p", "func TestY(t *testing.T) {}")
	method := src("p_test", "type s struct{}\n\nfunc (s) TestX(t *testing.T) {}")
	variable := src("p_test", "var TestX = func(t *testing.T) {}")
	commented := src("p_test", "// func TestX(t *testing.T) {}")
	broken := []byte("package p_test\n\nfunc TestX(t *testing.T) {\n")
	nested := src("p_test", "func helper() {\n\tTestX := func() {}\n\t_ = TestX\n}")
	example := src("p", "func ExampleF() {}")
	exampleExt := src("p_test", "func ExampleF() {}")
	cases := []struct {
		name    string
		sources [][]byte
		test    string
		want    int
	}{
		{"none", nil, "TestX", 0},
		{"internal only", [][]byte{internal}, "TestX", 1},
		{"internal and external", [][]byte{internal, external}, "TestX", 2},
		{"same package twice", [][]byte{internal, src("p", "func TestX(t *testing.T) {}")}, "TestX", 1},
		{"three packages", [][]byte{internal, external, src("q", "func TestX(t *testing.T) {}")}, "TestX", 3},
		{"build constraints are not evaluated", [][]byte{internal, constrained}, "TestX", 2},
		{"subtest name uses the part before /", [][]byte{internal, external}, "TestX/case/deeper", 2},
		{"other name", [][]byte{other}, "TestX", 0},
		{"a method does not count", [][]byte{internal, method}, "TestX", 1},
		{"a variable does not count", [][]byte{internal, variable}, "TestX", 1},
		{"a comment does not count", [][]byte{internal, commented}, "TestX", 1},
		{"a local name does not count", [][]byte{nested}, "TestX", 0},
		{"a source that does not parse counts nothing", [][]byte{internal, broken}, "TestX", 1},
		{"only unparsable sources", [][]byte{broken}, "TestX", 0},
		{"empty source", [][]byte{{}}, "TestX", 0},
		{"examples", [][]byte{example, exampleExt, other}, "ExampleF", 2},
		{"name is not a prefix match", [][]byte{src("p", "func TestXY(t *testing.T) {}")}, "TestX", 0},
	}
	for _, c := range cases {
		if got := l3Safe(t, func() int { return langpack.GoTestDeclaringPackages(c.sources, c.test) }); got != c.want {
			t.Errorf("%s: GoTestDeclaringPackages(%q) = %d, want %d (R7.a)", c.name, c.test, got, c.want)
		}
	}
}
