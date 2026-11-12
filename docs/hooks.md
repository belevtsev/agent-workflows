# Plugin hooks

`hooks/hooks.json` registers two Claude Code hooks for the agent-workflows
plugin. Both run `cw`; when `cw` is not on `PATH` they degrade to printing the
conventions file or doing nothing. No hook uses a script file: every command is
inline, which keeps the repository's automation policy (`go test ./internal/skillcheck`)
satisfied.

## Session start

On `startup`, `resume`, `clear` and `compact`, `cw hook session-start` returns
`global/AGENTS.md` as the session's additional context. With the plugin option
`session_doctor` set to `true` (`CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR=true`), it
appends the one-line `cw doctor --summary` for the session's directory. That
summary is produced with a 5 second deadline and cached for one hour under
`${CLAUDE_PLUGIN_DATA}/doctor/<sha256 of the directory>.json`; when it cannot be
produced in time, the conventions are returned alone.

When `cw` is not on `PATH`, the hook command prints the file followed by one
line that starts `agent-workflows: the commit and push verify gate is inactive
because cw was not found on PATH.` and goes on with the `go install` command for
the plugin's release tag and a link to [Get cw](operations.md#get-cw), so the
session knows the gate below is off and can give the user the install command.
When `cw` is present but fails or prints nothing, the hook command prints the
file alone.

## Publication gate

The `PreToolUse` handler runs for every Bash command whose text contains `git`
(the condition `Bash(*git*)`; Claude Code also runs it when it cannot tell which
commands a Bash input runs). `cw hook pre-publish` then finds each `git commit`
and `git push` in the command and the directory it runs in, and exits at once,
without output, when there is none.

