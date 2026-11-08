package langpack_test

// Go test output, identity and credit (interface contract L3, revision 3):
// R19-R26. Expected values are hand-derived from the contract.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/langpack"
)

// l3Ev builds one go test -json event line (without the newline). Members
// are written in the order given; values are JSON-encoded.
func l3Ev(members ...any) string {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i+1 < len(members); i += 2 {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(members[i])
		v, _ := json.Marshal(members[i+1])
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.String()
}

// l3Pass and friends are typical events as go test -json writes them.
func l3Event(action, pkg, test string) string {
	return l3Ev("Time", "2026-11-08T10:00:00.000000Z", "Action", action, "Package", pkg, "Test", test, "Elapsed", 0.01)
}

func l3R(pkg, test string, pass, fail, skip int) langpack.GoTestResult {
	return langpack.GoTestResult{Package: pkg, Test: test, Pass: pass, Fail: fail, Skip: skip}
}

func l3Want(tests ...langpack.GoTestResult) langpack.GoTestResults {
	if tests == nil {
		tests = []langpack.GoTestResult{}
	}
	return langpack.GoTestResults{Tests: tests}
}

// l3Feed writes chunks to a fresh GoTestOutput, checking R20's Write
// contract on every call, and returns Results(). A panic of an unimplemented
// stub becomes a test failure.
func l3Feed(t *testing.T, chunks ...[]byte) (res langpack.GoTestResults) {
	t.Helper()
	var o langpack.GoTestOutput
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GoTestOutput panicked: %v", r)
		}
	}()
	for _, c := range chunks {
		n, err := o.Write(c)
		if n != len(c) || err != nil {
			t.Fatalf("Write(%d bytes) = %d, %v; want %d, nil (R20)", len(c), n, err, len(c))
		}
	}
	return o.Results()
}

func l3Lines(lines ...string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

func l3Same(t *testing.T, label string, got, want langpack.GoTestResults) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		t.Errorf("%s:\n got  %s\n want %s", label, g, w)
	}
}

func l3Safe[T any](t *testing.T, f func() T) (v T) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked: %v", r)
		}
	}()
	return f()
}

