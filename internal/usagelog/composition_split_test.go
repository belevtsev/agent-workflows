package usagelog

import (
	"reflect"
	"strings"
	"testing"
)

// Behavioural tests for the Bash command splitter written from test contract
// linear-split, revision 1 (requirements R1 and R3). They use only
// splitShell/parseShell, calls[i].fields, heredocs[i].body, heredocs[i].redirect
// and the work count.

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

// R1 (contract revision 2): this work-count test checks that splitting work
// stays linear in the command length. It does not measure time and is not an
// end-to-end timing check. A splitter that rescans the text before each "<<" to
// decide its quote state does work quadratic in the number of markers: at 8000
// words that is tens of millions of bytes against a bound of about 4*40 KB, and
// work(4n)/work(n) is about 16 instead of about 4. A rescan through a scanner
// that no splitShell call counts shows up in strayScanWork, which must stay 0.
func TestSplitShellWorkLinear(t *testing.T) {
	t.Parallel()
	const n = 2000
	inputs := []struct {
		name string
		cmd  func(n int) string
		// cwStart: the command holds a `cw task start` call; quoted: none of the
		// "<<" is a heredoc.
		cwStart, quoted bool
	}{
		{"single_quoted", func(n int) string {
			return "echo '" + splitTestWords(n, " ") + "'\ncw task start --repo r"
		}, true, true},
		{"double_quoted", func(n int) string {
			return "echo \"" + splitTestWords(n, " ") + " \"\ncw task start --repo r"
		}, true, true},
		{"unquoted_cw_first", func(n int) string {
			return "cw task start --repo r\necho " + splitTestWords(n, " ")
		}, true, false},
		{"arithmetic", func(n int) string {
			return "echo $((" + splitTestWords(n, "+") + "))"
		}, false, false},
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
			}
			// Scanner bytes outside any splitShell count (process-wide, atomic):
			// correct splitting never adds to it.
			if stray := strayScanWork(); stray != 0 {
				t.Errorf("strayScanWork() = %d after the splits, want 0 (uncounted quote scanning)", stray)
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

// R3: heredoc marker recognition.
func TestSplitShellHeredocMarkers(t *testing.T) {
	t.Parallel()
	type want struct {
		bodies     []string // non-redirect heredoc bodies, in order; nil means none
		firstOnly  bool     // assert only bodies[0] (case h)
		startCall  bool     // a call starts with cw task start
		verifyCall bool     // a call starts with cw task verify
		notCall    string   // no call's first field is this word ("" = unchecked)
	}
	heredocA := want{bodies: []string{"cw task start"}, verifyCall: true}
	plain := want{startCall: true}
	cases := []struct {
		name, cmd string
		want      want
	}{
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
		{"h_two_markers", "cat <<A <<B\nx\nA\ny\nB\ncw task start", want{bodies: []string{"x"}, firstOnly: true, notCall: "x"}},
		// i
		{"i_multiline_quote", "echo 'a\n<<EOF b'\ncw task start", plain},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			sh, work := splitShell(c.cmd)
			if work > splitTestWorkBound(c.cmd) {
				t.Errorf("work = %d, want <= %d", work, splitTestWorkBound(c.cmd))
			}
			if parsed := parseShell(c.cmd); !reflect.DeepEqual(parsed, sh) {
				t.Errorf("parseShell differs from splitShell:\nparseShell %+v\nsplitShell %+v", parsed, sh)
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