The gate asks a cooperative agent to stop when it skipped a step; it is not a
defence against an agent that sets out to bypass it. See the
[threat model](../SECURITY.md#threat-model) and [Known bypasses](#known-bypasses).

`cw publish` applies the same gate itself, before it commits or pushes: it runs
the same search for open records (over the task's own store and the store
below), so the gate holds under Codex, which has no hook, too. It refuses with
exit 3 `open-record` instead of a deny. The hook does not inspect `cw publish`:
its command text names no git, and cw runs git itself.

### Recognised commands

The hook reads the command the way the shell does before it expands anything
(`internal/shellwords`): it splits it into simple commands at `;`, `&&`, `||`,
`|`, `&`, newlines and parentheses, and into words with their quotes and
backslash escapes resolved. Text inside quotes, in a heredoc body or in a
comment is never a command, so `echo "todo; git commit later"`,
`rg "foo|git commit" docs` and `cat <<'EOF'` with `git push` in its body are
allowed.

That reading fails closed, and the set of commands it trusts is deliberately
small. It allows a command that names git outside a command only when every
form in the line is one it reads exactly as bash, zsh and dash do, and every
simple command in the line, inside substitutions too, is one of these:

- `echo`, `printf` without `-v`, `cat`, `rg` without `--pre` or
  `--pre-glob`, `grep`, `jq` and `cd`;
- `git commit` and `git push`, which the gate checks anyway, and `gh pr`;
- the reserved words `if`, `then`, `else`, `fi`, `for`, `do`, `done`,
  `while`, `case`, `esac`, `{`, `}` and `!`.

The command must also hold no `NAME=value` assignment, and no redirection
other than a heredoc or here-string as input, `/dev/null`, or a duplicated
descriptor such as `2>&1`; `cat > notes.md <<'EOF'` writes a file and is not
trusted. Any other command, however harmless, keeps the decision of the
simpler split the hook used before, which reads text in quotes and heredocs
as commands: the hook applies both and denies when either finds a commit or
push. It finds git:

- after leading `NAME=value` assignments, shell keywords (`if`, `then`, `do`,
  `!`, `{` and the like) and the prefixes `builtin`, `exec`, `eval`, `nohup`
  and `noglob`;
- after the wrappers `command`, `env`, `sudo`, `time`, `nice`, `timeout` and
  `stdbuf` with their own options (`command -p`, `env -i`, `env -u NAME`,
  `sudo -u USER`, `nice -n 5`, `timeout 30`, `stdbuf -oL`); `env -C DIR`
  (also `-CDIR` or in a cluster such as `-iC DIR`), `env --chdir=DIR` and
  `sudo -D DIR` move the command to `DIR` (within one wrapper the last
  one wins, as in `env` and `sudo`; nested wrappers such as
  `env -C a env -C b` move it to `a/b`);
- around a `$(...)` or backtick substitution in the same command
  (`GIT_COMMITTER_DATE="$(date -R)" git commit`); the command inside a
  substitution, including one in an unquoted heredoc body, is checked too,
  from the directory in effect where it appears;
- inside a command string given to `sh`, `bash`, `zsh`, `dash` or `ksh` with
  `-c` (also `-lc`, `-e -c`, `-c --` and similar), to `eval` or to
  `env -S`; a string nested more than eight levels deep counts as a commit
  and a push;
- written as a path (`/usr/bin/git`), quoted (`"git"`, `'commit'`) or with a
  backslash that bypasses an alias (`\git`);
- with git's global options, including `-c NAME=VALUE`, `--git-dir`,
  `--work-tree` and `-C DIR`.

The directory starts as the session's working directory and moves with a
preceding `cd DIR` or `pushd DIR`, quoted paths with spaces included, and with
each `-C DIR`; a `cd` inside parentheses, a substitution or an `sh -c` string
ends with them, as in a subshell; redirections on a `cd` are ignored, and
`"$(pwd)"` or `"$(git rev-parse --show-toplevel)"` after `cd` or `-C` stays in
the same repository. The hook fails open: after a directory change it cannot
follow (`cd` with options, `cd -`, `~`, `$VAR`, `popd`) later git commands are
not checked until an absolute `cd` or `-C`, and a `-C DIR` it cannot resolve
(`git -C "$PWD"`, `git -C ~/proj`, `env -C ~/proj git`) lets that command
through. A script or program that runs git itself (`./release.sh`,
`make publish`) is not seen, because the command text does not name git.

### Which records gate

A record governs the command when all of the following hold:

- its `plan.kind` is `substantial`;
- it has not been abandoned with `cw task abandon`;
- its `baseline.root` is the repository's worktree root, or the main worktree's
  root when the command runs in a linked worktree (a record started in the
  main checkout therefore governs every linked worktree; a record started in a
  linked worktree governs that worktree only);
- its latest revision was written within the last 14 days. Any recording
  command (`assess`, `run`, `assign`, `verify` and the others) writes a
  revision, so a task that is being worked on keeps governing however long it
  runs; one nobody has touched for 14 days stops.

Starting a new record does not hide an older one: every governing record gates
until `cw task verify` passes on the source being published, and the hook
denies the command with a reason that names each gating record, why it gates,
and both ways out: `cw task verify --task <dir> --summary`, or
`cw task abandon --task <dir> --reason TEXT` to close a task that is finished
or was given up. An abandoned record accepts no further revisions and never
gates.

A record is finished, and no longer governs, once another governing record
(substantial, not abandoned, written within the 14 days) for the repository
was started, after that verification, from exactly the bytes it was verified
on (the newer record's `baseline_manifest.sha256` equals the
older one's `verification.source_sha256`). Two records started on the same
bytes before either was verified do not finish each other. The usual sequence, verify task A, commit, start
task B, therefore releases A, and B's own record then covers every later
change. A record started over source that changed after A's last passing
verification does not finish A, so starting a fresh record cannot hide
unreviewed edits.

A record written by `cw` v2.0.66 to v2.0.96, or by a v3 build before
v3.0.0-rc.1, has no `packs` or `test_rules`. `cw` refuses to read it, so
neither `cw task verify` nor `cw task abandon` can close it, but it still gates
by the rules above, judged from its latest revision. The deny reason names its
directory: move that directory out of the task store, then start a new task
with `cw task start`. Such a record that a release from v2.0.66 to v2.0.96
verified as complete is finished and does not gate, since those releases
recorded no source digest.

### Which source counts

Only the content of the eligible source files counts: `cw task verify` records
a digest of each file's path, mode and bytes (`verification.source_sha256`),
so staging or committing the verified bytes does not reopen the record, and a
push after the commit goes ahead. Editing any eligible file does reopen it. A
record verified by a `cw` older than this digest stays open until it is
verified again.

The digest is taken where the command runs. For a command in the record's own
worktree that is the record's `baseline.selected_path`; for a command in a
linked worktree of the record's main checkout it is that linked worktree, and
the hook denies, naming both paths, unless its source matches the verified
bytes.

A `cw` release older than these fields rejects the unknown `source_sha256` and
`abandonment` members, so it cannot read a record this release has verified or
abandoned; keep every host that shares a task store on the same `cw`. The same
holds for `amendments`, which `cw task add-checks` writes: an older `cw` cannot
read a record carrying it, and its hook skips such a record instead of gating
it.

### Asking instead of denying

Two cases ask for confirmation (`permissionDecision: "ask"`) instead of
denying; a deny for any git command in the same Bash input wins.

- A push from a repository whose governing records all passed verification,
  while the working tree has uncommitted changes to tracked files: the
  verified bytes may then not be what the pushed commits hold.
- A commit or push from a repository with no governing record at all, when the
  change it publishes meets at least two of: more than 300 changed lines
  (added plus deleted, from `git diff --numstat`), changed files under more than
  one top-level directory (the first path component; files at the repository
  root count for none), and a changed non-test code file with no test file
  changed. A commit publishes the staged changes, or with `-a`/`--all` every
  change to a tracked file; a push publishes the commits since the merge base
  with `@{upstream}`, or without an upstream with `refs/remotes/origin/HEAD`
  (no ask when neither exists). Test files are `*_test.*`, `*_spec.*`,
  `test_*`, `*.test.*`, `*.spec.*`, `*Test.*` and `*Tests.*` names and
  anything under a `test`, `tests`, `testdata`, `__tests__` or `spec`
  directory, in any letter case. The reason suggests
  `cw task start`. Set the plugin option `small_task_prompt` to `false`
  (`CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false`) to turn this prompt off.

### Task store

The hook reads records from `$CW_TASK_STORE` when it is set, else from the
state directory `cw task start` writes to (`$XDG_STATE_HOME/agent-workflows/workflow-tasks`,
else `~/.local/state/agent-workflows/workflow-tasks`). `cw task start` does not
read `CW_TASK_STORE`; give it the same directory with `--store` when you use
the variable. `cw publish` reads both the store of the task it publishes (the
task directory's parent) and this store, once each when they are the same.

A relative `$XDG_STATE_HOME` names no store ([paths](operations.md#paths)).
Without `$CW_TASK_STORE`, the hook then asks before each `git commit` and
`git push` it would otherwise judge, with the reason "XDG_STATE_HOME must be
an absolute path; set it, or set CW_TASK_STORE", instead of letting the
command through unchecked. Commands that the rules below let through without
reading the store, such as one outside a Git repository, still go ahead.

## Fail-open rules

The gate only ever blocks or asks on a positive decision. `cw hook pre-publish`
exits 0 without output, and the command proceeds, when:

- the plugin options `enforce_verify` and `small_task_prompt` are both `false`
  (`enforce_verify=false` alone removes the deny and the uncommitted-push ask);
- the command contains no `git commit` or `git push` the hook recognises;
- the directory the command runs in is not inside a Git repository;
- the task store holds no governing record and the change does not look
  substantial;
- anything fails internally: unreadable input, an unreadable or damaged record,
  a failed source capture, a missing `git`, a git command that fails or times
  out. Damaged records are skipped rather than reported, except a record
  written by v2.0.66 to v2.0.96: `cw` refuses it at its first revision, so it
  gates even when a later revision is damaged. A relative `$XDG_STATE_HOME`
  is the other exception: it asks ([task store](#task-store)).

A 20 second deadline bounds the hook's Git and source-capture work. A record
whose source identity cannot be captured in time is skipped, like a record
whose capture fails; other records can still gate the command.

The hook never exits 2, and the command strings in `hooks/hooks.json` end with
`exit 0` rather than `exec`-ing `cw`, so even a `cw` too old to know the hook
subcommand cannot block. A hook timeout (30 seconds) is treated by Claude Code
as a non-blocking error.

## Known bypasses

The gate is a guardrail for a cooperative agent that skipped a step, not a
defence against an agent that sets out to bypass it (see the
[threat model](../SECURITY.md#threat-model)). Each row below lets a commit or
push through today while the repository has an open substantial task, or lets
`cw task verify` pass on evidence it cannot check.

| Way around the gate | What happens today |
| --- | --- |
| `cd "$PWD" && git commit -am x`, `cd ~/proj && git commit -am x`, `git -C "$PWD" commit -am x`, `git -C ~/proj commit -am x`, `env -C ~/proj git commit -am x`; also `cd -`, `popd` and `cd` with options | Allowed. After a `cd` or `-C` the hook cannot follow, the git command is not checked ([Recognised commands](#recognised-commands)). |
| `G=git; $G commit -am x` | Allowed. Git named through a variable is not recognised. |
| `git -c alias.ci=commit ci -am x`, or any other git alias | Allowed. Only the `commit` and `push` subcommands are checked. |
| `xargs git commit …`, `find … -exec git commit …` | Allowed. Git run as the argument of a command other than the wrappers in [Recognised commands](#recognised-commands) is not recognised. |
| `git cherry-pick`, `git revert`, `git merge`, `git merge --continue`, `git pull`, `git rebase --continue`, `git am`, `git commit-tree` followed by `git update-ref` | Allowed. These create or take in commits without `git commit`. `git merge --squash` is not one of them: it makes no commit, and the `git commit` that follows is denied. |
| `gh pr create --fill`, `make commit`, `./release.sh`, `python3 -c 'import subprocess; subprocess.run(["git", "commit", …])'` | Allowed. The hook does not parse a program's own arguments or what a script runs, so git run from inside them is not seen. |
| Staged bytes that differ from the verified working tree: `git add`, then restore the verified bytes on disk, then `git commit -m x` | Allowed once the task's verify passed on the working tree. The gate digests the working tree, not the index, so a commit without `-a` can hold staged bytes that were never verified. Only the later push asks, because the working tree then differs from the commit. |
| Git run outside Claude Code's Bash tool (Codex, a terminal, an editor), or commits made through a GitHub MCP tool or the GitHub API | Not checked: the hook reads only Bash commands, and an API call is not a git command. `cw publish` applies the gate itself wherever it runs. |
| A commit from another clone of the repository, or from a worktree the record does not govern | Allowed. A record governs its own worktree root and, when it was started in the main checkout, that checkout's linked worktrees; a record started in a linked worktree governs only that worktree ([Which records gate](#which-records-gate)). |
| An older `cw` on `PATH` that cannot read the record | Allowed. Its hook skips a record it cannot read, such as one with `amendments`, `packs` or `test_rules` ([Which source counts](#which-source-counts)). |
| An unreadable newest revision: `echo '{' > <task>/record-000002.json` | Allowed. A task whose record does not load is skipped, and a task directory whose newest revision does not decode is ignored. Removing the file restores the deny. |
| A rewritten record, such as revision 1 with `plan.kind` set to `small` | Allowed. The agent's user can write the store, and the revision hash chain is plain SHA-256 with no key, so a rewritten chain loads as valid. |
| `cw task abandon --task <task> --reason TEXT` | Allowed. An abandoned task never gates, and the deny text names this command as a way out. |
| A task nobody touched for 14 days | Allowed, with no message: the task stops gating 14 days after its latest revision ([Which records gate](#which-records-gate)). |
| A fake `go` earlier on `PATH`, such as a script that prints `ok` and exits 0 | `cw task run` records the check as passed with exit code 0. A check runs the first program of its name on `cw`'s `PATH`, and the Go evidence rule judges the argv, not the binary. Checking the binary's path or hash would not settle it either: the same user can rebuild `cmd/go` from modified source, or keep the binary and edit the standard library in a GOROOT it can write. That is a limit of the design. |
| A plan check that runs `make publish`, `python3 -c …`, `go run …` or `./release.sh` | Allowed by `cw task start`, and `cw task run` runs it with the user's rights, so it can commit and push; the hook never sees it. `cw task start` refuses shells, `env`, `sudo`, network and deployment tools, git other than inspection commands and `cw publish` by name, which guards against accidents, not against a program that does the same. |
| Self-declared agent IDs | One agent can record itself as implementer, test engineer and reviewer under different `agent_id` strings. Independence compares those strings with each other and with the plan's `coordinator_id`; nothing authenticates them. |
| The off switches and failures: `enforce_verify=false`, `cw` not on `PATH`, an internal error or timeout in the hook | Allowed. The deny text names the `enforce_verify` switch, and the agent can run the `claude plugin configure` command itself: the hook does not inspect it, and the option applies once Claude Code restarts. See [Fail-open rules](#fail-open-rules) and [Disabling the gate](#disabling-the-gate). |

## Disabling the gate

Set the plugin option `enforce_verify` to `false` in the plugin's configuration,
which exports `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` to the hook, and
`small_task_prompt` to `false` to stop the ask for unrecorded changes. Removing
`cw` from `PATH` also disables the gate, because the command checks for `cw`
first and exits 0 when it is absent; the session-start line then says so.