// TestGoTestOutputCountsOnlyJSONResultEvents fails when a line that is not
// exactly one JSON object with a pass, fail or skip Action and a non-empty
// string Test counts, or when such a line does not count once for its test in
// its package (R19, R20 control characters).
func TestGoTestOutputCountsOnlyJSONResultEvents(t *testing.T) {
	t.Parallel()
	const p = "example.com/m/p"
	pass := l3Event("pass", p, "TestA")
	cases := []struct {
		name string
		line string
		want langpack.GoTestResults
	}{
		{"pass", pass, l3Want(l3R(p, "TestA", 1, 0, 0))},
		{"fail", l3Event("fail", p, "TestA"), l3Want(l3R(p, "TestA", 0, 1, 0))},
		{"skip", l3Event("skip", p, "TestA"), l3Want(l3R(p, "TestA", 0, 0, 1))},
		{"subtest", l3Event("pass", p, "TestA/case_1"), l3Want(l3R(p, "TestA/case_1", 1, 0, 0))},
		{"no Package member", l3Ev("Action", "pass", "Test", "TestA"), l3Want(l3R("", "TestA", 1, 0, 0))},
		{"members in another order", l3Ev("Test", "TestA", "Package", p, "Action", "pass"), l3Want(l3R(p, "TestA", 1, 0, 0))},
		{"other members ignored", l3Ev("Action", "pass", "Package", p, "Test", "TestA", "Output", "x", "Extra", map[string]any{"n": []int{1}}), l3Want(l3R(p, "TestA", 1, 0, 0))},
		{"trailing whitespace", pass + " \t ", l3Want(l3R(p, "TestA", 1, 0, 0))},
		{"carriage return line end", pass + "\r", l3Want(l3R(p, "TestA", 1, 0, 0))},
		{"escaped non-ASCII name", `{"Action":"pass","Package":"p","Test":"Test\u00c9cole"}`, l3Want(l3R("p", "TestÉcole", 1, 0, 0))},
		{"lowercase member name", `{"action":"pass","Package":"p","Test":"TestA"}`, l3Want()},
		{"uppercase action value", l3Event("PASS", p, "TestA"), l3Want()},
		{"run action", l3Event("run", p, "TestA"), l3Want()},
		{"output action", l3Event("output", p, "TestA"), l3Want()},
		{"output holding a result line", l3Ev("Action", "output", "Package", p, "Test", "TestA", "Output", "--- PASS: TestA (0.00s)\n"), l3Want()},
		{"output holding a pass event", l3Ev("Action", "output", "Package", p, "Test", "TestA", "Output", pass+"\n"), l3Want()},
		{"bench action", l3Event("bench", p, "BenchmarkA"), l3Want()},
		{"pause action", l3Event("pause", p, "TestA"), l3Want()},
		{"package pass without Test", l3Ev("Action", "pass", "Package", p), l3Want()},
		{"empty Test", l3Event("pass", p, ""), l3Want()},
		{"Test not a string", `{"Action":"pass","Package":"p","Test":7}`, l3Want()},
		{"Action not a string", `{"Action":1,"Package":"p","Test":"TestA"}`, l3Want()},
		{"Package not a string", `{"Action":"pass","Package":["p"],"Test":"TestA"}`, l3Want()},
		{"duplicate Action", `{"Action":"run","Action":"pass","Package":"p","Test":"TestA"}`, l3Want()},
		{"duplicate ignored member", `{"Time":"a","Time":"b","Action":"pass","Package":"p","Test":"TestA"}`, l3Want()},
		{"invalid UTF-8 in an ignored member", "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"TestA\",\"Output\":\"\xff\"}", l3Want()},
		{"invalid UTF-8 in Test", "{\"Action\":\"pass\",\"Package\":\"p\",\"Test\":\"Test\xc3\"}", l3Want()},
		{"leading space", " " + pass, l3Want()},
		{"text after the object", pass + "x", l3Want()},
		{"two objects", pass + pass, l3Want()},
		{"array", "[" + pass + "]", l3Want()},
		{"truncated object", pass[:len(pass)-1], l3Want()},
		{"verbose result line", "--- PASS: TestA (0.00s)", l3Want()},
		{"verbose fail line", "--- FAIL: TestA (0.00s)", l3Want()},
		{"package line", "ok  \texample.com/m/p\t0.012s", l3Want()},
		{"build output", "# example.com/m/p", l3Want()},
		{"control character in Test", `{"Action":"pass","Package":"p","Test":"TestA\u0007"}`, l3Want()},
		{"DEL in Test", `{"Action":"pass","Package":"p","Test":"TestA\u007f"}`, l3Want()},
		{"C1 control in Test", `{"Action":"pass","Package":"p","Test":"TestA\u0085"}`, l3Want()},
		{"newline in Package", `{"Action":"pass","Package":"p\nq","Test":"TestA"}`, l3Want()},
		{"tab in Package", `{"Action":"pass","Package":"p\tq","Test":"TestA"}`, l3Want()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l3Same(t, fmt.Sprintf("line %q", c.line), l3Feed(t, []byte(c.line+"\n")), c.want)
		})
	}
}

// TestGoTestOutputCountsRepeatedEvents fails when counts do not accumulate
// per (package, test), or one test name in two packages shares an entry
// (R19, R21).
func TestGoTestOutputCountsRepeatedEvents(t *testing.T) {
	t.Parallel()
	got := l3Feed(t, l3Lines(
		l3Event("run", "a", "TestA"),
		l3Event("pass", "a", "TestA"),
		l3Event("pass", "a", "TestA"),
		l3Event("fail", "a", "TestA"),
		l3Event("skip", "a", "TestA"),
		l3Event("pass", "b", "TestA"),
		"--- PASS: TestA (0.00s)",
		l3Event("fail", "b", "TestA"),
	))
	l3Same(t, "repeated events", got, l3Want(l3R("a", "TestA", 2, 1, 1), l3R("b", "TestA", 1, 1, 0)))
}

// TestGoTestOutputSortsResults fails when Results is not sorted by package and
// then test in byte order (R21).
func TestGoTestOutputSortsResults(t *testing.T) {
	t.Parallel()
	got := l3Feed(t, l3Lines(
		l3Event("pass", "b", "TestA"),
		l3Event("pass", "a/b", "TestZ"),
		l3Event("pass", "a.b", "TestA"),
		l3Event("pass", "a", "TestÉ"),
		l3Event("pass", "a", "Test_x"),
		l3Event("pass", "a", "TestB"),
		l3Event("pass", "a", "TestA/sub"),
		l3Event("pass", "a", "TestA"),
		l3Ev("Action", "pass", "Test", "TestQ"),
	))
	l3Same(t, "sorted results", got, l3Want(
		l3R("", "TestQ", 1, 0, 0),
		l3R("a", "TestA", 1, 0, 0),
		l3R("a", "TestA/sub", 1, 0, 0),
		l3R("a", "TestB", 1, 0, 0),
		l3R("a", "Test_x", 1, 0, 0),
		l3R("a", "TestÉ", 1, 0, 0),
		l3R("a.b", "TestA", 1, 0, 0),
		l3R("a/b", "TestZ", 1, 0, 0),
		l3R("b", "TestA", 1, 0, 0),
	))
}

