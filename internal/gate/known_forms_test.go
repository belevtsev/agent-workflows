package gate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/belevtsev/agent-workflows/v3/internal/gate"
	"github.com/belevtsev/agent-workflows/v3/internal/taskflow"
)

// TestPrePublishKnownForms holds one row per known command form, with the
// decision the gate gives it while the repository has a gating record. Later
// changes to command recognition add rows here, so a change in any known
// form shows up as a failing row. {R} is the gated repository, {S} a gated
// repository whose path holds a space, and {O} a directory outside both.
func TestPrePublishKnownForms(t *testing.T) {
	repo := canonicalDir(t)
	spaced := filepath.Join(canonicalDir(t), "my proj")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := canonicalDir(t)
	open := func(id string) []taskflow.OpenRecord {
		return []taskflow.OpenRecord{record(id, "Open", "it has no passing cw task verify")}
	}
	store := &fakeStore{open: map[string][]taskflow.OpenRecord{repo: open("task-k"), spaced: open("task-s")}}
	rows := []struct {
		name    string
		cwd     string
		command string
		want    gate.Permission
	}{
		// Oracle for these rows: nothing in the command commits or pushes,
		// and every program in it only prints, searches or writes files, so
		// the gate neither denies nor asks.
		{"separator inside a quoted argument", "{R}", `echo "todo; git commit later"`, gate.Allow},
		{"pipe inside a quoted search pattern", "{R}", `rg "foo|git commit" docs`, gate.Allow},
		{"git push in a heredoc body", "{R}", "cat > notes.md <<'EOF'\nRelease steps:\ngit push origin main\nEOF", gate.Allow},

		// Oracle for the rest: each command runs a git commit or push in a
		// gated repository under bash, zsh or dash, so the gate denies.
		{"plain commit", "{R}", "git commit -m x", gate.Deny},
		{"commit after a quoted separator", "{R}", `echo "todo; later" && git commit -m "fix; git push"`, gate.Deny},
		{"push after a heredoc", "{R}", "cat > notes.md <<'EOF'\ngit push origin main\nEOF\ngit push origin main", gate.Deny},
		{"push after a pipeline with a quoted pattern", "{R}", `rg "foo|bar" docs | head -1 && git push`, gate.Deny},
		{"commit inside a quoted substitution", "{R}", `echo "$(git commit -m x)"`, gate.Deny},

		// Intended changes: the shellwords reading resolves quoted paths and
		// keeps a cd inside sh -c in its own shell.
		{"cd to a quoted path with a space", "{O}", `cd "{S}" && git commit -m x`, gate.Deny},
		{"git -C a quoted path with a space", "{O}", `git -C "{S}" push`, gate.Deny},
		{"env -C a quoted path with a space", "{O}", `env -C "{S}" git push`, gate.Deny},
		{"a cd inside sh -c stays in its shell", "{R}", "sh -c 'cd {O}'; git commit -m x", gate.Deny},

		// Heredoc delimiters that are not a plain name: the shell ends the
		// body at the line equal to the unquoted word.
		{"delimiter with a hyphen", "{R}", "cat > /dev/null <<EOF-X\nbody\nEOF-X\ngit push", gate.Deny},
		{"quoted delimiter with a hyphen", "{R}", "cat > /dev/null <<'EOF-X'\nbody\nEOF-X\ngit push", gate.Deny},
		{"quoted delimiter with hyphens before a commit", "{R}", "cat > /dev/null <<'END-OF-MSG'\nbody\nEND-OF-MSG\ngit commit -m x", gate.Deny},
		{"delimiter with a dot", "{R}", "cat > /dev/null <<EOF.\nbody\nEOF.\ngit push", gate.Deny},
		{"delimiter with an inner backslash", "{R}", "cat > /dev/null <<E\\OF\nbody\nEOF\ngit push", gate.Deny},
		{"quoted delimiter with a space", "{R}", "cat > /dev/null <<'EOF X'\nbody\nEOF X\ngit push", gate.Deny},
		{"heredoc in a substitution closed on its delimiter line", "{R}", "echo \"$(cat <<EOF\nmsg\nEOF)\"; git push", gate.Deny},

		// Heredoc bodies whose text would open a frame past the delimiter.
		{"body with an unclosed ${", "{R}", "cat > /dev/null <<EOF\n${\nEOF\ngit push", gate.Deny},
		{"body with a quote inside ${...}", "{R}", "cat > /dev/null <<EOF\n${USER:-it's me}\nEOF\ngit push", gate.Deny},
		{"body with an unclosed $((", "{R}", "cat > /dev/null <<EOF\nprice $((1+\nEOF\ngit push", gate.Deny},

		// A single quote inside a double-quoted ${...} is literal in zsh and
		// dash.
		{"quote inside ${...} in double quotes before a push", "{R}", `echo "${X:-it's unset}"; git push`, gate.Deny},
		{"quote inside ${...} in double quotes before a commit", "{R}", `echo "${X:-it's unset}" && git commit -m x`, gate.Deny},
		{"lone quote inside ${...} in double quotes", "{R}", `echo "${X:-'}"; git push`, gate.Deny},

		// Rare forms.
		{"(( closed by a single )", "{R}", "((git push) )", gate.Deny},
		{"(( closed by ) and ;", "{R}", "((git push); true)", gate.Deny},
		{"$(( closed by a single )", "{R}", "$((git push) )", gate.Deny},
		{"echo $(( closed by a single )", "{R}", "echo $((git push) )", gate.Deny},
		{"case inside a quoted substitution", "{R}", `y="$(case x in x) git push;; esac)"`, gate.Deny},
		{"carriage return before #", "{R}", "echo a\r#b; git push", gate.Deny},
		{"env -S with a single-quoted string", "{R}", "env -S 'git push'", gate.Deny},
		{"env -S with a double-quoted string", "{R}", `env -S "git commit -m x"`, gate.Deny},
		{"env --split-string=", "{R}", "env --split-string='git push'", gate.Deny},
		{"(( read as nested subshells by dash", "{R}", "sh -c '((git push))'", gate.Deny},
		{"sh -c nested past the depth the gate reads", "{R}", nestedShellC(9, "x; git push"), gate.Deny},

		// Text that another program runs as shell commands.
		{"heredoc piped to a shell", "{R}", "cat <<'EOF' | sh\ngit push\nEOF", gate.Deny},
		{"heredoc read by a shell", "{R}", "bash <<'EOF'\necho start\ngit push\nEOF", gate.Deny},
		{"quoted text piped to a shell", "{R}", `echo "x; git push" | sh`, gate.Deny},
		{"eval of a heredoc", "{R}", "eval \"$(cat <<'EOF'\ntrue; git push\nEOF\n)\"", gate.Deny},
		{"git rebase --exec", "{R}", "git rebase --exec 'make; git push' HEAD~1", gate.Deny},
	}
	paths := strings.NewReplacer("{R}", repo, "{S}", spaced, "{O}", elsewhere)
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			git := &fakeGit{}
			git.repository(repo)
			git.repository(spaced)
			command := paths.Replace(row.command)
			got := decide(command, paths.Replace(row.cwd), true, true, git, store)
			if got.Permission != row.want {
				t.Fatalf("Decide(%q) = %q (reason %d bytes: %q), want %q", command, got.Permission, len(got.Reason), got.Reason, row.want)
			}
		})
	}
}

// nestedShellC wraps command in depth levels of sh -c "...", escaping each
// level for the double quotes around it.
func nestedShellC(depth int, command string) string {
	escape := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "$", `\$`, "`", "\\`")
	for range depth {
		command = `sh -c "` + escape.Replace(command) + `"`
	}
	return command
}
