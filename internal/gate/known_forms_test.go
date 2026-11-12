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
// HOME is {R}'s parent directory, and {N} is {R}'s name in it.
func TestPrePublishKnownForms(t *testing.T) {
	repo := canonicalDir(t)
	spaced := filepath.Join(canonicalDir(t), "my proj")
	if err := os.Mkdir(spaced, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := canonicalDir(t)
	home := filepath.Dir(repo)
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
		// and every command in it is one the gate trusts (echo, rg, cat with
		// a heredoc as input), so the gate neither denies nor asks.
		{"separator inside a quoted argument", "{R}", `echo "todo; git commit later"`, gate.Allow},
		{"pipe inside a quoted search pattern", "{R}", `rg "foo|git commit" docs`, gate.Allow},
		{"git push in a heredoc body", "{R}", "cat <<'EOF'\nRelease steps:\ngit push origin main\nEOF", gate.Allow},

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
		// A heredoc whose output goes to a file keeps the flat split's deny:
		// only input redirections, /dev/null and 2>&1 keep the trust.
		{"heredoc written to a file", "{R}", "cat > notes.md <<'EOF'\nRelease steps:\ngit push origin main\nEOF", gate.Deny},
		// Round 2: a single quote is a plain byte inside $(( )) and $[ ], and a quote,
		// blank or substitution inside an unquoted ${...} is not read.
		{"array subscript in $(( ))", "{R}", "echo $(( 'a[$(git push)]' ))", gate.Deny},
		{"single quotes in $(( ))", "{R}", "echo $(('$(git push)'))", gate.Deny},
		{"single quotes in a quoted $(( ))", "{R}", "echo \"$(( '$(git push)' ))\"", gate.Deny},
		{"backquotes in single quotes in $(( ))", "{R}", "echo $(( 1 + '`git push`' ))", gate.Deny},
		{"$[ ]", "{R}", "echo $[ '$(git push)' ]", gate.Deny},
		{"single quotes in a $(( )) conditional", "{R}", "echo $(( X ? 1 : '$(git push)' ))", gate.Deny},
		{"single quotes in an assigned $(( ))", "{R}", "x=$(( '$(git push)' ))", gate.Deny},
		{"$(( )) inside ${...}", "{R}", "echo ${X:-$(( '$(git push)' ))}", gate.Deny},
		{"subscript in ${#...}", "{R}", "echo ${#X[ '$(git push)' ]}", gate.Deny},
		{"subscript in ${...}", "{R}", "echo ${X[ '$(git push)' ]}", gate.Deny},
		{"offset with a blank in ${...}", "{R}", "echo ${X: '$(git push)'}", gate.Deny},
		{"offset in ${...} under zsh", "{R}", "echo ${X:'$(git push)'}", gate.Deny},
		// Round 2: git commands other than commit and push run configured commands.
		{"git fetch --upload-pack", "{R}", "git fetch --upload-pack='true; git push #' .", gate.Deny},
		{"git pull --upload-pack", "{R}", "git pull --upload-pack='true; git push #' .", gate.Deny},
		{"git grep -O", "{R}", "git grep -O'true; git push #' x", gate.Deny},
		{"git config core.fsmonitor then git status", "{R}", "git config core.fsmonitor 'true; git push #'; git status", gate.Deny},
		{"git config diff.external then git diff", "{R}", "git config diff.external 'true; git push #'; git diff", gate.Deny},
		{"git config core.editor then git tag -a", "{R}", "git config core.editor 'true; git push #'; git tag -a v9", gate.Deny},
		// Round 2: assignments, exported or prefixed, reach git or a later expansion.
		{"GIT_EXTERNAL_DIFF prefix", "{R}", "GIT_EXTERNAL_DIFF='true; git push #' git diff", gate.Deny},
		{"GIT_SSH_COMMAND prefix", "{R}", "GIT_SSH_COMMAND='true; git push #' git fetch ssh://h/r", gate.Deny},
		{"exported GIT_EXTERNAL_DIFF", "{R}", "export GIT_EXTERNAL_DIFF='true; git push #'; git diff", gate.Deny},
		{"env GIT_EXTERNAL_DIFF", "{R}", "env GIT_EXTERNAL_DIFF='true; git push #' git diff", gate.Deny},
		{"GIT_CONFIG_COUNT prefix", "{R}", "GIT_CONFIG_COUNT=1 GIT_CONFIG_KEY_0=core.fsmonitor GIT_CONFIG_VALUE_0='true; git push #' git status", gate.Deny},
		{"assignment expanded by ${X@P}", "{R}", "X='$(git push)'; echo ${X@P}", gate.Deny},
		{"assignment evaluated in $(( ))", "{R}", "X='a[$(git push)]'; echo $((X))", gate.Deny},
		{"assignment expanded by ${!X}", "{R}", "X='a[$(git push)]'; echo ${!X}", gate.Deny},
		{"assignment evaluated by [[ ]]", "{R}", "X='a[$(git push)]'; [[ X -eq 0 ]]", gate.Deny},
		// Round 2: builtins that evaluate an array subscript.
		{"[[ -eq ]] subscript", "{R}", "[[ 'a[$(git push)]' -eq 0 ]]", gate.Deny},
		{"[[ -v ]] subscript", "{R}", "[[ -v 'a[$(git push)]' ]]", gate.Deny},
		{"test -v subscript", "{R}", "test -v 'a[$(git push)]'", gate.Deny},
		{"read subscript", "{R}", "read 'a[$(git push)]' <<< 1", gate.Deny},
		{"printf -v subscript", "{R}", "printf -v 'a[$(git push)]' x", gate.Deny},
		{"declare subscript", "{R}", "declare 'a[$(git push)]=1'", gate.Deny},
		{"declare -i subscript", "{R}", "declare -i n='a[$(git push)]'", gate.Deny},
		{"export subscript", "{R}", "export 'a[$(git push)]=1'", gate.Deny},
		// Round 2: output written to a file that git or another program then runs.
		{"post-checkout hook written by cat", "{R}", "cat > .git/hooks/post-checkout <<'EOF'\n#!/bin/sh\ngit push origin main\nEOF\nchmod +x .git/hooks/post-checkout; git checkout -q -b z", gate.Deny},
		{"fsmonitor appended to .git/config", "{R}", "cat >> .git/config <<'EOF'\n[core]\n\tfsmonitor = \"true; git push #\"\nEOF\ngit status", gate.Deny},
		{"post-checkout hook written by printf", "{R}", "printf '%s\\n' '#!/bin/sh' 'true\ngit push origin main' > .git/hooks/post-checkout; chmod +x .git/hooks/post-checkout; git switch -q -c z", gate.Deny},
		{"reference-transaction hook", "{R}", "cat > .git/hooks/reference-transaction <<'EOF'\n#!/bin/sh\n[ -e .p ] || { : > .p; git push origin main; }\nEOF\nchmod +x .git/hooks/reference-transaction; git branch q", gate.Deny},
		{"hook in core.hooksPath", "{R}", "mkdir -p h; cat > h/post-checkout <<'EOF'\n#!/bin/sh\ngit push origin main\nEOF\nchmod +x h/post-checkout; git config core.hooksPath h; git checkout -q -b z", gate.Deny},
		{"post-index-change hook", "{R}", "cat > .git/hooks/post-index-change <<'EOF'\n#!/bin/sh\ngit push origin main\nEOF\nchmod +x .git/hooks/post-index-change; git add a.txt", gate.Deny},
		{"post-checkout hook for git worktree add", "{R}", "cp /dev/null x; cat > .git/hooks/post-checkout <<'EOF'\n#!/bin/sh\ngit push origin main\nEOF\nchmod +x .git/hooks/post-checkout; git worktree add -q ../wt", gate.Deny},
		{".git/config written by sort -o", "{R}", "printf '%s\\n' '[core]' 'fsmonitor = \"true; git push ../remote.git HEAD:main #\"' | sort -o .git/config; git status", gate.Deny},
		{".git/config written by git log --output", "{R}", "git log -1 --format='[core]%n%x09fsmonitor = \"true; git push ../remote.git HEAD:main #\"' --output=.git/config; git status", gate.Deny},
		// Round 2: programs that run another program.
		{"rg --pre", "{R}", "cat > p <<'EOF'\n#!/bin/sh\ngit push\nEOF\nchmod +x p; rg --pre ./p x a.txt", gate.Deny},
		{"sort --compress-program", "{R}", "printf '%s\\n' 'true; git push' {1..50000} | sort -S 64K --compress-program=sh -o /dev/null", gate.Deny},

		// Directories named through HOME or PWD: bash, zsh and dash expand a
		// leading unquoted ~ and a leading $HOME, ${HOME}, $PWD or ${PWD},
		// unquoted or in double quotes, so the git command runs in {R}.
		{"cd to $PWD in double quotes", "{R}", `cd "$PWD" && git commit -m x`, gate.Deny},
		{"cd to a path under ~", "{O}", "cd ~/{N} && git commit -m x", gate.Deny},
		{"cd to a path under $HOME", "{O}", "cd $HOME/{N} && git push", gate.Deny},
		{"cd to a path under ${HOME} in double quotes", "{O}", `cd "${HOME}/{N}" && git push`, gate.Deny},
		{"cd ~, then a relative cd", "{O}", "cd ~ && cd {N} && git commit -m x", gate.Deny},
		{"pushd to a path under ~", "{O}", "pushd ~/{N} && git push", gate.Deny},
		{"git -C a path under ~", "{O}", "git -C ~/{N} commit -m x", gate.Deny},
		{"git -C $PWD after a cd", "{O}", `cd {R} && git -C "$PWD" push`, gate.Deny},
		{"env -C a path under ~", "{O}", "env -C ~/{N} git commit -m x", gate.Deny},
		{"cd to a path under ~ inside sh -c", "{O}", "sh -c 'cd ~/{N} && git push'", gate.Deny},
		{"a push after the cd beside an unresolved commit", "{R}", `(cd $OTHER && git commit -m x); cd "$PWD" && git push`, gate.Deny},

		// Directories the gate cannot resolve: the git command is checked in
		// the session's directory, {R}, and asks. Where the shell does not
		// expand the word, or the command may change HOME, PWD or IFS
		// first, expanding it would deny.
		{"cd to another variable", "{R}", "cd $OTHER && git commit -m x", gate.Ask},
		{"a relative cd after another variable", "{R}", "cd $OTHER && cd sub && git push", gate.Ask},
		{"cd to $HOME in single quotes", "{R}", "cd '$HOME/{N}' && git commit -m x", gate.Ask},
		{"cd to an escaped ~", "{R}", `cd \~/{N} && git push`, gate.Ask},
		{"cd to another user's home", "{R}", "cd ~nobody && git push", gate.Ask},
		{"cd to a glob", "{R}", "cd /tmp/* && git push", gate.Ask},
		{"cd to a path under ~ after HOME is assigned", "{R}", "HOME=/tmp; cd ~/{N} && git push", gate.Ask},
		{"cd to an unquoted $HOME after IFS is assigned", "{R}", "IFS=/; cd $HOME/{N} && git push", gate.Ask},
		{"cd to an unquoted $PWD that holds a space", "{S}", "cd $PWD && git push", gate.Ask},
		{"git -C another variable", "{R}", "git -C $OTHER push", gate.Ask},
		{"env -C another variable", "{R}", "env -C $OTHER git commit -m x", gate.Ask},

		// Directory changes the gate does not follow, OLDPWD and the
		// directory stack among them: as above, checked in {R} and asked.
		{"cd -", "{R}", "cd - && git commit -m x", gate.Ask},
		{"cd -P", "{R}", "cd -P {O} && git commit -m x", gate.Ask},
		{"cd --", "{R}", "cd -- {O} && git push", gate.Ask},
		{"cd to an empty word", "{R}", `cd "" ; git push`, gate.Ask},
		{"popd", "{R}", "popd && git push", gate.Ask},
		{"pushd -n", "{R}", "pushd -n {O} && git push", gate.Ask},
		{"pushd without a directory", "{R}", "pushd && git push", gate.Ask},
		// A bare cd moves to HOME, which the gate follows.
		{"a bare cd, then a relative cd", "{O}", "cd && cd {N} && git push", gate.Deny},
	}
	paths := strings.NewReplacer("{R}", repo, "{S}", spaced, "{O}", elsewhere, "{N}", filepath.Base(repo))
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			git := &fakeGit{}
			git.repository(repo)
			git.repository(spaced)
			command := paths.Replace(row.command)
			got := decideIn(gate.Input{Command: command, CWD: paths.Replace(row.cwd), Home: home, Enforce: true, Prompt: true}, git, store)
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