// TestGoTestOutputZeroValueAndRepeatedResults fails when the zero value is not
// ready, Tests is nil, or calling Results again changes the value (R21).
func TestGoTestOutputZeroValueAndRepeatedResults(t *testing.T) {
	t.Parallel()
	empty := l3Feed(t)
	if empty.Tests == nil || len(empty.Tests) != 0 || empty.Truncated {
		t.Errorf("zero GoTestOutput Results() = %#v, want empty non-nil Tests (R21)", empty)
	}
	if data, _ := json.Marshal(empty); string(data) != `{"tests":[],"truncated":false}` {
		t.Errorf("zero results encode as %s, want {\"tests\":[],\"truncated\":false} (R21)", data)
	}
	noTests := l3Feed(t, []byte("PASS\nok  \tp\t0.1s\n"))
	if noTests.Tests == nil {
		t.Errorf("Results() after non-event output has nil Tests (R21)")
	}
	var o langpack.GoTestOutput
	l3Safe(t, func() int { n, _ := o.Write(l3Lines(l3Event("pass", "p", "TestA"))); return n })
	first := l3Safe(t, o.Results)
	second := l3Safe(t, o.Results)
	l3Same(t, "second Results()", second, first)
	l3Same(t, "first Results()", first, l3Want(l3R("p", "TestA", 1, 0, 0)))
}

// TestGoTestOutputReadsTheUnterminatedLine fails when the bytes after the last
// newline are not read as a final line for Results, or when Results consumes
// them so that a later Write does not continue that line (R21).
func TestGoTestOutputReadsTheUnterminatedLine(t *testing.T) {
	t.Parallel()
	pass := l3Event("pass", "p", "TestA")
	var o langpack.GoTestOutput
	write := func(s string) {
		t.Helper()
		n := l3Safe(t, func() int {
			n, err := o.Write([]byte(s))
			if err != nil {
				t.Errorf("Write returned %v (R20)", err)
			}
			return n
		})
		if n != len(s) {
			t.Fatalf("Write = %d for %d bytes (R20)", n, len(s))
		}
	}
	results := func() langpack.GoTestResults { t.Helper(); return l3Safe(t, o.Results) }

	write(pass)
	l3Same(t, "unterminated final line", results(), l3Want(l3R("p", "TestA", 1, 0, 0)))
	write("  \n")
	l3Same(t, "line continued with whitespace", results(), l3Want(l3R("p", "TestA", 1, 0, 0)))

	write(l3Event("pass", "p", "TestB"))
	l3Same(t, "second unterminated line", results(), l3Want(l3R("p", "TestA", 1, 0, 0), l3R("p", "TestB", 1, 0, 0)))
	write("x\n")
	l3Same(t, "line continued into garbage", results(), l3Want(l3R("p", "TestA", 1, 0, 0)))

	write(`{"Action":"pa`)
	l3Same(t, "partial event", results(), l3Want(l3R("p", "TestA", 1, 0, 0)))
	write(`ss","Package":"p","Test":"TestC"}` + "\n")
	l3Same(t, "event completed after Results", results(), l3Want(l3R("p", "TestA", 1, 0, 0), l3R("p", "TestC", 1, 0, 0)))
}

// l3Padded returns a pass event for test whose line is exactly n bytes long.
func l3Padded(t *testing.T, test string, n int) string {
	t.Helper()
	head := `{"Action":"pass","Package":"p","Test":"` + test + `","Output":"`
	pad := n - len(head) - len(`"}`)
	if pad < 0 {
		t.Fatalf("line of %d bytes too short for %s", n, test)
	}
	line := head + strings.Repeat("o", pad) + `"}`
	if len(line) != n {
		t.Fatalf("built %d bytes, want %d", len(line), n)
	}
	return line
}

