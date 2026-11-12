package shellwords

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Behavioural tests for the command splitter written from test contract
// linear-split, revision 5 (requirements R1, R3 and R4), first for the
// splitter in internal/usagelog that Split replaces. They use only split,
// Split, the words' Text, the heredoc bodies and the work count.

// splitTestBodies returns the heredoc bodies of sh in order (a `> FILE`
// redirection has no body).
func splitTestBodies(sh []SimpleCommand) []string {
	var bodies []string
	for _, c := range sh {
		for _, r := range c.Redirects {
			if r.Op == "<<" || r.Op == "<<-" {
				bodies = append(bodies, r.Body)
			}
		}
	}
	return bodies
}

// splitTestHasCall reports whether some command's words start with prefix.
func splitTestHasCall(sh []SimpleCommand, prefix ...string) bool {
	for _, c := range sh {
		if len(c.Words) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			if c.Words[i].Text != p {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func splitTestCallFields(sh []SimpleCommand) [][]string {
	out := make([][]string, 0, len(sh))
	for _, c := range sh {
		var fields []string
		for _, w := range c.Words {
			fields = append(fields, w.Text)
		}
		out = append(out, fields)
	}
	return out
}

// splitTestWorkBound is R1's bound on the scanner work for a command.
func splitTestWorkBound(command string) int { return 4*len(command) + 16 }

// splitTestWords returns n copies of "a<<b" joined by sep.
func splitTestWords(n int, sep string) string {
	words := make([]string, n)
	for i := range words {
		words[i] = "a<<b"
	}
	return strings.Join(words, sep)
}

// splitTestDeepRow is one R4a input: a command nested n levels deep, built
// for size n, and the exact number of non-redirect heredoc bodies it must give
// for that size (bodies == nil: unchecked). Every expected body is "".
type splitTestDeepRow struct {
	name   string
	cmd    func(n int) string
	bodies func(n int) int
}

func splitTestNone(int) int   { return 0 }
func splitTestEach(n int) int { return n }

// splitTestDeepRows are the R4a rows (contract revision 5).
func splitTestDeepRows() []splitTestDeepRow {
	return []splitTestDeepRow{
		{"deep_lines", func(n int) string {
			return strings.Repeat("( ", n) + strings.Repeat("\n", n)
		}, nil},
		{"deep_valid_markers", func(n int) string {
			return strings.Repeat("( ", n) + splitTestWords(n, " ")
		}, splitTestEach},
		{"deep_invalid_markers", func(n int) string {
			return strings.Repeat("( ", n) + strings.Repeat("a<<1 ", n)
		}, splitTestNone},
		{"deep_marker_lines", func(n int) string {
			return strings.Repeat("( ", n) + "\n" + strings.Repeat("a<<b\nb\n", n)
		}, splitTestEach},
		// Every << is an arithmetic shift inside $(( ... )): a correctness row.
		{"deep_arithmetic", func(n int) string {
			return "echo " + strings.Repeat("$(( ", n) + splitTestWords(n, " ")
		}, splitTestNone},
	}
}

// R1 and R4a (contract revision 5): this work-count test checks that splitting
// work stays linear in the command length, including for deeply nested
// commands. It does not measure time and is not an end-to-end timing check.
//
// What it guards: `work` grows with the bytes each step of the lexer moves
// over, which it does only forwards, with the bytes of each heredoc body line
// it compares with the delimiter, and with each line it searches for the
// closing quote of a heredoc delimiter. A splitter whose search for a
// delimiter line or closing quote at every "<<" runs over the rest of the
// command, or that compares a body line more than once, does work quadratic
// in the number of markers or lines: about 10 million bytes at 2000 words and
// about 160 million at 8000 words, against bounds of about 40 KB and 160 KB,
// and work(4n)/work(n) is about 16 instead of about 4. deep_arithmetic also
// checks that no << inside $(( ... )) is taken as a heredoc, and the other
// deep rows check their exact body counts.
//
// What it does not detect: work the lexer does without counting it, such as
// a separate scan back over bytes already passed, or searching or copying the
// stack of open quotes and parentheses for every line or marker, which only
// R4d's allocation bound (TestSplitDeepNestingAllocations) can catch, and
// only when the copy allocates; a quadratic marker matcher; and pattern
// matching against the rest of the command.
func TestSplitWorkLinear(t *testing.T) {
	t.Parallel()
	const n = 2000
	type input struct {
		name string
		cmd  func(n int) string
		// cwStart: the command holds a `cw task start` call; quoted: none of the
		// "<<" is a heredoc.
		cwStart, quoted bool
		// bodies: exact number of non-redirect heredoc bodies, all "", for
		// size n (nil: unchecked).
		bodies func(n int) int
	}
	inputs := []input{
		{"single_quoted", func(n int) string {
			return "echo '" + splitTestWords(n, " ") + "'\ncw task start --repo r"
		}, true, true, nil},
		{"double_quoted", func(n int) string {
			return "echo \"" + splitTestWords(n, " ") + " \"\ncw task start --repo r"
		}, true, true, nil},
		{"unquoted_cw_first", func(n int) string {
			return "cw task start --repo r\necho " + splitTestWords(n, " ")
		}, true, false, nil},
		{"arithmetic", func(n int) string {
			return "echo $((" + splitTestWords(n, "+") + "))"
		}, false, false, nil},
	}
	for _, d := range splitTestDeepRows() {
		inputs = append(inputs, input{name: d.name, cmd: d.cmd, bodies: d.bodies})
	}
	for _, in := range inputs {
		t.Run(in.name, func(t *testing.T) {
			t.Parallel()
			var work [2]int
			for i, words := range []int{n, 4 * n} {
				cmd := in.cmd(words)
				result, w := split(cmd)
				sh := result.Commands
				work[i] = w
				if w > splitTestWorkBound(cmd) {
					t.Errorf("%d words: work = %d, want <= 4*len+16 = %d (len %d)", words, w, splitTestWorkBound(cmd), len(cmd))
				}
				if w <= 0 {
					t.Errorf("%d words: work = %d, want > 0 (the lexer reads the command)", words, w)
				}
				if in.cwStart && !splitTestHasCall(sh, "cw", "task", "start") {
					t.Errorf("%d words: no call starts with cw task start; calls %d", words, len(sh))
				}
				if in.quoted {
					if b := splitTestBodies(sh); len(b) != 0 {
						t.Errorf("%d words: %d heredoc bodies, want none (every << is quoted)", words, len(b))
					}
				}
				if in.bodies != nil {
					b := splitTestBodies(sh)
					if want := in.bodies(words); len(b) != want {
						t.Errorf("%d words: %d heredoc bodies, want %d", words, len(b), want)
					}
					for j, body := range b {
						if body != "" {
							t.Errorf("%d words: heredoc body %d = %q, want \"\"", words, j, body)
							break
						}
					}
				}
			}
			// Linear: about 4x for 4x the words; a rescanning splitter gives about 16x.
			if work[0] > 0 {
				ratio := float64(work[1]) / float64(work[0])
				if ratio < 3 || ratio > 5 {
					t.Errorf("work(%d words) = %d, work(%d words) = %d: ratio %.2f, want about 4 (3..5)",
						n, work[0], 4*n, work[1], ratio)
				}
			}
		})
	}
}

// splitTestHeredocWant is the expected result of one heredoc-marker case.
type splitTestHeredocWant struct {
	bodies     []string // non-redirect heredoc bodies, in order; nil means none
	firstOnly  bool     // assert only bodies[0] (case h)
	startCall  bool     // a call starts with cw task start
	verifyCall bool     // a call starts with cw task verify
	notCall    string   // no call's first field is this word ("" = unchecked)
}

type splitTestHeredocCase struct {
	name, cmd string
	want      splitTestHeredocWant
}

// splitTestHeredocCases are the cases of TestSplitHeredocMarkers (R3 and
// R4b); FuzzSplitWork also uses their commands as seeds.
func splitTestHeredocCases() []splitTestHeredocCase {
	heredocA := splitTestHeredocWant{bodies: []string{"cw task start"}, verifyCall: true}
	plain := splitTestHeredocWant{startCall: true}
	return []splitTestHeredocCase{
		// a
		{"a_plain", "cat <<EOF\ncw task start\nEOF\ncw task verify", heredocA},
		// b
		{"b_dash", "cat <<-EOF\ncw task start\nEOF\ncw task verify", heredocA},
		{"b_space_single_quoted", "cat << 'EOF'\ncw task start\nEOF\ncw task verify", heredocA},
		{"b_double_quoted", "cat <<\"EOF\"\ncw task start\nEOF\ncw task verify", heredocA},
		{"b_tab", "cat <<\tEOF\ncw task start\nEOF\ncw task verify", heredocA},
		{"b_unclosed_quote", "cat <<'EOF\ncw task start\nEOF\ncw task verify", heredocA},
		// c
		{"c_here_string", "cat <<<EOF\ncw task start", plain},
		// d
		{"d_single_quotes", "echo 'x<<EOF'\ncw task start", plain},
		{"d_double_quotes", "echo \"x<<EOF\"\ncw task start", plain},
		{"d_dollar_quotes", "echo $'x<<EOF'\ncw task start", plain},
		// e
		{"e_command_substitution_in_double_quotes", "echo \"$(cat <<EOF\ncw task start\nEOF\n)\"\ncw task verify", heredocA},
		// f
		{"f_arithmetic_shift", "echo $((1<<n))\ncw task start", plain},
		// g
		{"g_digit_after_marker", "cat << 1\ncw task start", plain},
		{"g_marker_at_end_of_line", "cat <<\ncw task start", plain},
		// h (only the first heredoc is asserted)
		{"h_two_markers", "cat <<A <<B\nx\nA\ny\nB\ncw task start", splitTestHeredocWant{bodies: []string{"x"}, firstOnly: true, notCall: "x"}},
		// i
		{"i_multiline_quote", "echo 'a\n<<EOF b'\ncw task start", plain},
		// R4b (contract revision 5): arithmetic and heredoc nesting.
		// R4b-1: << inside nested parentheses of $(( ... )) is a shift.
		{"r4b_1_nested_parens_in_arithmetic", "echo $(( 1 + (2 << x) ))\ncw task start", plain},
		// R4b-2: $(( ... )) is closed before <<EOF, so that is a heredoc.
		{"r4b_2_heredoc_after_closed_arithmetic", "echo $((1<<2)) <<EOF\ncw task start\nEOF\ncw task verify", heredocA},
		// R4b-3: << after a quoted operand inside $(( ... )) is a shift.
		{"r4b_3_quoted_operand_in_arithmetic", "echo $(( \"1\" << y ))\ncw task start", plain},
		// R4b-4: << inside (( ... )) is a shift.
		{"r4b_4_arithmetic_command", "(( x << y ))\ncw task start", plain},
		// R4b-5: an unclosed quote in a heredoc marker does not leak into the
		// following lines.
		{"r4b_5_unclosed_marker_quote_does_not_leak", "cat <<'EOF\nx\nEOF\ncat <<END\ncw task start\nEND\ncw task verify",
			splitTestHeredocWant{bodies: []string{"x", "cw task start"}, verifyCall: true}},
		// R4b-6: a $( closed on the next line leaves no nesting behind.
		{"r4b_6_substitution_closed_next_line", "echo $(\n) 'q' cat <<EOF\nbody1\nEOF\ncat <<END\ncw task start\nEND\ncw task verify",
			splitTestHeredocWant{bodies: []string{"body1", "cw task start"}, verifyCall: true}},
		// R4b-7 and R4b-8 (amendment 2): a $(( ... )) that opens and closes
		// inside a double-quoted string returns to that string, so the
		// string's closing " ends it and the following <<EOF is a heredoc.
		{"r4b_7_arithmetic_in_double_quotes_then_heredoc", "echo \"$((1))\" && cat <<EOF\ncw task start\nEOF\ncw task verify", heredocA},
		{"r4b_8_shift_in_double_quotes_then_heredoc", "x=\"$(( 2 << 1 ))\"; cat <<EOF\ncw task start\nEOF\ncw task verify", heredocA},
	}
}

// R3 and R4b: heredoc marker recognition, including arithmetic and heredoc
// nesting.
func TestSplitHeredocMarkers(t *testing.T) {
	t.Parallel()
	for _, c := range splitTestHeredocCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			result, work := split(c.cmd)
			sh := result.Commands
			if work > splitTestWorkBound(c.cmd) {
				t.Errorf("work = %d, want <= %d", work, splitTestWorkBound(c.cmd))
			}
			bodies := splitTestBodies(sh)
			if c.want.firstOnly {
				if len(bodies) == 0 || bodies[0] != c.want.bodies[0] {
					t.Errorf("heredoc bodies = %q, want the first to be %q", bodies, c.want.bodies[0])
				}
			} else if !reflect.DeepEqual(bodies, c.want.bodies) {
				t.Errorf("heredoc bodies = %q, want %q", bodies, c.want.bodies)
			}
			if !c.want.firstOnly {
				if got := splitTestHasCall(sh, "cw", "task", "start"); got != c.want.startCall {
					t.Errorf("call cw task start present = %v, want %v; calls %q", got, c.want.startCall, splitTestCallFields(sh))
				}
				if c.want.verifyCall && !splitTestHasCall(sh, "cw", "task", "verify") {
					t.Errorf("no call starts with cw task verify; calls %q", splitTestCallFields(sh))
				}
			}
			if c.want.notCall != "" && splitTestHasCall(sh, c.want.notCall) {
				t.Errorf("heredoc body word %q is a call; calls %q", c.want.notCall, splitTestCallFields(sh))
			}
		})
	}
}

