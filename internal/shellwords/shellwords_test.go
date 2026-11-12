package shellwords_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// render writes each simple command on a line of its own: its scope moves
// (-Leave +Enter), its lines, its words' Text separated by |, and each
// redirection as <Op Target> with a heredoc's body after =.
func render(commands []shellwords.SimpleCommand) string {
	var out strings.Builder
	for _, c := range commands {
		texts := make([]string, len(c.Words))
		for i, w := range c.Words {
			texts[i] = w.Text
		}
		fmt.Fprintf(&out, "-%d+%d L%d-%d [%s]", c.Leave, c.Enter, c.Line, c.EndLine, strings.Join(texts, "|"))
		for _, r := range c.Redirects {
			fmt.Fprintf(&out, " <%s %s", r.Op, r.Target.Text)
			if r.Op == "<<" || r.Op == "<<-" {
				fmt.Fprintf(&out, "=%q", r.Body)
			}
			out.WriteString(">")
		}
		out.WriteString("\n")
	}
	return out.String()
}

func TestSplit(t *testing.T) {
	// Oracle for each row: the simple commands a POSIX shell reads from the
	// command before it expands anything, with quotes removed from the words,
	// written as render writes them.
	rows := []struct {
		name, command, want string
	}{
		{"separators inside quotes stay in the word", `echo "todo; git commit | x && y" 'a&b'`,
			"-0+0 L0-0 [echo|todo; git commit | x && y|a&b]\n"},
		{"quotes and escapes are resolved", `cd "/a b" it"'"s x\ y "q\"t" $'a\tb' "\$HOME"`,
			"-0+0 L0-0 [cd|/a b|it's|x y|q\"t|a\tb|$HOME]\n"},
		{"every control operator ends a command", "a; b & c && d || e | f |& g\nh",
			"-0+0 L0-0 [a]\n-0+0 L0-0 [b]\n-0+0 L0-0 [c]\n-0+0 L0-0 [d]\n-0+0 L0-0 [e]\n-0+0 L0-0 [f]\n-0+0 L0-0 [g]\n-0+0 L1-1 [h]\n"},
		{"a comment runs to the end of its line", "echo a # b; git push\necho c#d",
			"-0+0 L0-0 [echo|a]\n-0+0 L1-1 [echo|c#d]\n"},
		{"an escaped separator is text", `echo foo\; git commit`,
			"-0+0 L0-0 [echo|foo;|git|commit]\n"},
		{"a line continuation joins the lines", "git \\\n  push",
			"-0+0 L0-1 [git|push]\n"},
		{"redirections are not words", "cd /x 2>/dev/null >out 2>&1 <in >>log &>all",
			"-0+0 L0-0 [cd|/x] <2> /dev/null> <> out> <2>& 1> << in> <>> log> <&> all>\n"},
		{"expansions keep their text", `x=${a:-"b c"} $((1 << 2)) $HOME ~/p`,
			"-0+0 L0-0 [x=${a:-\"b c\"}|$((1 << 2))|$HOME|~/p]\n"},
		{"a quoted heredoc body is text", "cat > n.md <<'EOF'\ngit push origin main\n$(git push)\nEOF\ngit status",
			"-0+0 L0-0 [cat] <> n.md> <<< EOF=\"git push origin main\\n$(git push)\">\n-0+0 L4-4 [git|status]\n"},
		{"an unquoted heredoc body holds substitutions", "cat <<EOF\ngit push\n$(git commit)\nEOF\nls",
			"-0+0 L0-0 [cat] <<< EOF=\"git push\\n$(git commit)\">\n-0+1 L2-2 [git|commit]\n-1+0 L4-4 [ls]\n"},
		{"two heredocs on one line", "cat <<A <<-B\nx\nA\ny\n\tB\nz",
			"-0+0 L0-0 [cat] <<< A=\"x\"> <<<- B=\"y\">\n-0+0 L5-5 [z]\n"},
		{"a << with no delimiter word is text", "cat << 1",
			"-0+0 L0-0 [cat|<<|1]\n"},
		{"arithmetic shifts are not heredocs", "echo $((1<<n)) && (( x << y ))\ngit push",
			"-0+0 L0-0 [echo|$((1<<n))]\n-0+0 L0-0 [(( x << y ))]\n-0+0 L1-1 [git|push]\n"},
		{"subshells and substitutions move the scope", "(cd x; (git commit)) && echo `pwd` \"$(a $(b))\" <(c)",
			"-0+1 L0-0 [cd|x]\n-0+1 L0-0 [git|commit]\n-2+1 L0-0 [pwd]\n-1+2 L0-0 [b]\n-1+0 L0-0 [a|$()]\n-1+1 L0-0 [c]\n-1+0 L0-0 [echo|``|$()|<()]\n"},
		{"a heredoc inside a substitution", "echo \"$(cat <<EOF\ngit push\nEOF\n)\" && git status",
			"-0+1 L0-0 [cat] <<< EOF=\"git push\">\n-1+0 L0-3 [echo|$()]\n-0+0 L3-3 [git|status]\n"},
		{"an unmatched closing parenthesis only separates", "cd x) && git push",
			"-0+0 L0-0 [cd|x]\n-0+0 L0-0 [git|push]\n"},
		{"an unclosed quote runs to the end", "echo 'a; git push\ngit commit",
			"-0+0 L0-1 [echo|a; git push\ngit commit]\n"},
		{"an empty command line has no commands", " ; \n# only a comment\n", ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := render(shellwords.Split(row.command)); got != row.want {
				t.Errorf("Split(%q) =\n%s\nwant\n%s", row.command, got, row.want)
			}
		})
	}
}

func TestSplitKeepsRawWords(t *testing.T) {
	// Oracle: Raw is the word exactly as written, so a reader can tell a
	// quoted $HOME from an unquoted one.
	got := shellwords.Split(`cd '$HOME' "$HOME" $HOME`)
	want := []shellwords.Word{{Text: "cd", Raw: "cd"}, {Text: "$HOME", Raw: "'$HOME'"}, {Text: "$HOME", Raw: `"$HOME"`}, {Text: "$HOME", Raw: "$HOME"}}
	if len(got) != 1 || fmt.Sprint(got[0].Words) != fmt.Sprint(want) {
		t.Fatalf("Split = %+v, want one command with words %+v", got, want)
	}
}