// TestGoTestOutputLineLengthBound fails when a line of exactly 64 KiB does
// not count, a longer one counts, or a long line changes how later lines are
// read (R20).
func TestGoTestOutputLineLengthBound(t *testing.T) {
	t.Parallel()
	limit := langpack.MaxGoTestLineBytes
	if limit != 64<<10 {
		t.Fatalf("MaxGoTestLineBytes = %d, want 64 KiB as the contract states", limit)
	}
	exact := l3Padded(t, "TestExact", limit)
	exactCR := l3Padded(t, "TestExactCR", limit) + "\r"
	over := l3Padded(t, "TestOver", limit+1)
	huge := l3Padded(t, "TestHuge", 5*limit)
	after := l3Event("pass", "p", "TestAfter")
	input := l3Lines(exact, exactCR, over, after, huge, after, strings.Repeat("y", 3*limit), after)
	want := l3Want(l3R("p", "TestAfter", 3, 0, 0), l3R("p", "TestExact", 1, 0, 0), l3R("p", "TestExactCR", 1, 0, 0))
	l3Same(t, "one write", l3Feed(t, input), want)
	var chunks [][]byte
	for i := 0; i < len(input); i += 1000 {
		chunks = append(chunks, input[i:min(i+1000, len(input))])
	}
	l3Same(t, "1000-byte writes", l3Feed(t, chunks...), want)
	// The over-long line as the unterminated final line counts nothing either.
	l3Same(t, "over-long final line", l3Feed(t, []byte(after+"\n"+over)), l3Want(l3R("p", "TestAfter", 1, 0, 0)))
}

// l3Corpus mixes events, noise, CRLF, over-long and unterminated lines.
func l3Corpus() []byte {
	var lines []string
	for i := 0; i < 40; i++ {
		pkg := fmt.Sprintf("example.com/m/p%d", i%3)
		lines = append(lines,
			l3Event("run", pkg, fmt.Sprintf("Test%d", i%7)),
			l3Event([]string{"pass", "fail", "skip"}[i%3], pkg, fmt.Sprintf("Test%d", i%7)),
			"--- PASS: TestNoise (0.00s)",
			l3Event("pass", pkg, fmt.Sprintf("Test%d/sub_%d", i%7, i%2))+"\r",
			`{"Action":"pass","Test":"TestDup","Test":"x"}`,
		)
		if i%13 == 0 {
			lines = append(lines, strings.Repeat("z", langpack.MaxGoTestLineBytes+5))
		}
	}
	return []byte(strings.Join(lines, "\n") + "\n" + l3Event("fail", "tail", "TestTail"))
}

// TestGoTestOutputSplitInvariance fails when splitting the same bytes into
// different Write calls, or calling Results between writes, changes the
// results (R20, R21).
func TestGoTestOutputSplitInvariance(t *testing.T) {
	t.Parallel()
	data := l3Corpus()
	whole := l3Feed(t, data)
	if len(whole.Tests) < 10 {
		t.Fatalf("corpus read as %d tests; the property would prove little", len(whole.Tests))
	}
	bytewise := make([][]byte, len(data))
	for i := range data {
		bytewise[i] = data[i : i+1]
	}
	l3Same(t, "byte-by-byte writes", l3Feed(t, bytewise...), whole)
	rng := rand.New(rand.NewPCG(3, 17))
	for iter := 0; iter < 60; iter++ {
		var o langpack.GoTestOutput
		for i := 0; i < len(data); {
			n := rng.IntN(300)
			if rng.IntN(10) == 0 {
				n = rng.IntN(3 * langpack.MaxGoTestLineBytes)
			}
			end := min(i+n, len(data))
			chunk := data[i:end]
			if w := l3Safe(t, func() int {
				w, err := o.Write(chunk)
				if err != nil {
					t.Errorf("Write returned %v (R20)", err)
				}
				return w
			}); w != len(chunk) {
				t.Fatalf("Write returned %d for %d bytes (R20)", w, len(chunk))
			}
			if rng.IntN(4) == 0 {
				l3Safe(t, o.Results)
			}
			i = end
		}
		got := l3Safe(t, o.Results)
		if !reflect.DeepEqual(got, whole) {
			t.Fatalf("iteration %d: random splits gave different results than one write (R20)", iter)
		}
	}
}

