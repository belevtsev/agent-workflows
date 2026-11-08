package usagelog

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
)

// Behavioural tests for the Bash command splitter written from test contract
// linear-split, revision 5 (requirements R1, R3 and R4). They use only
// splitShell, parseShell, strayScanWork, calls[i].fields, heredocs[i].body,
// heredocs[i].redirect and the work count.

// splitTestBodies returns the heredoc bodies of sh, ignoring redirect entries
// (`> FILE` lines are not heredoc bodies).
func splitTestBodies(sh shellCommand) []string {
	var bodies []string
	for _, h := range sh.heredocs {
		if !h.redirect {
			bodies = append(bodies, h.body)
		}
	}
	return bodies
}

// splitTestHasCall reports whether some call's fields start with prefix.
func splitTestHasCall(sh shellCommand, prefix ...string) bool {
	for _, c := range sh.calls {
		if len(c.fields) < len(prefix) {
			continue
		}
		match := true
		for i, p := range prefix {
			if c.fields[i] != p {
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

func splitTestCallFields(sh shellCommand) [][]string {
	out := make([][]string, 0, len(sh.calls))
	for _, c := range sh.calls {
		out = append(out, c.fields)
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
// What it guards: `work` grows with every byte fed through the call's own quote
// scanner and its clones, with each once-per-line redirect-target search of a
// line that holds a heredoc marker, and (revision 5) with the number of nesting
// levels (open quotes, `(`, `$(`, `$((`, `((`, backquotes) that any operation
// copies or searches. A splitter that rescans the line prefix for every "<<"
// through that scanner (or a clone of it) does work quadratic in the number of
// markers: about 10 million bytes at 2000 words and about 160 million at 8000
// words, against bounds of about 40 KB and 160 KB, and work(4n)/work(n) is
// about 16 instead of about 4. Likewise, the deep_* rows open n nesting levels
// and then end n lines or present n heredoc markers, so a splitter that copies
// the whole nesting state for every line, or searches it for every marker, and
// counts those levels as work, does work quadratic in the depth and fails the
// bound and the ratio. Separately, strayScanWork grows with bytes fed through a
// quote scanner that has no counter at all, so a rescan through such a scanner
// makes it non-zero. deep_arithmetic also checks that no << inside $(( ... ))
// is taken as a heredoc, and the other deep rows check their exact body counts.
//
// What it does not detect: anything outside the counted scanner bytes, the
// once-per-line redirect-search count and the counted nesting levels is
// invisible to both counts. Examples (not a complete list): a rescan through a
// scanner clone given its own private counter; prefix logic that does not use
// the quote scanner (for example counting quote bytes in line[:at] with
// strings.Count); a quadratic marker matcher; other quadratic steps such as
// rebuilding kept lines; copying the nesting state without adding the copied
// levels to work, which only R4d's allocation bound
// (TestSplitShellDeepNestingAllocations) can catch, and only when the copy
// allocates; and searching the nesting state without adding the searched
// levels to work, which no test here detects.
func TestSplitShellWorkLinear(t *testing.T) {
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
				sh, w := splitShell(cmd)
				work[i] = w
				if w > splitTestWorkBound(cmd) {
					t.Errorf("%d words: work = %d, want <= 4*len+16 = %d (len %d)", words, w, splitTestWorkBound(cmd), len(cmd))
				}
				if w <= 0 {
					t.Errorf("%d words: work = %d, want > 0 (the scanner reads the command)", words, w)
				}
				if in.cwStart && !splitTestHasCall(sh, "cw", "task", "start") {
					t.Errorf("%d words: no call starts with cw task start; calls %d", words, len(sh.calls))
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
			// Bytes fed through a quote scanner with no counter at all
			// (process-wide, atomic): correct splitting never adds to it.
			if stray := strayScanWork(); stray != 0 {
				t.Errorf("strayScanWork() = %d after the splits, want 0 (bytes fed through a quote scanner with no counter)", stray)
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

// splitTestHeredocCases are the cases of TestSplitShellHeredocMarkers (R3 and
// R4b); FuzzSplitShellWork also uses their commands as seeds.
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
	}
}

// R3 and R4b: heredoc marker recognition, including arithmetic and heredoc
// nesting.
func TestSplitShellHeredocMarkers(t *testing.T) {
	t.Parallel()
	for _, c := range splitTestHeredocCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sh, work := splitShell(c.cmd)
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

// R4c: for any command, splitShell does not panic, its work stays within
// 4*len+16, and its result equals parseShell's. Run as a normal test, it
// checks the seeds: the R4a inputs at n = 20 and every command of
// TestSplitShellHeredocMarkers.
func FuzzSplitShellWork(f *testing.F) {
	for _, d := range splitTestDeepRows() {
		f.Add(d.cmd(20))
	}
	for _, c := range splitTestHeredocCases() {
		f.Add(c.cmd)
	}
	f.Fuzz(func(t *testing.T, command string) {
		shell, work := splitShell(command)
		if bound := splitTestWorkBound(command); work > bound {
			t.Errorf("work = %d, want <= 4*len+16 = %d (len %d)", work, bound, len(command))
		}
		if parsed := parseShell(command); !reflect.DeepEqual(parsed, shell) {
			t.Errorf("parseShell(%q) differs from splitShell's result", command)
		}
	})
}

// splitTestSink keeps the R4d split result alive across the second reading.
var splitTestSink shellCommand

// R4d (contract revision 5, amendment 1): splitting the deep_lines input
// allocates at most 400 bytes per input byte. The bound is not a tight
// allocation budget: a linear splitter allocates up to about 190 bytes per
// input byte here. Its purpose is to catch allocation that grows with the
// nesting depth on every line, such as a splitter that copies the whole
// nesting state for every line without counting it as work (about 1,500 bytes
// per input byte at n = 4000 and 5,600 at n = 16000), and it catches that only
// when the copy allocates. It does not call t.Parallel: TotalAlloc is
// process-wide.
func TestSplitShellDeepNestingAllocations(t *testing.T) {
	const perByte = 400
	for _, n := range []int{4000, 16000} {
		command := strings.Repeat("( ", n) + strings.Repeat("\n", n)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		splitTestSink, _ = splitShell(command)
		runtime.ReadMemStats(&after)
		allocated := after.TotalAlloc - before.TotalAlloc
		if allocated > perByte*uint64(len(command)) {
			t.Errorf("n = %d: splitShell allocated %d bytes for %d input bytes: %.1f bytes per input byte, want <= %d",
				n, allocated, len(command), float64(allocated)/float64(len(command)), perByte)
		}
	}
	splitTestSink = shellCommand{}
}
