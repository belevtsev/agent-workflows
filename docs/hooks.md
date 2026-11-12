# Plugin hooks

`hooks/hooks.json` registers two hooks for the agent-workflows plugin. Claude
Code runs them, and so does Codex once you trust them; [Codex](#codex) covers
what differs there. Both run `cw`; when `cw` is not on `PATH` they degrade to printing the
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
commands a Bash input runs, and Codex ignores the condition and runs it for every
shell command). `cw hook pre-publish` then finds each `git commit`
and `git push` in the command and the directory it runs in, and exits at once,
without output, when there is none.

The gate asks a cooperative agent to stop when it skipped a step; it is not a
defence against an agent that sets out to bypass it. See the
[threat model](../SECURITY.md#threat-model) and [Known bypasses](#known-bypasses).

`cw publish` applies the same gate itself, before it commits or pushes: it runs
the same search for open records (over the task's own store and the store
below), so the gate also holds where the hook does not run, such as under Codex
before you trust its hooks. It refuses with exit 3 `open-record` instead of a deny. The hook does not inspect `cw publish`:
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
the same repository. `DIR` may start with `~` or `~/`, `$HOME`, `${HOME}`,
`$PWD` or `${PWD}`, unquoted or in double quotes: `~` and `$HOME` are the
hook's `HOME`, which the agent's shell shares, and `$PWD` is the directory in
effect, so `cd "$PWD"`, `cd ~/proj` and `git -C "$HOME/proj"` are followed.
Quoted and escaped characters are literal, so `git -C "…/com~apple~CloudDocs/proj"`
and `cd '/tmp/price$5'` are followed as written. A bare `cd` moves to `HOME`;
`cd -L DIR`, `cd -P DIR` and `cd -- DIR` move to `DIR`, and `pushd -n DIR`
stays where it is. A git `-C DIR` is also checked as plain text, unexpanded,
as earlier versions of the hook read it.

The hook never skips a git command after a directory change it cannot
follow: `cd -`, `cd ""`, `popd`, `pushd` without a directory, a directory
stack entry such as `cd +1`, other `cd` and `pushd` options, `cd -P` to a
path with `..`, and a `DIR` the hook cannot resolve. A `DIR` cannot be
resolved when it is another variable (`cd $OTHER`), `~user`, an unquoted
glob, an unquoted `$HOME` or `$PWD` the shell may split (a value with a
blank, or a command that sets `IFS`), or `~` or `$HOME` in a command that
assigns, exports, unsets or reads `HOME` (`HOME=/x`, `export HOME=/x`,
`unset HOME`, `read HOME`); likewise `$PWD` where `PWD` is set, and in a
double-quoted `sh -c` or `eval` string, where the outer shell has already
expanded it. After such a change, later git commands are checked against the
session's working directory until an absolute `cd` or `-C`, and a record
that gates them there asks instead of denying, naming the change
([Asking instead of denying](#asking-instead-of-denying)). A script or
program that runs git itself (`./release.sh`, `make publish`) is not seen,
because the command text does not name git.

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
v3.0.0-rc.1, has no `packs` or `test_rules`, or binds the removed v1 Go rule
(`go.package-test/v1`). `cw` refuses to read it, so
neither `cw task verify` nor `cw task abandon` can close it, but it still gates
by the rules above, judged from its newest decodable revision. The deny reason names its
directory: move that directory out of the task store, then start a new task
with `cw task start`. Such a record that a release from v2.0.66 to v2.0.96
verified as complete is finished and does not gate, since those releases
recorded no source digest.

### Records that cannot be read

Any other record that `cw` cannot load is judged by its newest revision file
that still decodes. Such records include a revision that is not JSON (such as
after `echo '{' > <task>/record-000002.json`), a revision file or task
directory that other users can read, a broken revision chain, and a record a
newer `cw` wrote. When that revision says the record governs the repository
by the rules above, and no readable record gates, the hook asks
(`permissionDecision: "ask"`) instead of letting the command through, with a
reason that names the task directory and the load error, and `cw publish`
refuses with exit 3 `open-record`. To clear it, remove the task directory or
move it out of the task store; for a record from a newer `cw`, upgrade `cw`
instead. A task directory in which no revision decodes is ignored
([Known bypasses](#known-bypasses)). Under Codex the command goes ahead,
unless it also raises an ask that Codex denies
([Asking instead of denying](#asking-instead-of-denying), [Codex](#codex)).

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
it. It also holds for the record version: every revision this release writes
is version 4, which `cw` v3.0.0-rc.1 cannot read
([task record format](development.md#task-record-format)).

### Asking instead of denying

These cases ask for confirmation (`permissionDecision: "ask"`) instead of
denying; a deny for any git command in the same Bash input wins, and asks in
one input are joined. Codex cannot ask, so there the first three are denies
and the last two let the command run, unless joined with one of the first
three ([Codex](#codex)).

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
- A commit or push after a directory change the hook cannot follow, such as
  `cd $OTHER && git commit`, `cd - && git commit` or `popd && git push`,
  when a record gates the session's working directory, where the hook checks
  it instead ([Recognised commands](#recognised-commands)). The reason names
  the change as written and each gating record.
- A commit or push from a repository where no readable record gates, but a
  record that governs it cannot be read
  ([Records that cannot be read](#records-that-cannot-be-read)). The gate
  also works out the first two asks as if that record did not exist: a push
  of uncommitted changes over the verified records beside it, or a
  substantial change with no other record, worded "no readable cw task
  record". Either one is joined to this ask in the same reason.
- A commit or push the gate would judge while the task store cannot be located
  or read ([Task store](#task-store)).

### Task store

The hook reads records from `$CW_TASK_STORE` when it is set, else from the
state directory `cw task start` writes to (`$XDG_STATE_HOME/agent-workflows/workflow-tasks`,
else `~/.local/state/agent-workflows/workflow-tasks`). `cw task start` does not
read `CW_TASK_STORE`; give it the same directory with `--store` when you use
the variable. `cw publish` reads both the store of the task it publishes (the
task directory's parent) and this store, once each when they are the same.

Without `$CW_TASK_STORE`, the hook cannot locate the store when
`$XDG_STATE_HOME` is relative, which names no store
([paths](operations.md#paths)), or when the home directory cannot be
resolved. It then asks before each `git commit` and `git push` it would
otherwise judge, instead of letting the command through unchecked, with a
reason that names the cause: "XDG_STATE_HOME must be an absolute path; set it,
or set CW_TASK_STORE", or "cw cannot locate the task store (resolve home
directory); set CW_TASK_STORE to it". It asks the same way when the store
exists but cannot be listed, such as a store directory at mode 0000 or a
regular file in its place, with the reason "cw cannot read the task store
(ERROR); make it a directory you can read, or set CW_TASK_STORE to one". Under
Codex the command goes ahead ([Codex](#codex)). Commands that the rules below
let through without reading the store, such as one outside a Git repository,
still go ahead. A store that does not exist holds no records.

## Fail-open rules

The gate only ever blocks or asks on a positive decision. `cw hook pre-publish`
exits 0 without output, and the command proceeds, when:

- the plugin options `enforce_verify` and `small_task_prompt` are both `false`
  (`enforce_verify=false` alone removes the deny, the uncommitted-push ask and
  the ask for a record that cannot be read, which then counts as no record);
- the command contains no `git commit` or `git push` the hook recognises;
- the directory the command runs in is not inside a Git repository;
- the task store holds no governing record and the change does not look
  substantial;
- anything fails internally: unreadable input, a task directory in which no
  revision decodes or that cannot be listed, a failed source capture, a missing
  `git`, a git command that fails or times out. Two failures ask instead: a
  governing record that cannot be read
  ([Records that cannot be read](#records-that-cannot-be-read)) and a task
  store that cannot be located or read ([task store](#task-store)). A record
  written by v2.0.66 to v2.0.96 still denies: `cw` refuses it at its first
  revision, so it gates even when a later revision is damaged;
- under Codex, either of those two asks, unless it is joined with an ask that
  Codex turns into a deny: Codex cannot ask, and a deny would block the user on
  a file or setting they may not be able to fix from there ([Codex](#codex)).

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
| `cd - && git commit -am x`, `popd && git commit -am x`, `cd $OTHER && git commit -am x` and the other directory changes the hook cannot follow ([Recognised commands](#recognised-commands)), from a session directory that no record gates | Allowed. The hook checks the git command against the session's working directory, so it asks only when a record gates that directory. |
| A `cd` that fails, followed by `;`, `\|\|` or a newline: `cd ~/proj; git commit -am x` where `~/proj` does not exist, bash's `cd ""`, `cd ${HOME}x`, `cd /missing \|\| git commit -am x` | Allowed. The hook judges the directory the `cd` names; when the `cd` fails, git runs where the shell stayed. |
| `HOME=/x cd ~/proj && git push` | Allowed from a session directory that no record gates. The shell expands `~` with the old `HOME`, before the assignment applies to `cd`, but the hook treats `HOME` as unknown in a command that assigns it ([Recognised commands](#recognised-commands)). |
| `G=git; $G commit -am x` | Allowed. Git named through a variable is not recognised. |
| `git -c alias.ci=commit ci -am x`, or any other git alias | Allowed. Only the `commit` and `push` subcommands are checked. |
| `xargs git commit …`, `find … -exec git commit …` | Allowed. Git run as the argument of a command other than the wrappers in [Recognised commands](#recognised-commands) is not recognised. |
| `git cherry-pick`, `git revert`, `git merge`, `git merge --continue`, `git pull`, `git rebase --continue`, `git am`, `git commit-tree` followed by `git update-ref` | Allowed. These create or take in commits without `git commit`. `git merge --squash` is not one of them: it makes no commit, and the `git commit` that follows is denied. |
| `gh pr create --fill`, `make commit`, `./release.sh`, `python3 -c 'import subprocess; subprocess.run(["git", "commit", …])'` | Allowed. The hook does not parse a program's own arguments or what a script runs, so git run from inside them is not seen. |
| Staged bytes that differ from the verified working tree: `git add`, then restore the verified bytes on disk, then `git commit -m x` | Allowed once the task's verify passed on the working tree. The gate digests the working tree, not the index, so a commit without `-a` can hold staged bytes that were never verified. Only the later push asks, because the working tree then differs from the commit. |
| Git run outside the agent's shell tool (a terminal, an editor), under Codex before you trust its hooks, or commits made through a GitHub MCP tool or the GitHub API | Not checked: the hook reads only shell commands, Codex runs no plugin hook you have not trusted, and an API call is not a git command. `cw publish` applies the gate itself wherever it runs. |
| A commit from another clone of the repository, or from a worktree the record does not govern | Allowed. A record governs its own worktree root and, when it was started in the main checkout, that checkout's linked worktrees; a record started in a linked worktree governs only that worktree ([Which records gate](#which-records-gate)). |
| An older `cw` on `PATH` that cannot read the record | Allowed. Its hook skips a record it cannot read, such as one with `amendments`, `packs` or `test_rules`, or, under `cw` v3.0.0-rc.1, any version 4 record ([Which source counts](#which-source-counts)). The current hook asks about a record from a newer `cw` instead ([Records that cannot be read](#records-that-cannot-be-read)). |
| A task directory in which no revision decodes, such as `echo '{' > <task>/record-000001.json` for a task with one revision | Allowed. With no revision to read, the hook cannot tell which repository the task belongs to, so it ignores the directory. A damaged later revision asks instead ([Records that cannot be read](#records-that-cannot-be-read)). |
| `chmod 000 <task>`, or `chmod 000` on every revision file of a task (its only one, for a new task) | Allowed. A task directory the hook cannot list, or whose revision files it cannot open, has no revision to read and is ignored, like one in which no revision decodes. |
| A newest revision that decodes but names another `baseline.root`, has `plan.kind` `small` or carries `abandonment`, such as one copied in from another task | Allowed. The hook selects a task by its newest decodable revision and loads the whole chain only for a task that this revision says governs, so the revision hides an open record even though its chain would not load. |
| A record that cannot be read, under Codex: `echo '{' > <task>/record-000002.json` | Allowed. Codex cannot ask, and the hook lets the command go ahead there rather than block the user on a file it cannot judge ([Codex](#codex)). |
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
Codex has no plugin configuration: start Codex with the variable instead, as
in `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false codex` ([Codex](#codex)).

## Codex

Codex runs both hooks from the installed plugin's `hooks/hooks.json`. What
follows was recorded on 2026-11-12 with codex-cli 0.167.2, `codex exec` and a
wrapper in place of `cw` that saved each hook's input and environment;
[`codex-pre-tool-use.json`](../internal/cli/testdata/codex-pre-tool-use.json)
is the recorded PreToolUse input, with paths replaced by placeholders.

- **Trust.** Codex runs a plugin hook only after you trust it: before that,
  neither hook ran, and `codex exec` did not say so.
  `--dangerously-bypass-hook-trust` runs untrusted hooks for one invocation.
  Per the Codex hooks documentation, you trust hooks with `/hooks` in the CLI
  or **Review hooks** in the desktop app, and trust is recorded against the
  hook's hash, so review again when an update changes `hooks/hooks.json`. No
  feature flag is needed: `codex features list` shows `hooks` stable and on,
  and `plugin_hooks` removed.
- **Input.** The PreToolUse input names the shell tool `Bash` and holds `cwd`
  and `tool_input.command`, as Claude Code's does, plus `turn_id` and `model`,
  which Codex documents as its own extensions. Claude Code's PreToolUse input
  has neither, so `cw` takes the two together as the sign that Codex ran the
  hook; one of them alone is not enough.
- **Matcher.** Codex matches `Bash` but ignores the `if` condition, so
  `cw hook pre-publish` runs for every shell command; it exits at once, without
  output, for a command with no `git commit` or `git push`.
- **Decisions.** A deny blocks the command, and the agent sees
  `Command blocked by PreToolUse hook:` and the reason. No output lets the
  command run. An ask is not supported: Codex ran the command without asking
  and told the agent nothing.
- **Environment.** Codex sets `CLAUDE_PLUGIN_ROOT` and `CLAUDE_PLUGIN_DATA`
  (also as `PLUGIN_ROOT` and `PLUGIN_DATA`) to the plugin's copy under
  `$CODEX_HOME/plugins/cache` and its data directory under
  `$CODEX_HOME/plugins/data`. It sets no `CLAUDE_PLUGIN_OPTION_*` variable, and
  the hook inherits the environment Codex was started with.
- **Session start.** On `startup` the session-start hook adds
  `global/AGENTS.md` to the session, so the trusted plugin also loads the
  working conventions.

So under Codex the pre-publish hook turns three asks into denies: an
unrecorded substantial change, a push with uncommitted changes over a
verified record, and a commit or push after a directory the hook cannot
resolve ([Asking instead of denying](#asking-instead-of-denying)). The
reason starts `agent-workflows: Codex cannot ask for confirmation`, carries the
question Claude Code would ask, and says to put it to the user, who goes ahead
by running the command outside Codex. Every reason under Codex ends by naming
the option variables.

The asks for a task record that cannot be read
([Records that cannot be read](#records-that-cannot-be-read)) and for a task
store that cannot be located or read ([Task store](#task-store)) do not become
denies: a deny would block the user on a file or setting they may not be able
to fix from Codex, so under Codex the hook prints nothing and the command goes
ahead, as Codex runs any ask. A relative `XDG_STATE_HOME` is one of these, so
it no longer denies there. When one of them is joined with one of the
denied asks, the command is denied: a push of uncommitted changes beside a
record that cannot be read, or a substantial change whose only record cannot
be read.

The plugin options are environment variables under Codex: export
`CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY`, `CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT`
or `CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR` in the shell that starts Codex. In the
recorded run, `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` let through a commit
that the hook otherwise denied.