// FuzzGoTestOutputSplitInvariance checks R20 and R21 over arbitrary bytes:
// one write, two writes at any cut and byte-by-byte writes agree, and the
// results are sorted, distinct and free of empty or control-character names.
func FuzzGoTestOutputSplitInvariance(f *testing.F) {
	f.Add(l3Corpus()[:4000], uint16(17))
	f.Add([]byte(l3Event("pass", "p", "TestA")+"\r\n"+l3Event("fail", "p", "TestA")), uint16(5))
	f.Add([]byte(`{"Action":"pass","Test":"T"}`), uint16(0))
	f.Fuzz(func(t *testing.T, data []byte, cut uint16) {
		whole := l3Feed(t, data)
		c := int(cut) % (len(data) + 1)
		l3Same(t, "two writes", l3Feed(t, data[:c], data[c:]), whole)
		bytewise := make([][]byte, len(data))
		for i := range data {
			bytewise[i] = data[i : i+1]
		}
		l3Same(t, "byte-by-byte", l3Feed(t, bytewise...), whole)
		if whole.Tests == nil {
			t.Fatalf("nil Tests (R21)")
		}
		for i, r := range whole.Tests {
			if r.Test == "" || strings.ContainsFunc(r.Test+r.Package, func(c rune) bool { return c < 0x20 || (c >= 0x7f && c <= 0x9f) }) {
				t.Fatalf("recorded %q in %q (R19, R20)", r.Test, r.Package)
			}
			if r.Pass+r.Fail+r.Skip == 0 {
				t.Fatalf("entry %q with no count (R21)", r.Test)
			}
			if i > 0 {
				p := whole.Tests[i-1]
				if p.Package > r.Package || (p.Package == r.Package && p.Test >= r.Test) {
					t.Fatalf("results not sorted and distinct at %d (R21)", i)
				}
			}
		}
	})
}

// TestGoTestOutputCountBound fails when more than 50,000 distinct pairs are
// recorded, when exactly 50,000 set Truncated, or when counts of recorded
// pairs stop changing after the bound (R22).
func TestGoTestOutputCountBound(t *testing.T) {
	t.Parallel()
	if langpack.MaxGoTestResults != 50000 {
		t.Fatalf("MaxGoTestResults = %d, want 50000 as the contract states", langpack.MaxGoTestResults)
	}
	// No Package and 5-byte names cost 2*5+128 = 138 bytes each, so 50,000
	// pairs use 6,900,000 of the 7 MiB: only the count bound applies.
	var buf bytes.Buffer
	for i := 0; i < 50000; i++ {
		fmt.Fprintf(&buf, `{"Action":"pass","Test":"%05d"}`+"\n", i)
	}
	var o langpack.GoTestOutput
	l3Safe(t, func() int { n, _ := o.Write(buf.Bytes()); return n })
	at := l3Safe(t, o.Results)
	if len(at.Tests) != 50000 || at.Truncated {
		t.Fatalf("50,000 pairs: recorded %d, truncated %v; want 50000, false (R22)", len(at.Tests), at.Truncated)
	}
	l3Safe(t, func() int {
		n, _ := o.Write([]byte(`{"Action":"pass","Test":"50000"}` + "\n" + `{"Action":"fail","Test":"00000"}` + "\n"))
		return n
	})
	over := l3Safe(t, o.Results)
	if len(over.Tests) != 50000 || !over.Truncated {
		t.Fatalf("50,001 pairs: recorded %d, truncated %v; want 50000, true (R22)", len(over.Tests), over.Truncated)
	}
	if over.Tests[0] != l3R("", "00000", 1, 1, 0) {
		t.Errorf("first pair after the bound = %+v, want its fail counted (R22)", over.Tests[0])
	}
	for _, r := range over.Tests {
		if r.Test == "50000" {
			t.Errorf("pair 50,001 was recorded (R22)")
		}
	}
}

