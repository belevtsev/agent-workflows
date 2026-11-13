# Plugin hooks

`hooks/hooks.json` registers the agent-workflows plugin's hooks for Claude
Code: one at session start and four before each Bash command. Codex runs
`hooks/codex-hooks.json` instead, which `.codex-plugin/plugin.json` names, once
you trust it: the same hooks with `--host codex`; [Codex](#codex) covers what
differs there. Each runs `cw`; when `cw` is not on `PATH` they degrade to printing the
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

Under Codex ([which host](#codex)), a `resume` adds nothing. A resumed
Codex thread keeps all that earlier session starts added, and after a
compaction, which drops it, Codex runs the hook again with `compact`
([Codex](#codex)). `cw` then prints a SessionStart object without
`additionalContext`: printing nothing would make the hook command print the
file. The lines below and the doctor summary also wait for the next startup
or compaction, so a resume never suggests `cw gate install` beside the
`compact` run that Codex can start at the same moment. A thread started before
you trusted the hooks gets the conventions at its next compaction. Claude
Code's `resume` is unchanged.

Between the conventions and that summary, after a blank line, it adds one
short line for each version that is out of step, and one when git's hooks
would catch more than the plugin hook does; the context is paid for in every
session, so there is nothing when all is in order:

- `plugin 3.0.0-rc.3, cw 3.0.0-rc.2: upgrade cw` when the `version` of the
  plugin manifest beside `global/` (`.claude-plugin/plugin.json`, or
  `.codex-plugin/plugin.json` under [Codex](#codex)) and the `cw version`
  differ on major, minor, patch or pre-release. It ends `update the plugin`
  when `cw` is the newer one. A `cw` built between tags (`dev` or a Go
  pseudo-version) is not compared. A `cw` older than v3.0.0-rc.2 prints no
  line.
- `Claude Code 2.1.291, agent-workflows needs 2.1.292: upgrade Claude Code`
  when the client is older than the minimum in the
  [quick start](../README.md#quick-start). No documented hook input or
  variable carries the client version, so `cw` reads `AI_AGENT`, which Claude
  Code sets to a value such as `claude-code_2-1-311_harness` unless the
  environment it starts in already names another agent. Any other value, and
  Codex, give no line.
- `git hooks absent, so a commit or push from a script, an alias or a terminal
  skips the verify gate: suggest cw gate install to the user`, once per task:
  when the session's repository has an open substantial task
  ([Which records gate](#which-records-gate)), verified or not, that has not
  had the line yet, and `cw gate status` reports a hook `absent` from the
  repository's own hooks directory. When the other hooks are installed, the
  line names the absent ones and says what skips the gate without them, as
  `git hook pre-merge-commit absent, so a merge commit made without conflicts
  skips the verify gate: suggest cw gate install to the user` does where an
  older `cw gate install` wrote only `pre-commit` and `pre-push`. `cw` then
  records the suggestion as an empty file
  `${CLAUDE_PLUGIN_DATA}/gate-suggested/<task id>` for each open
  task, so a resume, clear or compact repeats nothing, and only a new task
  brings the line back. Without that directory, or when the file cannot be
  written, the line comes every time. A hook that `cw gate install` did not
  write (`foreign`, such as a hook manager's, which may already run the
  gate), a hooks directory that other repositories use (`shared`, where
  install needs `--shared`), and `enforce_verify` set to `false` give no line;
  `cw doctor` reports the first two
  ([Installing the git hooks](#installing-the-git-hooks)). The session-start
  doctor summary leaves out the `git_hooks` check, which the line covers. The line
  asks the agent to suggest the command, because `cw` writes git's hooks only
  when you ask.

The session's directory, which the last line and the doctor summary need, is
the `cwd` of the hook input on stdin. `cw` reads stdin only when one of them
is on, or under Codex for its `source`, and for at most 2 seconds, then takes
its working directory, which Claude Code and Codex set to the session's
directory. Input it cannot read gives no source, so the context is added.

When `cw` is not on `PATH`, the hook command prints the file followed by one
line that starts `agent-workflows: the commit and push verify gate is inactive
because cw was not found on PATH.` and goes on with the `go install` command for
the plugin's release tag and a link to [Get cw](operations.md#get-cw), so the
session knows the gate below is off and can give the user the install command.
When `cw` is present but fails or prints nothing, the hook command prints the
file alone.

## Publication gate

Three `PreToolUse` handlers run the same command, `cw hook pre-publish`: one for
every Bash command whose text contains `git` (the condition `Bash(*git*)`), one
for every one that contains `abandon` (`Bash(*abandon*)`,
[Abandoning a task](#abandoning-a-task)), and one for every one that contains
`owner-approval` (`Bash(*owner-approval*)`, [Owner approval](#owner-approval)). Claude Code checks a condition
against each command of a compound line, so neither of the last two requires
`cw` in the same command as `abandon` or `owner-approval`, as in
`C=cw; $C task abandon`; quotes inside
the word do not hide it, so `cw task aban''don` runs the second handler too;
and it runs both when it cannot tell which commands a Bash input runs, as for
`echo "$(printf x)"`. Where several handlers run for one command, they reach
the same decision, and the host applies it once
([Recorded host runs](#recorded-host-runs)). Codex ignores the conditions, so
its hooks file has one gate handler, `cw hook pre-publish --host codex`,
without a condition, which runs for every shell command ([Codex](#codex)). `cw hook pre-publish` then
finds each `git commit` and `git push` in the command and the directory it
runs in, each `cw task abandon` and each `--owner-approval` of a `cw` command,
and exits at once, without output, when there is none.

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
denies the command with a reason that names up to three gating records, why
each gates, and both ways out: `cw task verify --task <task-id>`, or, for
a task that is finished or was given up, `cw task abandon --task <task-id> --reason TEXT`,
which needs the user's confirmation ([Abandoning a task](#abandoning-a-task)).
The reason names each task by its ID, which `--task` takes, not by its directory. Past three, it
gives the number of other gating records and the task store they are in.
An abandoned record accepts no further revisions and never gates.

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
by the rules above, judged from its newest decodable revision. When the deny reason names it, it names its
directory: move that directory out of the task store, then start a new task
with `cw task start`. Such a record that a release from v2.0.66 to v2.0.96
verified as complete is finished and does not gate, since those releases
recorded no source digest.

### Abandoning a task

`cw task abandon` closes a task so that it never gates again, so the decision
belongs to the user. With `enforce_verify` on, the hook asks before every
`cw task abandon` it finds (`permissionDecision: "ask"`), naming each task's ID,
plan title and directory, or the directory alone when it cannot read the
record there. Past three tasks, it counts the rest, with the directory they
are in when it knows each one's. It finds the command as it finds git
([Recognised commands](#recognised-commands)): `cw` by any path, after
wrappers and prefixes, inside substitutions, `sh -c` strings and `eval`, with
`--task DIR` or `--task=DIR`, and `DIR` resolved from the directory in effect.
`cw` is not among the trusted commands, so a line that runs it also gets the
simpler split's reading: the hook asks whenever words of the line are `cw`, by
any path, as a value such as `C=cw` or as `go run …/cmd/cw@VERSION`, `task`
and `abandon`, in any order, and names one task it cannot identify for each
`task abandon` beyond those it read. That covers `cw` run by `xargs`,
`find -exec` or `go run`, or through a variable, and also asks about
`cw task abandon --help` and about a line that only mentions the three words
next to a command the gate does not trust, such as
`git add -A && git commit -m "docs: cw task abandon"`. A line without the word
`task`, such as `go test ./cmd/cw -run abandon`, and a line of trusted
commands that only mentions them, such as `echo "cw task abandon"`, are
allowed.

A `git commit` or `git push` that a record gates in the same Bash input is
denied, as before: it runs before the task is closed. Under Codex the ask is a
deny ([Codex](#codex)), and the user runs `cw task abandon` themselves.

### Owner approval

`cw task assign --owner-approval "<the owner's words>"` lets a round past its
stage's cap, or a third fix round on an open finding or requirement, through and
records the words ([round caps](operations.md#round-caps), [fix rounds](operations.md#fix-rounds)),
and `cw task assign` or `cw task run --owner-approval "cost-budget=<words>"` lets
the task go past its budget's stop ([budget stop](operations.md#budget-stop)).
The agent supplies that text, so with `enforce_verify` on the hook asks before
every `cw` command it finds with `--owner-approval TEXT` or
`--owner-approval=TEXT`, quoting each text (three at most, counting the rest),
so a person confirms the words are theirs. Text the shell expands, with a `$`
outside single quotes or a backtick, as in `"$(cat words.txt)"` or `"$W"`, is not
quoted: the ask says the gate cannot read those words. It finds `cw` as it finds
`cw task abandon`, and fails closed the same way: a line whose words include `cw`
and `owner-approval` that it cannot read, such as `C=cw; $C task assign
--owner-approval x`, asks about words it cannot read, while trusted commands
that only mention them, such as `echo`, are allowed. Under
Codex the ask is a deny, and the user runs the command themselves.

### Records that cannot be read

Any other record that `cw` cannot load is judged by its newest revision file
that still decodes. Such records include a revision that is not JSON (such as
after `echo '{' > <task>/record-000002.json`), a revision file or task
directory that other users can read, a broken revision chain, and a record a
newer `cw` wrote. When that revision says the record governs the repository
by the rules above, and no readable record gates, the hook asks
(`permissionDecision: "ask"`) instead of letting the command through, with a
reason that names the task directory and the load error of up to three such
records and counts the others with the task store they are in, and `cw publish`
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
it. It also holds for the record version: every revision this `cw` writes is
version 5, which v3.0.0-rc.2 and older cannot read; the hook of v3.0.0-rc.2
asks about such a record instead of skipping it
([task record format](development.md#task-record-format)).

### Asking instead of denying

These cases ask for confirmation (`permissionDecision: "ask"`) instead of
denying; a deny for any git command in the same Bash input wins, and asks in
one input are joined. Codex cannot ask, so there the first four are denies
and the last two let the command run, unless joined with one of the first
four ([Codex](#codex)). Like the deny, a reason names at most three tasks and
counts the rest.

- A `cw task abandon` ([Abandoning a task](#abandoning-a-task)).
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
  the change as written and the gating records as the deny does.
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
read `CW_TASK_STORE`, and the hook and `cw consult` do not see `--store`: keep
the two the same. When you set the variable, give `cw task start` that
directory with `--store`; when you start a task with `--store DIR`, set
`CW_TASK_STORE=DIR` for the hook and for `cw consult`, which reads a task-bound
request's task record from this store and refuses the request without it. `cw publish` reads both the store of the task it publishes (the
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

## Usage sampling

A fourth `PreToolUse` handler, for every Bash command whose text contains `cw`
and then `task` (`Bash(*cw*task*)`), runs `cw hook task-usage` with its output
discarded, so it never adds a decision to the gate's. Before each `cw task`
command that names a task with `--task DIR`, `--task=DIR` or a task ID, it
imports the session's transcript (`transcript_path` in the hook input: a
Claude Code transcript, or under Codex the session's rollout) within 10
seconds, with cw's built-in price table, as `cw task usage` does, and stores
the sample in that task's record when it matters
([cost budget](operations.md#cost-budget)). The
[budget stop](operations.md#budget-stop) of the `cw task assign` or
`cw task run` that follows then reads it. A task ID is looked up in the task
store that command would use: a `CW_TASK_STORE`, `XDG_STATE_HOME` or `HOME`
assignment before `cw`, then cw's `--state-dir` and `--home`, then the hook's
own environment.

It reads no transcript, stores nothing and exits 0 when the command names no
open task with a budget (none, abandoned, unreadable or started before
profiles), when the import fails, is incomplete, or skips a malformed line or
an unreadable file, as for a broken or missing transcript, and on any other
error; a session's known usage then stays as last sampled. It runs alongside
the gate's handlers and takes the task's lock only to store a sample. Codex
ignores the condition and runs it for every shell command; it exits at once
for a command that names no task. Where Codex has not been told to trust the
plugin's hooks, nothing samples, and the skill runs
`cw task usage --codex-session FILE` itself ([Codex](#codex)). A hook timeout
(15 seconds) stores nothing.

## Fail-open rules

The gate only ever blocks or asks on a positive decision. `cw hook pre-publish`
exits 0 without output, and the command proceeds, when:

- the plugin options `enforce_verify` and `small_task_prompt` are both `false`
  (`enforce_verify=false` alone removes the deny, the uncommitted-push ask,
  the ask before `cw task abandon` and the ask for a record that cannot be
  read, which then counts as no record);
- the command contains no `git commit`, `git push` or `cw task abandon` the
  hook recognises;
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

## Git hooks

`cw hook git pre-commit`, `cw hook git pre-push` and
`cw hook git pre-merge-commit` apply the same gate from git's own `pre-commit`,
`pre-push` and `pre-merge-commit` hooks, so a commit, merge commit or push is
checked however it starts: through an alias, a script, `make` or `xargs`, or
from a terminal. `cw gate install` writes the three hooks into a repository
([Installing the git hooks](#installing-the-git-hooks)), or a hook of your own
can call them. Git runs each in the worktree it publishes from, and `cw`
judges that repository by the rules above:

- `cw hook git` knows which hook runs it only from its subcommand, the hook's
  name, which the installed script passes; it ignores git's arguments and
  stdin. `pre-commit` and `pre-merge-commit` judge a commit, `pre-push` a
  push.
- Git runs `pre-merge-commit`, not `pre-commit`, before `git merge` or
  `git pull` makes a merge commit without conflicts. When a remote-tracking
  ref (`refs/remotes/`) contains every commit the merge takes in, as it
  contains the base branch after `git fetch` or within `git pull`,
  `pre-merge-commit` lets the merge commit through without a check: it takes
  in work already on a remote, not local work that no record covers. That
  holds for any remote-tracking branch, not only the task's base. Git names
  the commits to the hook in `GITHEAD_<object name>` variables, since it
  writes `MERGE_HEAD` only after the hook; when it names none, or
  `git for-each-ref --contains` fails, the merge commit is judged as below. Any
  other merge commit, such as one of a local branch, is judged as
  `pre-commit` judges a commit, so it stops while a record gates, and git
  leaves the merge in progress (`git merge --abort` undoes it). A merge with
  conflicts is committed by `git commit` or `git merge --continue`, which run
  `pre-commit` and are judged as any commit, whatever they merge. A
  fast-forward makes no commit and runs neither
  ([Merging the base branch during a task](operations.md#merging-the-base-branch-during-a-task)).
- While a record gates, the deny reason goes to stderr and `cw` exits 3
  ([denied](operations.md#exit-statuses)); git stops at any non-zero status
  and itself exits 1.
- A git hook cannot ask, so each case in
  [Asking instead of denying](#asking-instead-of-denying) prints its question
  on stderr after `agent-workflows: warning: a git hook cannot ask for
  confirmation`, exits 0 and git goes ahead: a record that cannot be read or
  is from a newer `cw`, a task store that cannot be located or read, a push of
  uncommitted changes over verified records, and a substantial change with no
  record. Git names the directory, so the ask about a directory change cannot
  arise.
- For the substantial-change warning, a commit's change is the index git is
  committing (`GIT_INDEX_FILE`), so `git commit -a` and `git commit PATH`
  count what they record. `pre-merge-commit` gives no such warning: a merge
  commit's change is the incoming branch's, such as the base branch's on
  `git pull --no-rebase`, not work the task records.
- Where `cw hook pre-publish` lets the command through without output
  ([Fail-open rules](#fail-open-rules)), such as after a git failure or at the
  20 second deadline, `cw hook git` exits 0 without output.

The plugin config does not reach git. The hook reads
`CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY` and `CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT`
from git's environment, and `CW_GATE=off` turns it off, as in
`CW_GATE=off git commit -m x`. `git commit --no-verify`,
`git merge --no-verify` (and `git pull --no-verify`) and
`git push --no-verify` skip git's hooks, `cw`'s included, for one command;
`cw hook pre-publish` still checks a `git commit --no-verify` the agent runs.
Git's hook variables, such as `GIT_DIR` and `GIT_INDEX_FILE`, reach the gate's
own git commands, but not source capture, which runs git with a minimal
environment of its own.

### Installing the git hooks

Nothing is installed unless you ask, one repository at a time (`--repo`
defaults to the working directory; a symbolic link is followed first):

```sh
cw gate install --repo /path/to/repo
cw gate status --repo /path/to/repo      # --json: {hooks_path, shared, hooks: [{name, path, state}]}
cw gate uninstall --repo /path/to/repo
```

- **Where.** The hooks go in the directory git runs them from,
  `git rev-parse --git-path hooks`: `.git/hooks`, or `core.hooksPath` when it
  is set. `cw` runs that git without `GIT_DIR`, `GIT_WORK_TREE` and the other
  variables that would select another repository. Linked worktrees share the
  directory, so one install covers every worktree of the repository; only a
  relative `core.hooksPath` resolves in each worktree's own root. When
  `core.hooksPath` names a directory inside the worktree, the new hooks are
  untracked files there, which `git status` lists.
- **Shared directories.** A hooks directory can serve other repositories too:
  `core.hooksPath` set in your global or system git configuration, or a hooks
  directory outside the repository's git directory and work tree. Install
  and uninstall then exit 4 ([refused](operations.md#exit-statuses)) and
  change nothing, unless you pass `--shared`; `cw gate status` reports
  `shared`. Install creates a missing hooks directory only inside the git
  directory, and uninstall removes it again once it is empty; it refuses a
  missing one anywhere else.
- **What.** Each hook is a three-line `/bin/sh` script with mode 0755 that
  passes its own name to `cw hook git`; `pre-commit` is:

  ```sh
  #!/bin/sh
  # agent-workflows: written by cw gate install; cw gate uninstall deletes this file
  command -v cw >/dev/null 2>&1 || exit 0; exec cw hook git pre-commit "$@"
  ```

  Install writes it to a temporary file and links that into place, so an
  interrupted install never leaves part of a hook, and a file another program
  puts there in the meantime is never replaced. On a file system without hard
  links, install fails (exit 1) and changes nothing.

  The hook runs the first `cw` on git's `PATH`. With none, it exits 0 and git goes
  ahead without a check or a message ([Known bypasses](#known-bypasses)). A
  `cw` too old to have `cw hook git`, or, in `pre-merge-commit`, one older than
  `cw hook git pre-merge-commit`, exits with an error, which stops git until you
  upgrade it or run `cw gate uninstall` with the `cw` that installed the hooks.
  An older `cw gate uninstall` does not know `pre-merge-commit` and leaves it
  in place, so every merge commit then fails with `unknown command`: uninstall
  with the newer `cw`, or move `pre-merge-commit` out of the hooks directory
  (`git rev-parse --git-path hooks`). A repository whose hooks an older
  `cw gate install` wrote has no `pre-merge-commit`: `cw gate status` reports it
  `absent` until `cw gate install` runs again.
- **Ownership.** `cw gate status` reports each hook as `installed` (its bytes
  are exactly the script above, and it has an execute bit), `absent`, or
  `foreign` (anything else at that path, including a copy of the script with
  lines of your own). Git skips a hook it cannot execute, so the script
  without an execute bit is `absent`, and the text output says so; install
  restores its mode, and uninstall removes it. Install
  replaces, and uninstall removes, only an installed hook: before either, `cw`
  moves the file aside and checks it again, and puts back a file that changed
  since the first check.
- **A hook that is already there.** `cw gate install` leaves a foreign hook
  unchanged, writes the others, exits 4 ([refused](operations.md#exit-statuses))
  and prints the line to add after the foreign hook's first line:

  ```sh
  if command -v cw >/dev/null 2>&1; then cw hook git pre-commit "$@" </dev/null || exit $?; fi
  ```

  In `pre-push` and `pre-merge-commit`, use `cw hook git pre-push` and
  `cw hook git pre-merge-commit`. Hook managers write the hook
  files themselves, so add the line where the manager reads it: to
  `.husky/pre-commit` for husky, which points `core.hooksPath` at `.husky/_`;
  as a `run:` command in `lefthook.yml` for lefthook; or as a `repo: local`
  hook with `language: system` for the pre-commit framework. `cw gate status`
  then still reports that hook as `foreign`.
- **Reminders.** `cw` never installs the hooks by itself. When a substantial
  task opens in a repository without them, the next session-start context
  carries one line that suggests `cw gate install`, once for that task
  ([Session start](#session-start)), and `cw doctor` reports a `git_hooks`
  check whose detail names the absent hooks, says what skips the gate without
  them (only a merge commit, when just `pre-merge-commit` is absent) and what
  to do. Only the hooks missing while a task is
  open count as a problem, so a repository that simply has no hooks does not
  change `cw doctor --summary`, which exits 0 either way. The doctor summary
  in the session-start context leaves the check out:

  | `cw gate status` | Open substantial task | `git_hooks` status | Listed by `--summary` |
  | --- | --- | --- | --- |
  | any, with `enforce_verify` `false` | either | `not_applicable`, not inspected | No |
  | all three hooks `installed` | either | `configured` | No |
  | a hook `foreign` | either | `detected`, with the hint to add the line above | No |
  | otherwise a hook `absent`, directory `shared` | either | `detected`, with the hint that install needs `--shared` | No |
  | otherwise a hook `absent` | yes | `missing` | Yes, so the summary's status is `attention` |
  | otherwise a hook `absent` | no | `not_applicable` | No |
  | not a repository, or git or the task store cannot be read | either | `unchecked` | No |

## Known bypasses

The gate is a guardrail for a cooperative agent that skipped a step, not a
defence against an agent that sets out to bypass it (see the
[threat model](../SECURITY.md#threat-model)). Each row below lets a commit or
push through today while the repository has an open substantial task, or lets
`cw task verify` pass on evidence it cannot check. In a repository where
`cw gate install` wrote git's hooks, git runs the gate itself before each
`git commit`, merge commit and `git push`, however it was started: the rows that say "Caught
when `cw gate install` has run" no longer let a commit or push through there,
and the last row lists what still gets past git's hooks.

| Way around the gate | What happens today |
| --- | --- |
| `cd - && git commit -am x`, `popd && git commit -am x`, `cd $OTHER && git commit -am x` and the other directory changes the hook cannot follow ([Recognised commands](#recognised-commands)), from a session directory that no record gates | Allowed. The hook checks the git command against the session's working directory, so it asks only when a record gates that directory. Caught when `cw gate install` has run: git runs its hook in the repository it commits to. |
| A `cd` that fails, followed by `;`, `\|\|` or a newline: `cd ~/proj; git commit -am x` where `~/proj` does not exist, bash's `cd ""`, `cd ${HOME}x`, `cd /missing \|\| git commit -am x` | Allowed. The hook judges the directory the `cd` names; when the `cd` fails, git runs where the shell stayed. Caught when `cw gate install` has run: git's hook judges the repository git runs in. |
| `HOME=/x cd ~/proj && git push` | Allowed from a session directory that no record gates. The shell expands `~` with the old `HOME`, before the assignment applies to `cd`, but the hook treats `HOME` as unknown in a command that assigns it ([Recognised commands](#recognised-commands)). Caught when `cw gate install` has run. |
| `G=git; $G commit -am x` | Allowed. Git named through a variable is not recognised. Caught when `cw gate install` has run. |
| `git -c alias.ci=commit ci -am x`, or any other git alias | Allowed. Only the `commit` and `push` subcommands are checked. Caught when `cw gate install` has run: git runs its hooks for the command an alias stands for. |
| `xargs git commit …`, `find … -exec git commit …` | Allowed. Git run as the argument of a command other than the wrappers in [Recognised commands](#recognised-commands) is not recognised. Caught when `cw gate install` has run. |
| `git cherry-pick`, `git revert`, `git merge`, `git merge --continue`, `git pull`, `git rebase --continue`, `git am`, `git commit-tree` followed by `git update-ref` | Allowed. These create or take in commits without `git commit`. `git merge --squash` is not one of them: it makes no commit, and the `git commit` that follows is denied. Caught when `cw gate install` has run: a merge commit, from `git merge`, `git merge --continue` or `git pull`, runs `pre-merge-commit` or, after conflicts, `pre-commit`, except that `pre-merge-commit` lets through a merge of commits on remote-tracking branches ([Git hooks](#git-hooks)). The agent's own `git merge --no-verify`, `git pull --no-verify` or `CW_GATE=off git merge` still gets past both hooks, since the plugin hook does not check a merge. The others can still make their commits, since git does not run `pre-commit` for them as it does for `git commit`; pushing them is caught. |
| A remote-tracking ref pointed at local work, as in `git update-ref refs/remotes/origin/x side` or `git fetch . side:refs/remotes/origin/x`, followed by `git merge origin/x` | Allowed where `cw gate install` has run. `pre-merge-commit` lets through a merge of commits a remote-tracking ref contains, and nothing checks how the ref got there: rewriting one, which the plugin hook does not see, makes local work look published ([Git hooks](#git-hooks)). |
| `gh pr create --fill`, `make commit`, `./release.sh`, `python3 -c 'import subprocess; subprocess.run(["git", "commit", …])'` | Allowed. The hook does not parse a program's own arguments or what a script runs, so git run from inside them is not seen. Caught when `cw gate install` has run: git runs its hooks whichever program starts it. |
| Staged bytes that differ from the verified working tree: `git add`, then restore the verified bytes on disk, then `git commit -m x` | Allowed once the task's verify passed on the working tree. The gate digests the working tree, not the index, so a commit without `-a` can hold staged bytes that were never verified. Only the later push asks, because the working tree then differs from the commit. |
| Git run outside the agent's shell tool (a terminal, an editor), under Codex before you trust its hooks (again after an update that changes `hooks/codex-hooks.json`), or commits made through a GitHub MCP tool or the GitHub API | Not checked: the hook reads only shell commands, Codex runs no plugin hook you have not trusted, and an API call is not a git command. `cw publish` applies the gate itself wherever it runs. Git run from a terminal or an editor, and under Codex before you trust its hooks, is caught when `cw gate install` has run, within the limits of the last row; an API call still is not. |
| A commit from another clone of the repository, or from a worktree the record does not govern | Allowed. A record governs its own worktree root and, when it was started in the main checkout, that checkout's linked worktrees; a record started in a linked worktree governs only that worktree ([Which records gate](#which-records-gate)). |
| An older `cw` on `PATH` that cannot read the record | Allowed. Its hook skips a record it cannot read, such as one with `amendments`, `packs` or `test_rules`, or, under a `cw` older than v3.0.0-rc.2, any record of version 4 or later ([Which source counts](#which-source-counts)). The current hook asks about a record from a newer `cw` instead ([Records that cannot be read](#records-that-cannot-be-read)). |
| A task directory in which no revision decodes, such as `echo '{' > <task>/record-000001.json` for a task with one revision | Allowed. With no revision to read, the hook cannot tell which repository the task belongs to, so it ignores the directory. A damaged later revision asks instead ([Records that cannot be read](#records-that-cannot-be-read)). |
| `chmod 000 <task>`, or `chmod 000` on every revision file of a task (its only one, for a new task) | Allowed. A task directory the hook cannot list, or whose revision files it cannot open, has no revision to read and is ignored, like one in which no revision decodes. |
| A newest revision that decodes but names another `baseline.root`, has `plan.kind` `small` or carries `abandonment`, such as one copied in from another task | Allowed. The hook selects a task by its newest decodable revision and loads the whole chain only for a task that this revision says governs, so the revision hides an open record even though its chain would not load. |
| Codex running `hooks/hooks.json` (a plugin installed before `hooks/codex-hooks.json`, or a Codex that ignores the manifest's `hooks` entry) and sending PreToolUse input without `turn_id` or without `model` | Allowed: the asks before `cw task abandon`, before an `--owner-approval`, for an unrecorded substantial change, for a push with uncommitted changes and after a directory the hook cannot resolve. That file passes no `--host`, so `cw` tells Codex apart by those two members, and without both it answers as for Claude Code, whose ask Codex does not honour. The recorded Codex inputs carry both. The denies still deny. Commit and push are caught when `cw gate install` has run ([Codex](#codex)). |
| A record that cannot be read, under Codex: `echo '{' > <task>/record-000002.json` | Allowed. Codex cannot ask, and the hook lets the command go ahead there rather than block the user on a file it cannot judge ([Codex](#codex)). |
| A rewritten record, such as revision 1 with `plan.kind` set to `small` | Allowed. The agent's user can write the store, and the revision hash chain is plain SHA-256 with no key, so a rewritten chain loads as valid. |
| `cw task abandon --task <task> --reason TEXT` run where the hook cannot see it: from a script, `make` or a plan check; through a name that no word of the command spells, such as `X=c; ${X}w task abandon …`; or through a renamed copy of `cw`, such as `cw-g5 task abandon …` | Allowed. An abandoned task never gates. The hook asks before every `cw task abandon` whose words it can see ([Abandoning a task](#abandoning-a-task)), and under Codex it denies one. |
| A task nobody touched for 14 days | Allowed, with no message: the task stops gating 14 days after its latest revision ([Which records gate](#which-records-gate)). |
| A `go` rebuilt from modified `cmd/go` source, or a GOROOT the user can write with an edited standard library or tools | `cw task run` records a pass. A check named `go` runs only a build of `cmd/go`, so a script named `go` first on `PATH` is refused with failure `untrusted_toolchain` ([which go a check runs](operations.md#which-go-a-check-runs)), but a build of modified source still passes. A check that runs another program, such as `make`, runs whatever `PATH` names. That is a limit of the design. |
| A C compiler chosen by `CC`, `go env -w CC` or a `gcc` first on `PATH`, which `go test` runs for `-race` and cgo builds | `cw task run` records a pass, and the Go rule credits it, although the compiler can make the test binary report a pass without running a test: `cw` does not verify the C compiler. A known gap, open until a later fix. |
| A plan check that runs `make publish`, `python3 -c …`, `go run …` or `./release.sh` | Allowed by `cw task start`, and `cw task run` runs it with the user's rights, so it can commit and push; the hook never sees it. `cw task start` refuses shells, `env`, `sudo`, network and deployment tools, git other than inspection commands and `cw publish` by name, which guards against accidents, not against a program that does the same. Its commits and pushes are caught when `cw gate install` has run: git runs its hooks under the check too, while the task is still unverified. |
| Self-declared agent IDs | One agent can record itself as implementer, test engineer and reviewer under different `agent_id` strings. Independence compares those strings with each other and with the plan's `coordinator_id`; nothing authenticates them. |
| The off switches and failures: `enforce_verify=false`, `cw` not on `PATH`, an internal error or timeout in the hook | Allowed. The deny text names the `enforce_verify` switch, and the agent can run the `claude plugin configure` command itself: the hook does not inspect it, and the option applies once Claude Code restarts. See [Fail-open rules](#fail-open-rules) and [Disabling the gate](#disabling-the-gate). |
| Where `cw gate install` wrote git's hooks, git run outside the agent's shell tool with `--no-verify` or `CW_GATE=off`, a merge the agent runs that way itself (`git merge --no-verify`, `CW_GATE=off git merge`), after `core.hooksPath` changed, or with no `cw` on git's `PATH` (a GUI client's `PATH` may be shorter) | Allowed, with no message. Git skips its hooks, or the installed hook exits 0 when it finds no `cw` ([Installing the git hooks](#installing-the-git-hooks)). |

## Disabling the gate

Set the plugin option `enforce_verify` to `false` in the plugin's configuration,
which exports `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` to the hook, and
`small_task_prompt` to `false` to stop the ask for unrecorded changes. Removing
`cw` from `PATH` also disables the gate, because the command checks for `cw`
first and exits 0 when it is absent; the session-start line then says so.
Codex has no plugin configuration: start Codex with the variable instead, as
in `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false codex` ([Codex](#codex)).
`cw hook git` reads the same variables from git's environment, and also
`CW_GATE=off` ([Git hooks](#git-hooks)); `cw gate uninstall` removes the git
hooks that `cw gate install` wrote.

## Codex

Codex runs the hooks file that `.codex-plugin/plugin.json` names in `hooks`,
`hooks/codex-hooks.json`, in place of `hooks/hooks.json`. Each of its commands
passes `--host codex`, and a given `--host` (`codex` or `claude-code`) decides
the host. Without one, as `hooks/hooks.json` runs `cw`, `cw` tells Codex apart
as it did before `--host`: by `turn_id` and `model` in the PreToolUse input,
and at session start, whose input has no `turn_id`, by `PLUGIN_ROOT`, which
Codex sets and Claude Code does not. So a Codex that still runs
`hooks/hooks.json`, from a plugin installed before the Codex file or because
it ignores the manifest's `hooks` entry, keeps the Codex decisions below.
What follows was recorded with codex-cli 0.167.2 on 2026-11-12 and
0.168.0 on 2026-11-13, `codex exec` and a wrapper in place of `cw` that saved
each hook's arguments, input and environment
([Recorded host runs](#recorded-host-runs));
[`codex-pre-tool-use.json`](../internal/cli/testdata/codex-pre-tool-use.json)
and [`codex-session-start.json`](../internal/cli/testdata/codex-session-start.json)
are recorded inputs, with paths replaced by placeholders.

- **Trust.** Codex runs a plugin hook only after you trust it: before that,
  no hook ran, and `codex exec` did not say so.
  `--dangerously-bypass-hook-trust` runs untrusted hooks for one invocation.
  Per the Codex hooks documentation, you trust hooks with `/hooks` in the CLI
  or **Review hooks** in the desktop app, and trust is recorded against the
  hook's hash, so review again when an update changes `hooks/codex-hooks.json`.
  The update that added that file changed every Codex hook: trust them again
  after it, or Codex runs none of them and only `cw publish` and git's hooks
  ([Installing the git hooks](#installing-the-git-hooks)) apply the gate. No
  feature flag is needed: `codex features list` shows `hooks` stable and on,
  and `plugin_hooks` removed.
- **Hooks file.** In the recorded runs Codex ran only the handlers of
  `hooks/codex-hooks.json`: every call carried `--host codex`, and none came
  from `hooks/hooks.json`. Claude Code does not read that manifest.
- **Input.** The PreToolUse input names the shell tool `Bash` and holds `cwd`
  and `tool_input.command`, as Claude Code's does, plus `turn_id` and `model`,
  which Codex documents as its own extensions. Claude Code's PreToolUse input
  has neither; without `--host`, `cw` takes the two together as the sign of
  Codex, and one of them alone is not enough.
- **Matcher.** Codex matches `Bash` but ignores the `if` condition, so the
  Codex file has two PreToolUse handlers without one, each run for every shell
  command, at the same time. `cw hook pre-publish --host codex` exits at once,
  without output, for a command with no `git commit`, `git push`,
  `cw task abandon` or `--owner-approval`; `cw hook task-usage --host codex`
  samples the rollout in `transcript_path` before a `cw task` command and
  otherwise exits at once ([Usage sampling](#usage-sampling)).
- **An older `cw`.** A `cw` from before `--host` rejects the flag with exit 2
  before it reads its input. Each command of the Codex file then runs the
  same hook again without the flag, on the same input, so the gate stays on:
  that `cw` tells Codex apart by the input's `turn_id` and `model`, and at
  session start by `PLUGIN_ROOT`, as a current `cw` does without `--host`.
  Any other exit status runs nothing more.
- **Decisions.** A deny blocks the command, and the agent sees
  `Command blocked by PreToolUse hook:` and the reason. No output lets the
  command run. An ask is not supported: Codex ran the command without asking
  and told the agent nothing.
- **Environment.** Codex sets `PLUGIN_ROOT` and `PLUGIN_DATA` (also as
  `CLAUDE_PLUGIN_ROOT` and `CLAUDE_PLUGIN_DATA`) to the plugin's copy under
  `$CODEX_HOME/plugins/cache` and its data directory under
  `$CODEX_HOME/plugins/data`; the Codex file reads the first two. It sets no
  `CLAUDE_PLUGIN_OPTION_*` variable, and the hook inherits the environment
  Codex was started with.
- **Session start.** On `startup` the session-start hook adds
  `global/AGENTS.md` to the session as developer context, so the trusted
  plugin also loads the working conventions. Codex runs it again on `resume`,
  where `cw hook session-start --host codex` adds nothing
  ([Session start](#session-start)): after a `codex exec resume` the thread
  held one copy. Compaction drops it from the thread's history, and the
  `compact` run adds it back before the next request. A thread from
  `codex exec fork` carries its parent's history, conventions included; Codex
  names that source `fork`, which the matcher does not list, and no
  session-start hook ran. Under Codex, `cw` compares itself with
  `.codex-plugin/plugin.json` and skips the Claude Code version. That version
  line is not recorded under Codex: the runs used a `cw` built between tags,
  which is not compared.

So under Codex the pre-publish hook turns four asks into denies: a
`cw task abandon`, an unrecorded substantial change, a push with uncommitted
changes over a verified record, and a commit or push after a directory the
hook cannot resolve ([Asking instead of denying](#asking-instead-of-denying)). The
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

## Recorded host runs

On 2026-11-12 the plugin's hooks ran live under both hosts, each in a
throwaway Git repository with an open substantial task, the plugin loaded from
a checkout, and a wrapper in place of `cw` that logged each hook call before
running the real `cw`.

**Claude Code 2.1.311**, headless: `claude -p --plugin-dir <checkout> --model
haiku --allowedTools Bash --permission-prompts none --include-hook-events`.
Nobody can answer a prompt there, so an ask became a denial, and the agent
was told the hook's reason.

| Bash command | Handlers that ran | Result |
| --- | --- | --- |
| `cw task abandon --task <task> --reason probe` | the abandon handler | Ask, denied; the command did not run. |
| `echo f3-probe` | none, and no hook event | The command ran. |
| `cw task aban''don --task <task> --reason probe` | the abandon handler | Ask, denied, as for the plain command. |
| `git status --short && cw task abandon --task <task> --reason probe` | both | Two asks, one denial. |
| `echo "$(printf f3-subst)"` | both | No output from either; the command ran. |

**Codex 0.167.2**: `codex exec --json --dangerously-bypass-hook-trust` with a
temporary `CODEX_HOME` and the plugin added with `codex plugin marketplace add`
and `codex plugin add`, as in [Codex](#codex).

| Run | Hook calls | Result |
| --- | --- | --- |
| New session: `git status --short`, then `cw task abandon --task <task> --reason probe` | SessionStart `startup` once; `cw hook pre-publish` twice for each command, at the same time | The conventions were added once. The status ran; the abandon was blocked with `Command blocked by PreToolUse hook:` and the reason. |
| `codex exec resume` of that session | SessionStart `resume` once | The conventions were added again: the thread held two identical copies. |
| `codex exec resume` with `-c model_auto_compact_token_limit=30000` | SessionStart `resume` and `compact`, both after the compaction and before the next request | The compacted history held no copy; each hook added one. |
| `codex exec fork` of another session | no SessionStart call; `cw hook pre-publish` twice | The fork's first request carried its parent's history. |
| A copy of the plugin whose `.codex-plugin/plugin.json` names `./hooks/codex-hooks.json` | that file's SessionStart handler and its one PreToolUse handler, once each; none from `hooks/hooks.json` | The manifest's file replaces the default. |

The Codex runs used a `cw` from before F19: since then a Codex `resume` adds nothing, so the conventions are not
added again on resume, and after a compaction only the `compact` run adds them ([Session start](#session-start)).

**Codex 0.168.0**, 2026-11-13, with the plugin's own `hooks/codex-hooks.json`,
otherwise set up as above. The task was open and unverified; the prompt ran
`echo`, then `cw task abandon --task <task> --reason probe`, then
`git commit --allow-empty -m probe`.

| Run | Hook calls | Result |
| --- | --- | --- |
| New session, the `cw` with `--host` | SessionStart `--host codex` once; for each command `pre-publish --host codex` and `task-usage --host codex` once each | The echo ran; the abandon and the commit were blocked, the abandon with the reason that starts `agent-workflows: Codex cannot ask for confirmation`. |
| The same, with a `cw` from before `--host` | each hook once with `--host codex`, exit 2, then once without it, exit 0 | The same three results: the older `cw` read the input on the second call and denied both. |
| `codex exec resume` of the first session | SessionStart `--host codex` once, with no context | The thread held one copy of the conventions. |
| A new session without `--dangerously-bypass-hook-trust` | none | `pwd` ran; Codex did not say it skipped the untrusted hooks. |

These runs did not cover SessionStart on `clear` under either host (neither
headless mode can clear), on `resume` and `compact` under Claude Code, or in
agents Codex spawns; hooks trusted through `/hooks` rather than
`--dangerously-bypass-hook-trust`; the version line under Codex; or an ask
answered in an interactive Claude Code session.
