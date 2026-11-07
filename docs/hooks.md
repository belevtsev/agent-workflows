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
appends the one-line `cw doctor --summary` for the session's directory. When
`cw` is missing or prints nothing, the hook command prints the file itself. That
summary is produced with a 5 second deadline and cached for one hour under
`${CLAUDE_PLUGIN_DATA}/doctor/<sha256 of the directory>.json`; when it cannot be
produced in time, the conventions are returned alone.

## Publication gate

Before the Bash tool runs a command that Claude Code's `Bash(git commit *)` or
`Bash(git push *)` rule matches, `cw hook pre-publish` finds the directory each
`git commit` or `git push` in the command runs in (the session's working
directory, moved by a preceding `cd DIR` or `pushd DIR` and by git's `-C DIR`;
a `cd` inside parentheses or backticks ends with them, as in a subshell;
redirections on a `cd` are ignored, and `cd "$(git rev-parse --show-toplevel)"`
stays in the same repository) and looks for
the newest substantial task record bound to that directory's repository. The
record gates the command while it has no passing `cw task verify`, or while
its passing verification was taken on different source bytes than the working
tree holds now. The hook then denies the command and names the record and the
exact `cw task verify --task <dir> --summary` to run; after a passing
verification on the current source the same command goes ahead.

Only the content of the eligible source files counts: `cw task verify` records
a digest of each file's path, mode and bytes (`verification.source_sha256`),
so staging or committing the verified bytes does not reopen the record, and a
push after the commit goes ahead. Editing any eligible file does reopen it. A
record verified by a `cw` older than this digest stays open until it is
verified again. In the other direction, a `cw` release older than this digest
rejects the unknown `source_sha256` member, so it cannot read a record this
release has verified; keep every host that shares a task store on the same
`cw`.

The record consulted is the newest one for which all of the following hold:

- its `plan.kind` is `substantial`;
- its `baseline.root` is the repository's worktree root, or the main worktree's
  root when the command runs in a linked worktree (a record started in the
  main checkout therefore gates every linked worktree, judged on the main
  checkout's source; a record started in a linked worktree gates that worktree
  only);
- it was started within the last 14 days.

Older records are superseded by the newest one: when the newest record is
closed, an older abandoned record does not deny. A record that cannot be read
is skipped and the next newest decides.

The hook reads records from `$CW_TASK_STORE` when it is set, else from the
state directory `cw task start` writes to (`$XDG_STATE_HOME/agent-workflows/workflow-tasks`,
else `~/.local/state/agent-workflows/workflow-tasks`). `cw task start` does not
read `CW_TASK_STORE`; give it the same directory with `--store` when you use
the variable.

## Fail-open rules

The gate only ever blocks on a positive decision. `cw hook pre-publish` exits 0
without output, and the command proceeds, when:

- the plugin option `enforce_verify` is `false`
  (`CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false`);
- the command is not a `git commit` or `git push` (the hook recognises git
  after `cd`, `&&`, `;`, pipes, shell keywords and prefixes such as `env` or
  `command`, and git's global options; forms it cannot follow are allowed,
  including any git command after a directory change it cannot resolve, such
  as `cd` with options, `cd -`, `~`, `$VAR`, a quoted path with spaces or
  `popd`, until an absolute `cd` or `-C`);
- the directory the command runs in is not inside a Git repository;
- the task store is missing, empty, or holds no matching record;
- anything fails internally: unreadable input, an unreadable or damaged record,
  a failed source capture, a missing `git`. Damaged records are skipped rather
  than reported.

The hook never exits 2, and the command strings in `hooks/hooks.json` end with
`exit 0` rather than `exec`-ing `cw`, so even a `cw` too old to know the hook
subcommand cannot block. A hook timeout (30 seconds) is treated by Claude Code
as a non-blocking error.

## Disabling the gate

Set the plugin option `enforce_verify` to `false` in the plugin's configuration,
which exports `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` to the hook. Removing
`cw` from `PATH` also disables both hooks, because each command checks for `cw`
first and exits 0 when it is absent.
