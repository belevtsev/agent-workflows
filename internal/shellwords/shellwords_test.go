package shellwords_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/shellwords"
)

// render writes each simple command on a line of its own: its scope moves
// (-Leave +Enter), its lines, its words' Text separated by |, and each
// redirection as <Op Target> with a heredoc's body after =. An uncertain
// result ends with a line "? REASON".
func render(result shellwords.Result) string {
	var out strings.Builder
	for _, c := range result.Commands {
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
	if result.Uncertain != "" {
		out.WriteString("? " + result.Uncertain + "\n")
	}
	return out.String()
}

func TestSplit(t *testing.T) {
	// Oracle for each row: the simple commands a POSIX shell reads from the
	// command before it expands anything, with quotes removed from the words,
	// written as render writes them; no "?" line, because every form in these
	// rows is one Split reads as a shell does.
	rows := []struct {
		name, command, want string
	}{
		{"separators inside quotes stay in the word", `echo "todo; git commit | x && y" 'a&b'`,
			"-0+0 L0-0 [echo|todo; git commit | x && y|a&b]\n"},
		{"quotes and escapes are resolved", `cd "/a b" it"'"s x\ y "q\"t" $"l c" "\$HOME"`,
			"-0+0 L0-0 [cd|/a b|it's|x y|q\"t|l c|$HOME]\n"},
		{"every control operator ends a command", "a; b & c && d || e | f |& g\nh",
			"-0+0 L0-0 [a]\n-0+0 L0-0 [b]\n-0+0 L0-0 [c]\n-0+0 L0-0 [d]\n-0+0 L0-0 [e]\n-0+0 L0-0 [f]\n-0+0 L0-0 [g]\n-0+0 L1-1 [h]\n"},
		{"only spaces and tabs separate words", "echo a\r#b\vc; git push",
			"-0+0 L0-0 [echo|a\r#b\vc]\n-0+0 L0-0 [git|push]\n"},
		{"a comment runs to the end of its line", "echo a # b; git push\necho c#d",
			"-0+0 L0-0 [echo|a]\n-0+0 L1-1 [echo|c#d]\n"},
		{"an escaped separator is text", `echo foo\; git commit`,
			"-0+0 L0-0 [echo|foo;|git|commit]\n"},
		{"a line continuation joins the lines", "git \\\n  push",
			"-0+0 L0-1 [git|push]\n"},
		{"redirections are not words", "cd /x 2>/dev/null >out 2>&1 <in >>log &>all",
			"-0+0 L0-0 [cd|/x] <2> /dev/null> <> out> <2>& 1> << in> <>> log> <&> all>\n"},
		{"expansions keep their dollar and braces", `x=${a:-"b c"} ${CW} $((1 << 2)) $HOME ~/p`,
			"-0+0 L0-0 [x=${a:-b c}|${CW}|$((1 << 2))|$HOME|~/p]\n"},
		{"a quote inside braces in double quotes is text", `echo "${X:-it's}"; git push`,
			"-0+0 L0-0 [echo|${X:-it's}]\n-0+0 L0-0 [git|push]\n"},
		{"a quoted heredoc body is text", "cat > n.md <<'EOF'\ngit push origin main\n$(git push)\nEOF\ngit status",
			"-0+0 L0-0 [cat] <> n.md> <<< EOF=\"git push origin main\\n$(git push)\">\n-0+0 L4-4 [git|status]\n"},
		{"an unquoted heredoc body holds substitutions", "cat <<EOF\ngit push\n$(git commit)\nEOF\nls",
			"-0+0 L0-0 [cat] <<< EOF=\"git push\\n$(git commit)\">\n-0+1 L2-2 [git|commit]\n-1+0 L4-4 [ls]\n"},
		{"the delimiter forms", "a <<\"A\" <<\\B <<-C\nx\nA\ny\nB\nz\n\t\tC\nw",
			"-0+0 L0-0 [a] <<< A=\"x\"> <<< B=\"y\"> <<<- C=\"z\">\n-0+0 L7-7 [w]\n"},
		{"arithmetic shifts are not heredocs", "echo $((1<<n)) \"$(( (x) << y ))\"\ngit push",
			"-0+0 L0-0 [echo|$((1<<n))|$(( (x) << y ))]\n-0+0 L1-1 [git|push]\n"},
		{"subshells and substitutions move the scope", "(cd x; (git commit)) && echo `pwd` \"$(a $(b))\" <(c)",
			"-0+1 L0-0 [cd|x]\n-0+1 L0-0 [git|commit]\n-2+1 L0-0 [pwd]\n-1+2 L0-0 [b]\n-1+0 L0-0 [a|$()]\n-1+1 L0-0 [c]\n-1+0 L0-0 [echo|``|$()|<()]\n"},
		{"a heredoc inside a substitution", "echo \"$(cat <<EOF\ngit push\nEOF\n)\" && git status",
			"-0+1 L0-0 [cat] <<< EOF=\"git push\">\n-1+0 L0-3 [echo|$()]\n-0+0 L3-3 [git|status]\n"},
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

func TestSplitReportsFormsItDoesNotFollow(t *testing.T) {
	// Oracle for each row: a shell may run a command here that Split's
	// reading misses or misplaces, so the result names why it is uncertain.
	rows := []struct {
		name, command, reason string
	}{
		{"delimiter with a hyphen", "cat <<'END-OF-MSG'\nx\nEND-OF-MSG\ngit commit -m x", "a heredoc delimiter that is not a plain name"},
		{"delimiter with a dot", "cat <<EOF.\nx\nEOF.\ngit push", "a heredoc delimiter that is not a plain name"},
		{"delimiter with an inner backslash", "cat <<E\\OF\nx\nEOF\ngit push", "a heredoc delimiter that is not a plain name"},
		{"delimiter with a space", "cat <<'EOF X'\nx\nEOF X\ngit push", "a heredoc delimiter that is not a plain name"},
		{"delimiter with an unclosed quote", "cat <<'EOF\nx\nEOF\ngit push", "a heredoc delimiter with an unclosed quote"},
		{"<< with a digit", "cat << 1\nx\n1\ngit push", "a << without a delimiter name"},
		{"<< at the end of a line", "cat <<\ngit push", "a << without a delimiter name"},
		{"heredoc without its delimiter line", "cat <<EOF\nbody\ngit push", "a heredoc without its delimiter line"},
		{"delimiter line with a closing parenthesis", "echo \"$(cat <<EOF\nmsg\nEOF)\"; git push", "a heredoc body line that nearly matches its delimiter"},
		{"delimiter line with a carriage return", "cat <<EOF\nx\nEOF\r\ngit push\nEOF", "a heredoc body line that nearly matches its delimiter"},
		{"heredoc with no line after it", "cat <<EOF", "a heredoc without a body"},
		{"substitution running past the delimiter", "cat <<EOF\nprice $((1+\nEOF\ngit push", "a quote or substitution in a heredoc body runs past its delimiter"},
		{"heredoc inside a heredoc body", "cat <<A\n$(cat <<B\nx\nB\n)\nA\ngit push", "a heredoc inside a heredoc body"},
		{"heredoc body starting inside a substitution", "cat <<EOF $(\necho)\nx\nEOF\ngit push", "a heredoc's body starts inside a substitution"},
		{"(( command", "(( x << y ))", "a (( command, which dash reads as nested subshells"},
		{"(( closed by a single )", "((git push) )", "a (( command, which dash reads as nested subshells"},
		{"$(( closed by a single )", "echo $((git push) )", "a (( or $(( that ) closes without ))"},
		{"case", `y="$(case x in x) git push;; esac)"`, "a case command"},
		{"escape in a $'...' string", `git $'pu\x73h'`, "a backslash escape in a $'...' string"},
		{"escaped backquote in a backquote substitution", "echo `echo \\`git push\\``", "an escaped backquote inside a backquote substitution"},
		{"unmatched )", "cd x) && git push", "an unmatched )"},
		{"unclosed quote", `echo "unterminated; git push`, "an unclosed quote, substitution or subshell"},
		{"unclosed substitution", `echo $(git push`, "an unclosed quote, substitution or subshell"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			if got := shellwords.Split(row.command).Uncertain; got != row.reason {
				t.Errorf("Split(%q).Uncertain = %q, want %q", row.command, got, row.reason)
			}
		})
	}
}

func TestSplitReadsTheShellsDelimiter(t *testing.T) {
	// Oracle: the shell ends a heredoc at the line equal to its unquoted
	// delimiter word, so the command after that line is a command even when
	// the delimiter is not a plain name.
	for _, command := range []string{
		"cat <<'END-OF-MSG'\nx\nEND-OF-MSG\ngit push",
		"cat <<EOF-X\nx\nEOF-X\ngit push",
		"cat <<E\\OF\nx\nEOF\ngit push",
		"cat <<'EOF X'\nx\nEOF X\ngit push",
		"cat <<EOF\nx\n EOF\nEOF\ngit push",
	} {
		commands := shellwords.Split(command).Commands
		if last := commands[len(commands)-1]; len(last.Words) != 2 || last.Words[1].Text != "push" {
			t.Errorf("Split(%q) ends with %+v, want git push", command, last)
		}
	}
}

func TestSplitKeepsRawWords(t *testing.T) {
	// Oracle: Raw is the word exactly as written, so a reader can tell a
	// quoted $HOME from an unquoted one.
	got := shellwords.Split(`cd '$HOME' "$HOME" $HOME`).Commands
	want := []shellwords.Word{{Text: "cd", Raw: "cd"}, {Text: "$HOME", Raw: "'$HOME'"}, {Text: "$HOME", Raw: `"$HOME"`}, {Text: "$HOME", Raw: "$HOME"}}
	if len(got) != 1 || fmt.Sprint(got[0].Words) != fmt.Sprint(want) {
		t.Fatalf("Split = %+v, want one command with words %+v", got, want)
	}
}