// TestGoTestOutputByteBound fails when the 7 MiB size bound is off by one,
// counts names without the factor 2 or the 128-byte overhead, stops counting
// recorded pairs after it is hit, or refuses a later new pair that still fits
// (R22).
func TestGoTestOutputByteBound(t *testing.T) {
	t.Parallel()
	if langpack.MaxGoTestResultBytes != 7<<20 {
		t.Fatalf("MaxGoTestResultBytes = %d, want 7 MiB as the contract states", langpack.MaxGoTestResultBytes)
	}
	// Package "p" and 4095-byte names cost 2*(1+4095)+128 = 8320 bytes:
	// 882 pairs use 7,338,240 bytes and leave 1,792 = 2*(1+831)+128.
	name := func(i, n int) string { return fmt.Sprintf("T%05d", i) + strings.Repeat("x", n-6) }
	var buf bytes.Buffer
	for i := 0; i < 882; i++ {
		buf.WriteString(l3Ev("Action", "pass", "Package", "p", "Test", name(i, 4095)) + "\n")
	}
	base := buf.Bytes()
	ev := func(test string) []byte { return []byte(l3Ev("Action", "pass", "Package", "p", "Test", test) + "\n") }

	fits := l3Feed(t, base, ev(name(882, 831)))
	if len(fits.Tests) != 883 || fits.Truncated {
		t.Fatalf("pairs summing to exactly 7 MiB: recorded %d, truncated %v; want 883, false (R22)", len(fits.Tests), fits.Truncated)
	}
	full := l3Feed(t, base, ev(name(882, 831)), ev("U"), ev(name(0, 4095)))
	if len(full.Tests) != 883 || !full.Truncated {
		t.Fatalf("one pair past 7 MiB: recorded %d, truncated %v; want 883, true (R22)", len(full.Tests), full.Truncated)
	}
	if full.Tests[0].Test != name(0, 4095) || full.Tests[0].Pass != 2 {
		t.Errorf("first pair after the bound: %q pass %d, want pass 2 (R22)", full.Tests[0].Test, full.Tests[0].Pass)
	}
	over := l3Feed(t, base, ev(name(882, 832)))
	if len(over.Tests) != 882 || !over.Truncated {
		t.Errorf("a pair 2 bytes past 7 MiB: recorded %d, truncated %v; want 882, true (R22)", len(over.Tests), over.Truncated)
	}
	// R22 decides each new pair on its own: after the 1794-byte pair is
	// refused, "U" (2*(1+1)+128 = 132 bytes) still fits in the 1,792 left.
	later := l3Feed(t, base, ev(name(882, 832)), ev("U"))
	if len(later.Tests) != 883 || !later.Truncated || later.Tests[882] != l3R("p", "U", 1, 0, 0) {
		t.Errorf("a smaller pair after a refused one: recorded %d, truncated %v; want 883 ending in U pass 1, true (R22)",
			len(later.Tests), later.Truncated)
	}
}

// TestGoTestsCredited fails when per-test credit differs from R23's examples
// and boundary cases.
func TestGoTestsCredited(t *testing.T) {
	t.Parallel()
	cases := []struct {
		argv    string
		goflags string
		want    bool
	}{
		{"go test -json ./...", "", true},
		{"go test --json ./...", "", true},
		{"go test ./... -json", "", true},
		{"go test -race -count=1 -json ./internal/...", "", true},
		{"go test -json example.com/m/p", "", true},
		{"go test -json", "", true},
		{"go test -json ./a ./b", "", true},
		{"/usr/local/go/bin/go test -json ./...", "", true},
		{"go.exe test -json ./...", "", true},
		{"go test -json ./...", "-count=1", true},
		{"go test ./...", "", false},
		{"go test -v ./...", "", false},
		{"go test -json=true ./...", "", false},
		{"go test -json -json=false ./...", "", false},
		{"go test --json=true ./...", "", false},
		{"go test -tags -json ./...", "", false},
		{"go test -json -run TestX ./...", "", false},
		{"go test -json -skip TestX ./...", "", false},
		{"go test -json ./... -args -x", "", false},
		{"go vet -json ./...", "", false},
		{"gofmt test -json ./...", "", false},
		{"go test -json -count=0 ./...", "", false},
		{"go test -json ./a -race ./b", "", false},
		{"go test ./...", "-json", false},
		{"go test -json ./...", "-run=X", false},
		{"go test -json ./...", "-count=0", false},
	}
	for _, c := range cases {
		run := langpack.Run{Argv: strings.Fields(c.argv), GOFLAGS: c.goflags}
		if got := l3Safe(t, func() bool { return langpack.GoTestsCredited(run) }); got != c.want {
			t.Errorf("GoTestsCredited(%q, GOFLAGS %q) = %v, want %v (R23)", c.argv, c.goflags, got, c.want)
		}
	}
}

