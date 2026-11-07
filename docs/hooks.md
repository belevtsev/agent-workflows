# Plugin hooks

`hooks/hooks.json` registers two Claude Code hooks for the agent-workflows
plugin. Both run `cw`; when `cw` is not on `PATH` they degrade to printing the
conventions file or doing nothing. No hook uses a script file: every command is
inline, which keeps `cwdev check`'s first-party automation policy satisfied.

## Session start

On `startup`, `resume`, `clear` and `compact`, `cw hook session-start` returns
`global/AGENTS.md` as the session's additional context. With the plugin option
`session_doctor` set to `true` (`CLAUDE_PLUGIN_OPTION_SESSION_DOCTOR=true`), it
appends the one-line `cw doctor --summary` for the session's directory. That
summary is produced with a 5 second deadline and cached for one hour under
`${CLAUDE_PLUGIN_DATA}/doctor/<sha256 of the directory>.json`; when it cannot be
produced in time, the conventions are returned alone.

When `cw` is not on `PATH`, the hook command prints the file followed by one
line, `agent-workflows: the commit and push verify gate is inactive because cw
was not found on PATH.`, so the session knows the gate below is off. When `cw`
is present but fails or prints nothing, the hook command prints the file alone.

## Publication gate

The `PreToolUse` handler runs for every Bash command whose text contains `git`
(the condition `Bash(*git*)`; Claude Code also runs it when it cannot tell which
commands a Bash input runs). `cw hook pre-publish` then finds each `git commit`
and `git push` in the command and the directory it runs in, and exits at once,
without output, when there is none.

### Recognised commands

The hook splits the command into simple commands at `;`, `&&`, `||`, `|`, `&`,
newlines, parentheses and any unpaired backtick, and finds git:

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
  substitution is checked too, from the directory in effect where it appears;
- inside a command string given to `sh`, `bash`, `zsh`, `dash` or `ksh` with
  `-c` (also `-lc` and similar), or to `eval`;
- written as a path (`/usr/bin/git`), quoted (`"git"`, `'commit'`) or with a
  backslash that bypasses an alias (`\git`);
- with git's global options, including `-c NAME=VALUE`, `--git-dir`,
  `--work-tree` and `-C DIR`.

The directory starts as the session's working directory and moves with a
preceding `cd DIR` or `pushd DIR` and with each `-C DIR`; a `cd` inside
parentheses or a substitution ends with them, as in a subshell; redirections on a
`cd` are ignored, and `"$(pwd)"` or `"$(git rev-parse --show-toplevel)"` after
`cd` or `-C` stays in the same repository. The split is deliberately simple and fails open: after a directory
change the hook cannot follow (`cd` with options, `cd -`, `~`, `$VAR`, a quoted
path with spaces, `popd`) later git commands are not checked until an absolute
`cd` or `-C`. A script or program that runs git itself (`./release.sh`,
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
abandoned; keep every host that shares a task store on the same `cw`.

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
the variable.

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
  out. Damaged records are skipped rather than reported.

The hook never exits 2, and the command strings in `hooks/hooks.json` end with
`exit 0` rather than `exec`-ing `cw`, so even a `cw` too old to know the hook
subcommand cannot block. A hook timeout (30 seconds) is treated by Claude Code
as a non-blocking error.

## Disabling the gate

Set the plugin option `enforce_verify` to `false` in the plugin's configuration,
which exports `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` to the hook, and
`small_task_prompt` to `false` to stop the ask for unrecorded changes. Removing
`cw` from `PATH` also disables the gate, because the command checks for `cw`
first and exits 0 when it is absent; the session-start line then says so.