// R4c: for any command, split does not panic, its work stays within
// 4*len+16, and its result equals Split's. Run as a normal test, it checks
// the seeds: the R4a inputs at n = 20 and every command of
// TestSplitHeredocMarkers.
//
// The Split == split check is not a check of splitting: Split is defined as
// split's first result, so it cannot detect a defect inside split. It guards
// that Split keeps returning exactly split's result if either of them is
// changed later.
func FuzzSplitWork(f *testing.F) {
	for _, d := range splitTestDeepRows() {
		f.Add(d.cmd(20))
	}
	for _, c := range splitTestHeredocCases() {
		f.Add(c.cmd)
	}
	f.Fuzz(func(t *testing.T, command string) {
		commands, work := split(command)
		if bound := splitTestWorkBound(command); work > bound {
			t.Errorf("work = %d, want <= 4*len+16 = %d (len %d)", work, bound, len(command))
		}
		if exported := Split(command); !reflect.DeepEqual(exported, commands) {
			t.Errorf("Split(%q) differs from split's result", command)
		}
	})
}

// splitTestSink keeps the R4d split result alive across the second reading.
var splitTestSink Result

// R4d (contract revision 5, amendments 5 and 6): splitting a deeply nested
// input allocates at most 300 bytes per input byte more than splitting a
// control input whose parentheses all close. With n = 16000 it builds, before
// any measurement, the deep input Repeat("( ", n) + Repeat("\n", n) (the
// deep_lines input: n "(" and no ")", so nesting depth n on every following
// line) and the control input Repeat("( ) ", n/2) + Repeat("\n", n) (n/2 "("
// and n/2 ")", so nesting depth 0 on the following lines), and fails if their
// lengths differ. The two inputs do not have the same parentheses; what is
// equal is the input length, the number of parenthesis bytes (n), the spaces
// (n), the newlines (n) and the number of command separators on the first
// line. For each input it runs runtime.GC twice, reads
// runtime.MemStats.TotalAlloc, calls split once (keeping the result alive in
// splitTestSink) and reads TotalAlloc again; the difference is alloc(input).
// It asserts that the excess alloc(deep) - alloc(control), a signed number
// that can be negative, is at most 300 * len(deep).
//
// Its purpose is to catch allocation that grows with the nesting depth on every
// line, in both modes, such as a splitter that copies its whole nesting state
// for every line without counting it as work; it is not a tight allocation
// budget. Both inputs make the splitter do the same line handling, so the
// control cancels the allocation that does not depend on nesting depth, and the
// check holds with and without the race detector and is never skipped.
// Measured excess in bytes per input byte at n = 16000 (48,000 input bytes):
// this package's splitter, about 0 in both modes, because directly nested
// subshells share one frame, and about 95 in both modes with one 56-byte frame
// per open parenthesis. The splitter in internal/usagelog that this package
// replaced: about 4 without -race and about -25 to +40 under -race, and about
// -145 to +190 under -race (standard deviation about 60) in its noisiest
// variant (a simpler separator pattern whose line handling stayed in the
// regexp's backtracking regime under -race), so 300 was about 1.6x the largest
// reading seen. A splitter that copies its nesting state per line in a compact
// form (one bit per nesting level), about 570-720 in both modes, and any copy
// that keeps each level's kind needs more; one that copies it per line as a
// byte slice, about 5,460 in both modes; one that copies it per line as linked
// frames, about 318,850 in both modes.
//
// It does not call t.Parallel: TotalAlloc is process-wide.
func TestSplitDeepNestingAllocations(t *testing.T) {
	const (
		n          = 16000
		perByteMax = 300
	)
	// Build both inputs before any measurement.
	deep := strings.Repeat("( ", n) + strings.Repeat("\n", n)
	control := strings.Repeat("( ) ", n/2) + strings.Repeat("\n", n)
	if len(deep) != len(control) {
		t.Fatalf("deep input is %d bytes but control input is %d bytes; R4d needs inputs of the same length", len(deep), len(control))
	}
	measure := func(command string) int64 {
		var before, after runtime.MemStats
		splitTestSink = Result{}
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)
		splitTestSink, _ = split(command)
		runtime.ReadMemStats(&after)
		return int64(after.TotalAlloc - before.TotalAlloc)
	}
	allocDeep := measure(deep)
	allocControl := measure(control)
	splitTestSink = Result{}
	excess := allocDeep - allocControl
	if excess > perByteMax*int64(len(deep)) {
		t.Errorf("alloc(deep) = %d bytes, alloc(control) = %d bytes: excess %.1f bytes per input byte (%d input bytes), want <= %d",
			allocDeep, allocControl, float64(excess)/float64(len(deep)), len(deep), perByteMax)
	}
}