// TestGoTestsCreditedMatchesGoTestsRule fails when, for argv with -json as a
// flag argument and ./... as a package argument, credit differs from whether
// the existing GoTests rule raises no problem (R23 equivalence).
func TestGoTestsCreditedMatchesGoTestsRule(t *testing.T) {
	t.Parallel()
	flags := [][]string{
		{"-race"}, {"-count=1"}, {"-count=0"}, {"-count", "2"}, {"-v"}, {"-short"}, {"-failfast"},
		{"-run", "TestX"}, {"-run=TestX"}, {"-skip", "TestY"}, {"-timeout", "30s"}, {"-tags", "e2e"},
		{"-cover"}, {"-p", "4"}, {"-cpu", "1,2"}, {"-shuffle", "on"}, {"-zzz"}, {"-args", "-x"}, {"-json"}, {"--json"},
		{"-bench", "."}, {"-list", "."}, {"-c"}, {"-o", "x.test"}, {"-exec", "x"}, {"-vet=off"},
	}
	goflags := []string{"", "-race", "-count=1", "-run=X", "-mod=mod", "-v", "-count=0", "-json"}
	src := langpack.Source{Current: []string{"go.mod", "p/p.go"}, Changed: []string{"p/p.go"}}
	rng := rand.New(rand.NewPCG(23, 46))
	seen := map[bool]int{}
	for iter := 0; iter < 400; iter++ {
		argv := []string{"go", "test", "-json"}
		var after, tail []string
		for k := rng.IntN(4); k > 0; k-- {
			fl := flags[rng.IntN(len(flags))]
			switch {
			case fl[0] == "-args":
				tail = fl // keeps ./... a package argument
			case rng.IntN(3) == 0:
				after = append(after, fl...)
			default:
				argv = append(argv, fl...)
			}
		}
		argv = append(argv, "./...")
		argv = append(argv, after...)
		argv = append(argv, tail...)
		run := langpack.Run{Argv: argv, GOFLAGS: goflags[rng.IntN(len(goflags))]}
		// -json right after "test" is a flag argument, and no token is
		// -json=<value>, so condition 2 holds for every generated run.
		credited := l3Safe(t, func() bool { return langpack.GoTestsCredited(run) })
		clean := len(langpack.GoTests{}.Problems(src, []langpack.Run{run})) == 0
		if credited != clean {
			t.Errorf("argv %q GOFLAGS %q: GoTestsCredited = %v, GoTests problems empty = %v (R23)", argv, run.GOFLAGS, credited, clean)
		}
		seen[credited]++
	}
	if seen[true] == 0 || seen[false] == 0 {
		t.Errorf("generator produced only one outcome %v; the property would prove little", seen)
	}
}

// TestGoTestName fails when test-name validity differs from R24.
func TestGoTestName(t *testing.T) {
	t.Parallel()
	long := "Test" + strings.Repeat("A", 1020)
	cases := map[string]bool{
		"TestX": true, "Test_x": true, "Test1": true, "Test": true, "TestÉcole": true, "TestA/sub_case": true,
		"FuzzP/seed#0": true, "ExampleT": true, "Example_x": true, "Fuzz": true, "Example": true,
		"TestA/sub/deeper": true, "TestA/é": true, "Test/x": true, "ExampleT_Method": true,
		long: true, long[:1019] + "/" + "abcd": true,
		"Testx": false, "Testé": false, "testX": false, "TestA/": false, "TestA//b": false, "TestA/a b": false,
		"BenchmarkX": false, "Test-x": false, "": false, "Fuzzx": false, "Examplex": false, "TestA b": false,
		"TestA/a\tb": false, "TestA/\x01": false, "TestA/\xff": false, "/TestA": false, "Test\u00a0": false,
		long + "A": false, long + "/b": false, "TestA/a\u00a0b": false, "TestA/a\u0085b": false, "X": false,
	}
	for name, want := range cases {
		if got := l3Safe(t, func() bool { return langpack.GoTestName(name) }); got != want {
			t.Errorf("GoTestName(%q) [%d bytes] = %v, want %v (R24)", name, len(name), got, want)
		}
	}
}

// TestGoTestDeclared fails when declaration detection differs from R25.
func TestGoTestDeclared(t *testing.T) {
	t.Parallel()
	src := `//go:build never

package p

import "testing"

type S struct{}

func (S) TestMethod(t *testing.T) {}

var TestVar = func(t *testing.T) {}

// func TestCommented(t *testing.T) {}

func TestX(t *testing.T) {
	_ = func() {}
}

func TestNoArgs() {}

func helper() { TestInner := 1; _ = TestInner }
`
	cases := []struct {
		src, name string
		want      bool
	}{
		{src, "TestX", true},
		{src, "TestX/sub case", true},
		{src, "TestNoArgs", true},
		{src, "TestY", false},
		{src, "TestMethod", false},
		{src, "TestVar", false},
		{src, "TestCommented", false},
		{src, "TestInner", false},
		{src, "TestXY", false},
		{"package p\nfunc TestX(t *testing.T) {", "TestX", false},
		{"func TestX(t *testing.T) {}\n", "TestX", false},
		{"", "TestX", false},
	}
	for _, c := range cases {
		if got := l3Safe(t, func() bool { return langpack.GoTestDeclared([]byte(c.src), c.name) }); got != c.want {
			t.Errorf("GoTestDeclared(%.40q, %q) = %v, want %v (R25)", c.src, c.name, got, c.want)
		}
	}
}

// TestGoTestPackage fails when the package go test ./... at the module root
// would compile a file into differs from R26, including the cases where
// go test ignores the file.
func TestGoTestPackage(t *testing.T) {
	t.Parallel()
	const mod = "module example.com/m\n\ngo 1.22\n"
	current := []string{"go.mod", "a_test.go", "x/y/a_test.go", "sub/go.mod", "sub/a_test.go"}
	type want struct {
		pkg string
		ok  bool
	}
	modules := map[string]want{
		mod: {"example.com/m", true},
		"// header\n\nmodule example.com/m // trailing\n": {"example.com/m", true},
		"module \"example.com/m\"\n":                      {"example.com/m", true},
		"module \"example.com/m\" // c\n":                 {"example.com/m", true},
		"module `example.com/m`\n":                        {"example.com/m", true},
		"module \"example.com/\\x6d\"\n":                  {"example.com/m", true},
		"  module\texample.com/m\n":                       {"example.com/m", true},
		"module a.com/first\nmodule b.com/second\n":       {"a.com/first", true},
		"go 1.22\n":                 {"", false},
		"modulex example.com/m\n":   {"", false},
		"// module example.com/m\n": {"", false},
		"":                          {"", false},
	}
	for gomod, w := range modules {
		got := l3Safe(t, func() want {
			p, ok := langpack.GoTestPackage([]byte(gomod), current, "x/y/a_test.go")
			return want{p, ok}
		})
		if w.ok {
			w.pkg += "/x/y"
		}
		if got != w {
			t.Errorf("GoTestPackage(go.mod %q, x/y/a_test.go) = %q, %v; want %q, %v (R26)", gomod, got.pkg, got.ok, w.pkg, w.ok)
		}
	}
	files := map[string]want{
		"a_test.go":              {"example.com/m", true},
		"x/y/a_test.go":          {"example.com/m/x/y", true},
		"z/unlisted_test.go":     {"example.com/m/z", true},
		"testdatax/a_test.go":    {"example.com/m/testdatax", true},
		"x/vendored/a_test.go":   {"example.com/m/x/vendored", true},
		"subx/a_test.go":         {"example.com/m/subx", true},
		"x/a.b/a_test.go":        {"example.com/m/x/a.b", true},
		"":                       {"", false},
		"/x/a_test.go":           {"", false},
		"./a_test.go":            {"", false},
		"x//a_test.go":           {"", false},
		"x/../a_test.go":         {"", false},
		"x/":                     {"", false},
		"..":                     {"", false},
		"../a_test.go":           {"", false},
		"_a_test.go":             {"", false},
		".a_test.go":             {"", false},
		"_test.go":               {"", false},
		"x/_a_test.go":           {"", false},
		"testdata/a_test.go":     {"", false},
		"x/testdata/y/a_test.go": {"", false},
		"vendor/a_test.go":       {"", false},
		"x/vendor/a_test.go":     {"", false},
		"_x/a_test.go":           {"", false},
		"x/.y/a_test.go":         {"", false},
		"sub/a_test.go":          {"", false},
		"sub/deep/a_test.go":     {"", false},
	}
	for file, w := range files {
		got := l3Safe(t, func() want { p, ok := langpack.GoTestPackage([]byte(mod), current, file); return want{p, ok} })
		if got != w {
			t.Errorf("GoTestPackage(%q) = %q, %v; want %q, %v (R26)", file, got.pkg, got.ok, w.pkg, w.ok)
		}
	}
	// A nested go.mod counts only when it is listed in current.
	if got := l3Safe(t, func() want {
		p, ok := langpack.GoTestPackage([]byte(mod), []string{"go.mod"}, "sub/a_test.go")
		return want{p, ok}
	}); got != (want{"example.com/m/sub", true}) {
		t.Errorf("GoTestPackage(sub/a_test.go) without sub/go.mod in current = %q, %v; want example.com/m/sub, true (R26)", got.pkg, got.ok)
	}
}
