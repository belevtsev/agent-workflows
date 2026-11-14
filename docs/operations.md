# Operations

This is the reference of `cw`: every command, the file `cw.toml`, the exit
statuses, where `cw` keeps its records, and how to use it in continuous
integration. [The README](../README.md#install) says how to install it, and
[the quick start](quickstart.md) takes a repository to its first gated commit.

`cw` itself knows no programming language. A check is a program and its
arguments; `cw` starts it, waits for it, and records how it ended.

The examples on this page were produced on Linux with git 2.53. Temporary
paths are shortened to `<repository>`.

## Contents

- [Command line](#command-line)
- [cw init](#cw-init)
- [cw add](#cw-add)
- [cw run](#cw-run)
- [cw status](#cw-status)
- [cw stats](#cw-stats)
- [cw context](#cw-context)
- [cw hook](#cw-hook)
- [cw guard](#cw-guard)
- [cw uninstall](#cw-uninstall)
- [cw roles](#cw-roles)
- [cw change](#cw-change)
- [cw tokens](#cw-tokens)
- [cw version](#cw-version)
- [cw.toml](#cwtoml)
- [Exit statuses](#exit-statuses)
- [Where cw keeps its records](#where-cw-keeps-its-records)
- [Continuous integration](#continuous-integration)

## Command line

- `cw help` prints the list of commands and the exit statuses, and exits 0.
  `cw` with no command prints the same list on standard error and exits 2.
- `cw help <command>`, `cw <command> --help` and `cw <command> -h` print the
  usage line and the summary of one command.
- Options may stand before or after the other arguments: `cw run lint --force`
  and `cw run --force lint` mean the same. An option takes its value as the
  next word or after `=`: `--base main` or `--base=main`.
- Everything after `--` is passed on unread. Only `cw add` accepts `--`.
- An error is printed on standard error with the prefix `cw:`.

## cw init

Creates `cw.toml` when the root of the working tree has none, installs the
three git hooks, and sets the mode of the gate. An existing `cw.toml` is kept
byte for byte.

```text
cw init [--observe]
```

| Option | Meaning |
| --- | --- |
| `--observe` | The hooks record what they would decide and allow every commit and push. Without it, `cw init` sets the gate to enforce. |

`cw init` writes the hooks `pre-commit`, `pre-merge-commit` and `pre-push` into
the `hooks` directory of the common git directory. It does not change a hook
file that another tool wrote, and it writes no hook at all when
`core.hooksPath` is set. [Hooks](hooks.md) explains both cases. Running
`cw init` again leaves the same hooks in place and only changes the mode.

```text
$ cw init
created cw.toml with no checks
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: cw add <id> -- <program> [<argument>...], then cw run
```

When a hook is left for you to edit, the line to add is printed on standard
error:

```text
cw: these hooks were written by another tool, and cw did not change them.
cw: add the line shown to each hook. It must be the last command of that hook:
  <repository>/.git/hooks/pre-commit
      cw hook pre-commit "$@"
```

Exit statuses: 0 done; 1 not inside a git repository, or a file cannot be
written; 2 an argument other than `--observe`; 4 a hook of another tool does
not call `cw` yet. With 4, `cw.toml`, the mode and the free hooks are written.

## cw add

Appends one check to `cw.toml`. No earlier byte of the file changes.

```text
cw add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]
```

| Option | Meaning |
| --- | --- |
| `--paths <pattern>` | A path pattern the check depends on. Give it once for each pattern. Without it, the check depends on every path. |
| `--timeout <duration>` | The time the check may take, from `1s` to `6h`, such as `90s` or `5m`. Without it, the check gets the default of 10 minutes. |

The words after `--` become `run` exactly as the shell gave them to `cw`. `cw`
does not split or quote them again. The result must be a valid declaration:
when it is not, nothing is written and the reason is printed.

```text
$ cw add unit --paths 'src/**' --paths unit.sh -- sh unit.sh
added the check `unit` to cw.toml
$ cw add unit -- true
cw: the check was not added, because the file would not be a valid declaration:
cw.toml:15: the id `unit` is declared twice, at lines 5 and 15
```

The first command appends this block:

```toml
[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]
```

`cw add` does not check that the program exists. A program that cannot be
started makes the run end `failed`.

Exit statuses: 0 added; 1 no `cw.toml`, or the file would not be valid; 2 no
id, more than one id, or nothing after `--`.

## cw run

Executes each declared check that has no passing run on the content of the
working tree, one at a time in the order of `cw.toml`, and records each run.
[Evidence](evidence.md) explains what "the content" is and when a run counts.

```text
cw run [<id>...] [--force] [--base <rev>]
```

| Option | Meaning |
| --- | --- |
| `<id>...` | Consider only these checks. An id that `cw.toml` does not declare is a usage error. |
| `--force` | Execute the selected checks even when they have a passing run on this content. |
| `--base <rev>` | Count a check as passed when its checked content is the same as in the revision `<rev>`. For continuous integration; see [Continuous integration](#continuous-integration). |

For each check, `cw run` takes a snapshot of the working tree: the tree that
`git add -A` followed by `git commit` would record. The index and the files on
disk do not change. The program runs from the root of the working tree,
without a shell, with the environment of `cw run` and no standard input. Its
output goes to the terminal and to a log. After it exits, `cw` takes the
snapshot again. A run ends with one outcome:

| Outcome | When |
| --- | --- |
| `passed` | The program exited 0 and the snapshot after it equals the snapshot before it. |
| `failed` | The program exited with another status, could not be started, or was ended by something outside `cw`, such as a signal. The text of the run says which. |
| `timed_out` | The timeout passed first. `cw` stopped the program and the processes it started. |
| `source_changed` | The program exited 0, and the working tree changed during the run. Up to 20 changed paths are named. |
| `interrupted` | `cw` received SIGINT, SIGTERM or SIGHUP, or the `cw` process died. |

```text
$ cw run
== unit: sh unit.sh
unit ok
== unit: passed in 0s
== lint: true
== lint: passed in 0s
every declared check has a passing run on this content
$ cw run
== unit: satisfied, not run again: run 20261114T144211Z-f48b9af5 passed on this content
== lint: satisfied, not run again: run 20261114T144211Z-44b14087 passed on this content
every declared check has a passing run on this content
```

A failure names the log:

```text
== unit: sh unit.sh
app is not ok
== unit: failed with exit status 3
   log: <repository>/.git/cw/logs/20261114T144217Z-80b277b8.log
```

When a named subset passes and other checks still lack a run, the last line is
`not yet verified; still missing:` and their ids, and the exit status is 0.
When no check is declared, `cw run` prints
`no checks are declared, so there is nothing to run`.

`cw run` also removes old records: see
[Where cw keeps its records](#where-cw-keeps-its-records).

On Windows the code looks the program up as the system does, and refuses a
`.bat` or `.cmd` program whose arguments hold a character other than letters,
digits and `_ . / : = + , @ \ -`, because `cmd.exe` could read such an argument
as a second command. This code has not been run on Windows.

Exit statuses: 0 every check it executed passed, or nothing needed a run; 1 the
declaration is missing or invalid, the working tree cannot be recorded, the
records cannot be written, or `--base` names no tree, or on Windows a batch
file was refused; 2 an unknown id, an unknown option, or `--`; 5 a check it
executed did not end `passed`.

## cw status

Says whether a commit of the working tree would be allowed, lists each
declared check with its state, and compares the index with the working tree.
It executes no check.

```text
cw status [--json]
```

| Option | Meaning |
| --- | --- |
| `--json` | Print one JSON object instead of text. |

The state of a check is `satisfied`, `not run`, `failed` or `timed out`.
[Evidence](evidence.md#what-cw-status-says) lists what each state means and the
text that goes with it.

```text
$ cw status
not verified: a commit of this content would be denied
  unit  failed with exit status 3; log: <repository>/.git/cw/logs/20261114T144217Z-80b277b8.log
  lint  satisfied
index: differs from the working tree; not staged:
  README.md
  src/app.txt
a commit of the index alone would be allowed
run `cw run` to produce the missing evidence
```

The lines that start with `index:` compare the tree of the index, which is what
`git commit` records, with the snapshot of the working tree. When they differ,
up to 20 paths that are not staged are listed, and `cw status` says whether a
commit of the index alone would be allowed. In observe mode a line
`mode: observe` is added. When the hooks of `cw` are not installed in this
clone, a warning that names `cw init` is printed on standard error.

`--json` prints this object. The fields stay while the declaration `version` is
1.

```json
{
  "tree": "92c70cfe788ecf26569e0146068a231b83c1ce1b",
  "verified": false,
  "index_tree": "33186c39d26954bf0add57cdb42d94b09682ec7b",
  "index_verified": true,
  "mode": "enforce",
  "hooks": "installed",
  "checks": [
    {
      "id": "unit",
      "state": "failed",
      "run_id": "20261114T144217Z-80b277b8",
      "tree": "92c70cfe788ecf26569e0146068a231b83c1ce1b",
      "outcome": "failed",
      "exit_status": 3,
      "ended_at": "2026-11-14T14:42:17Z",
      "log": "<repository>/.git/cw/logs/20261114T144217Z-80b277b8.log"
    },
    {
      "id": "lint",
      "state": "satisfied",
      "run_id": "20261114T144217Z-84d464ba",
      "tree": "92c70cfe788ecf26569e0146068a231b83c1ce1b",
      "outcome": "passed",
      "exit_status": 0,
      "ended_at": "2026-11-14T14:42:17Z",
      "log": "<repository>/.git/cw/logs/20261114T144217Z-84d464ba.log"
    }
  ],
  "not_staged": [
    "README.md",
    "src/app.txt"
  ]
}
```

| Field | Meaning |
| --- | --- |
| `tree` | The snapshot of the working tree. |
| `verified` | Every declared check is satisfied for `tree`. |
| `index_tree`, `index_verified` | The tree of the index, and whether a commit of it would be allowed. |
| `mode` | `enforce` or `observe`. |
| `hooks` | `installed` when each of the three hook files git would run exists, can be executed and calls `cw hook`; otherwise `missing`. |
| `checks` | One entry per declared check: `id`, `state` (`satisfied`, `not_run`, `failed`, `timed_out`), `running` when a run executes now, and the fields of the run that decides the state, when one does. |
| `not_staged`, `not_staged_more` | Up to 20 paths in which the index differs from the working tree, and the count of the rest. |
| `warnings` | Warnings, such as a record that cannot be read. |

Exit statuses: 0 the working tree is verified, or no check is declared; 1 the
declaration is missing or invalid, the working tree cannot be recorded, or the
records cannot be read; 2 an argument; 5 not verified. Whether the hooks are
installed does not change the exit status.

## cw stats

Counts what the records of this repository hold: runs of each check by
outcome, passes after a failure on the same checked content, and hook
decisions.

```text
cw stats [--since <period>] [--json]
```

| Option | Meaning |
| --- | --- |
| `--since <period>` | Count the records of this period: a number and a unit, `m`, `h`, `d` or `w`, such as `90m`, `36h`, `7d` or `2w`. The default is 30 days. |
| `--json` | Print one JSON object instead of text. |

```text
$ cw stats
from 15 Oct 2026 14:42 to 14 Nov 2026 14:42 UTC, from the records of this repository

runs of each check, by outcome
  lint: passed 4
  unit: passed 1, failed 1

passes after a failure on the same checked content
  none

hook decisions
  pre-commit, enforce mode: 1 allowed
  pre-commit, enforce mode: 2 denied
      failed: 1 (unit 1)
      not_run: 1 (lint 1)
```

- A run counts in the period when it ended in it, or, when it recorded no end,
  when it started in it.
- "Passes after a failure" counts passing runs whose previous decisive run of
  the same command, on the same checked content, did not pass. Nothing the
  check reads had changed, so each one is a result that did not repeat.
- A denial counts once under each reason it had: the state of a check that was
  not satisfied (`failed`, `not_run`, `timed_out`) or the cause that prevented
  a decision (`no_declaration`, `invalid_declaration`, `tree`, `store`,
  `internal`). In observe mode a denial is printed as `would have been denied`.
- Records that cannot be read are counted and named in the last line.

`--json` prints the same as one object with the fields `since`, `until`,
`runs`, `pass_after_failure`, `decisions` and `unreadable`.

`cw stats` reads no network and sends nothing anywhere: the counts stay in the
repository.

Exit statuses: 0 printed; 1 not inside a git repository, or the records cannot
be read; 2 an unknown option, a period that is not valid, or an argument.

## cw context

Prints what a coding agent should know at the start of a session in a gated
repository: that commits and pushes are gated, that `cw run` produces the
evidence, that `cw status` shows what is missing, and whether the hooks are
installed. The agent plugin runs it; see [Hooks](hooks.md#hooks-of-the-agent-plugin).

```text
cw context
```

```text
$ cw context
This repository is gated by cw. git allows a commit or a push only when every check declared in cw.toml has a recorded passing run on exactly the content that is committed or pushed.
- `cw run` executes the declared checks that have no passing run on the current content and records the results. Run it before you commit.
- `cw status` says whether a commit would be allowed and which checks are missing.
- A denied commit or push names what is missing. Produce the evidence with `cw run`; do not go around the hook.
The hooks are installed.
```

The last line says instead that the hooks are not installed in this clone, or
that the gate only observes. Outside a git repository, and where the root of
the working tree holds no `cw.toml`, it prints nothing.

Exit statuses: 0 always, also outside a repository; 2 an argument.

## cw hook

Decides for git whether a commit, a merge commit or a push goes through. The
hook files that `cw init` writes call it; you do not run it yourself. It
decides from the records alone: it executes no check and waits for none.
[Hooks](hooks.md) describes each decision.

```text
cw hook <name> [<argument>...]
```

`<name>` is `pre-commit`, `pre-merge-commit` or `pre-push`. The arguments and
the standard input are the ones git gives the hook. For `pre-push`, the first
argument is the name of the remote, and the standard input lists the refs that
the push updates.

A denial is printed on standard error:

```text
cw: this commit is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
cw: to produce the missing evidence, run:
  cw run
```

Exit statuses: 0 allowed, or observe mode; 2 no name or an unknown name; 3
denied. Outside a git repository the hook denies with 3.

## cw guard

Reads the notice that Claude Code or Codex gives a hook before a tool call, and
answers in two cases only: a shell command with a git call that skips git's
hooks, and an edit of `cw.toml`, of a git hook or of a record of `cw`. It never
judges a commit or a push. The agent plugin runs it; see
[Hooks](hooks.md#hooks-of-the-agent-plugin).

```text
cw guard [--host claude|codex]
```

| Option | Meaning |
| --- | --- |
| `--host claude` | The default. The answer asks the user. |
| `--host codex` | The answer denies the call with the reason, because Codex does not ask from a hook. |

The notice is a JSON object on standard input with `tool_name`, `cwd` and
`tool_input`. The guard reads `command` of the tool `Bash`, `file_path` of the
tools `Edit`, `Write` and `MultiEdit`, and the file names in the patch of
`apply_patch`. Where the working directory is not in a git repository whose
root holds `cw.toml`, it prints nothing.

The guard splits the command into simple commands and words, as a shell does:
quotes, backslashes, `;`, `&&`, `||`, `|`, `&`, newlines, `(`, `)`, `$(` and
backticks. It steps over `NAME=value` words and the wrappers `env`, `command`,
`sudo`, `time`, `nice` and `xargs`, and reads the text after `-c` of `sh`,
`bash`, `zsh` and `dash` as a command, to a depth of three. A call of `git`,
`/path/git` or `git.exe` skips the hooks when:

- a word after the subcommand is `--no-verify`, or a prefix of it at least as
  long as `--no-veri`, which git takes for the whole option;
- the subcommand is `commit` and a cluster of short options holds `n`, as in
  `-n`, `-an` or `-nm`; the cluster ends at `m`, `F`, `C`, `c` or `t`, which
  take a value, and at `S` or `u`, which take one only when it is attached;
- an option before the subcommand, `-c` or `--config-env`, sets
  `core.hooksPath`, in any case;
- the subcommand is `config`, a word names `core.hooksPath`, in any case, and a
  value follows the name or the call unsets it;
- a `NAME=value` word before the program holds `core.hooksPath`.

Words after `--` are not options, and the value of `-m`, `-F`, `--message`,
`--file`, `--author` and other options of `commit` that take one is not an
option. A command with an unclosed quote is judged by its text: it skips when
it holds `--no-verify`. An edit is of a git hook or a record of `cw` when its
path is under `hooks/` or `cw/` of the common git directory.

```text
$ echo '{"tool_name":"Bash","cwd":"<repository>","tool_input":{"command":"git commit -n -m x"}}' | cw guard
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"This command gives git commit the option -n, the short form of --no-verify, which skips the commit gate: the git hooks of cw would not judge the commit or the push."}}
```

The guard does not expand variables, aliases or functions, and does not run
the command. `git commit $FLAGS` or a git alias that adds `--no-verify` is not
recognised; the push rule of the hooks still judges the commit.

Exit statuses: 0 in every case it reads, also when the notice is not JSON; 2 an
argument or a host other than `claude` and `codex`.

## cw uninstall

Removes the hook files that `cw` wrote and the whole `cw/` directory of
records. `cw.toml` and every hook file of another tool stay as they are.

```text
cw uninstall
```

```text
$ cw uninstall
removed the hooks cw wrote and its records in <repository>/.git/cw
cw.toml is left as it is
```

When a hook file of another tool still calls `cw hook`, `cw uninstall` names it
and prints the line to remove:

```text
these hooks were not written by cw and still call it; remove the line from each:
  <repository>/.git/hooks/pre-commit
      cw hook pre-commit "$@"
```

Exit statuses: 0 done; 1 not inside a git repository, or a file cannot be
removed; 2 an argument; 4 a hook of another tool still calls `cw`.

## cw roles

Prints the seven roles of the task workflow, checks which paths a role changed
outside its limit, or prints the text of one role. [Roles](roles.md) describes
them.

```text
cw roles [--json]
cw roles check <role> --from <tree> [--task <task>]
cw roles text <role>
```

Exit statuses: 0 done, or the role stayed within its limit; 1 the role
configuration cannot be read, `--from` names no tree, or the working tree
cannot be recorded; 2 a wrong command line or an unknown role; 5 the role
changed a path outside its limit.

## cw change

Prints the stage of a task's change folder under `openspec/changes/`, or checks
the rules its files must obey. [The task workflow](workflow.md) describes the
folder and the rules.

```text
cw change status [<task>] [--json]
cw change check [<task>]
```

Exit statuses: 0 done, or no rule is broken; 1 the folder does not exist or
cannot be read; 2 a wrong command line or a task name that is not lower-case
words joined by hyphens; 5 `cw change check` found a broken rule.

## cw tokens

Reports the tokens a task used and the share the workflow itself took, or
records them from the notice of a host. [Tokens](tokens.md) says what is
counted.

```text
cw tokens [--task <task>] [--json]
cw tokens record [--host claude|codex]
```

`cw tokens record` reads the notice of the host on standard input. It records
nothing outside a git repository whose root holds `cw.toml`.

Exit statuses: 0 done; 1 no start is recorded for the task, the notice is not a
JSON object, or the records cannot be read or written; 2 a wrong command line.

## cw version

Prints the version, the revision, the build time, and the system and the
architecture it was built for. `cw --version` does the same.

```text
cw version
```

```text
$ cw version
cw v3.0.0-20261114142910-90aee1311da8+dirty
revision 90aee1311da859414c55b106f7a5655475bc2bc9
built 2026-11-14T14:29:10Z
linux/amd64
```

Exit statuses: 0 printed; 2 an argument.

## cw.toml

`cw.toml` at the root of the working tree declares the checks of the
repository. It is committed like any other file. `cw` reads it from the tree it
judges and from no other place: no file in the home directory, no environment
variable and no option adds, removes or changes a check. `cw run` and
`cw status` read it from the snapshot of the working tree, so a `cw.toml` that
git ignores counts as no declaration. A hook reads it from the tree of the
commit or of the pushed commit.

```toml
# The checks this repository requires before a commit or a push.
version = 1

[[check]]
id = "test"
run = ["go", "test", "./..."]

[[check]]
id = "lint"
run = ["npx", "eslint", "src"]
paths = ["src/**", "package.json", ".eslintrc.json"]
timeout = "5m"

[[check]]
id = "docs"
run = ["python3", "-m", "mkdocs", "build", "--strict"]
paths = ["docs/**", "mkdocs.yml"]
timeout = "90s"
```

The programs above are examples from three ecosystems. `cw` treats each of
them the same way: as a program and its arguments.

| Key | Required | Meaning |
| --- | --- | --- |
| `version` | yes | The integer `1`. |
| `[[check]]` | no | One table per check. A file with no check is valid, and then every commit is allowed. |
| `id` | yes | The name of the check: a lower-case letter or a digit, then up to 39 lower-case letters, digits, `_` or `-`. Unique in the file. |
| `run` | yes | An array of strings: the program, then its arguments. No shell reads it, so `$HOME`, `&&` or `*` reach the program as they are written. A check that needs a shell names one, such as `["sh", "-c", "..."]`. |
| `paths` | no | An array of one or more path patterns. The check depends only on the paths that match. Without it, the check depends on every path of the tree. |
| `timeout` | no | A string: a duration from `1s` to `6h` in the form `90s`, `5m` or `1h30m`. The default is `10m`. |

No other key is accepted, at the top or in a check.

A path pattern is matched against a path relative to the root of the
repository, with `/` as the separator and with case respected:

- `*` matches any run of characters inside one segment, and `?` one character.
- `**` as a whole segment matches any number of segments, also none, so
  `docs/**` matches `docs/a.md`, `docs/x/y.md` and an entry named `docs`.
- A plain name matches at the root only: `go.mod` does not match
  `tools/go.mod`; `**/go.mod` matches both.
- Nothing else is special. There is no `!`, no `[...]` and no `{a,b}`.
- A symbolic link and a submodule are each one entry, matched by their own
  path.

[Evidence](evidence.md#what-paths-changes) says what `paths` changes and what
it promises.

`cw` refuses a declaration that breaks a rule. Every broken rule is printed
with the file name and the line, and no check runs. A hook denies. The messages
are these:

| Problem | Message |
| --- | --- |
| Not valid TOML | `cw.toml:4: not valid TOML: toml: array is incomplete`, with the reason of the TOML reader |
| `version` missing, not an integer, or not 1 | `` cw.toml:1: `version` must be 1 `` |
| A key `cw` does not know | ``cw.toml:5: unknown key `tiemout` `` |
| No `id` | ``cw.toml:2: a check needs an `id` `` |
| An `id` of the wrong form | ``cw.toml:16: `id` must match [a-z0-9][a-z0-9_-]{0,39} `` |
| The same `id` twice | ``cw.toml:15: the id `unit` is declared twice, at lines 5 and 15`` |
| No `run` | ``cw.toml:2: a check needs `run`, an array of the program and its arguments`` |
| `run` as one string, empty, or with an empty program | ``cw.toml:4: `run` must be an array of strings that begins with a program, such as ["go", "test", "./..."]`` |
| `paths` empty or not an array of strings | ``cw.toml:5: `paths` must be an array of one or more patterns`` |
| A pattern that is empty, starts or ends with `/`, or holds `//` | ``cw.toml:5: the path pattern `/src/**` must be relative to the repository root, with no empty segment`` |
| `timeout` not a string, not a duration, or outside 1s to 6h | ``cw.toml:5: `timeout` must be a duration from 1s to 6h, such as "5m"`` |
| No `cw.toml` at the root of the tree | ``the tree has no declaration: there is no cw.toml at its root; `cw init` creates one`` |

A `cw.toml` that is not a regular file in the tree, such as a symbolic link, is
an invalid declaration.

## Exit statuses

Every command uses this one table.

| Status | Meaning |
| --- | --- |
| 0 | Done: `cw` did what was asked, and the answer is yes. |
| 1 | Could not: `cw` could not do what was asked, and says why. |
| 2 | Wrong command line: an unknown command, option or check id, or a missing argument. |
| 3 | A hook denies the commit, the merge or the push. |
| 4 | A hook of another tool is left for you to edit: `cw init` or `cw uninstall` printed the line to add or to remove. |
| 5 | Not verified: a check did not pass, the content has no passing run of every check, or a rule of a role or a change folder is broken. |

## Where cw keeps its records

`cw` keeps everything it records under `cw/` in the common git directory. In a
plain clone that is `.git/cw/`. All linked worktrees of a repository share the
common git directory, so they share the records: a run made in one worktree
counts in another for the same content.

```text
cw/mode                                      enforce or observe
cw/runs/<check id>/<content key>/<run id>.json   one record per run
cw/runs/<check id>/<content key>/<run id>.lock   held while the run executes
cw/runs/<check id>/<content key>/seq.lock        held while the end of a run is recorded
cw/logs/<run id>.log                         the output of one run
cw/decisions/<yyyy-mm>.jsonl                 one line per hook decision
cw/decisions/write.lock                      held while a decision is appended
cw/gc.lock                                   held while old records are removed
cw/tmp/                                      copies of the index, only while git works on them
cw/tokens/                                   token records; see tokens.md
```

- A run id is the start time in UTC and eight random hex digits, such as
  `20261114T144211Z-f48b9af5`.
- A log keeps at most the first 1 MiB and the last 9 MiB of the output, with a
  line between them that says how many bytes were dropped.
- A record that cannot be read is renamed to `<name>.damaged` by the next
  decisive run of its check on the same content.
- `cw run` removes logs older than 30 days, records of runs and files of
  decisions older than 365 days, and index copies older than one day. The age
  is the time the file was last written. When another `cw run` is already
  removing, this one skips it.

Outside the git directory, `cw` writes only `cw.toml`: `cw init` creates it, and
`cw add` writes the longer file to a temporary `.cw-toml-*` file beside it and
renames that over it. Nothing is written into the home directory. The hook
files are in the git directory. A check's program runs in the working tree
and can write there; `cw` records such a run as `source_changed`.

`cw uninstall` removes the hook files that hold the marker line of `cw`, in the
hooks directory git uses and in `hooks/` of the common git directory, and then
the whole `cw/` directory: runs, logs, decisions, the mode and the token
records. It leaves `cw.toml`, every hook file of another tool, and the agent
plugin.

## Continuous integration

The git hooks run only in a clone where `cw init` ran, and
`git commit --no-verify` skips them. A job in continuous integration is the
backstop: it runs the checks again on the content of the pull request. Records
are not shared between clones, so a fresh clone in CI starts with none.

```sh
git fetch origin main
cw run --base origin/main
```

`cw run` needs no `cw init` and no hooks. It exits 0 when every check it
executed passed and 5 when one did not, so the job fails on 5. The hook
warning of `cw status` does not appear, because `cw run` does not print it.

A job that only runs does not stop a merge. Make it a required status check
in the branch protection of the hosting service, so that a change whose job
failed, or never ran, cannot be merged. That setting is the only part of the
gate that a person or an agent with write access to a clone cannot skip:
some tools commit without the hooks, and a hook directory can be replaced.
[What other tools taught us](other-tools.md#what-speaks-against-parts-of-cw)
names the reports.

`--base <rev>` does this, according to the code:

1. It resolves `<rev>` to a tree. When `<rev>` names none, `cw run` exits 1 with
   ``the base revision `<rev>` names no tree in this repository``.
2. It reads `cw.toml` from that tree. When the base has no `cw.toml`, or an
   invalid one, the base counts nothing as passed, and every check runs.
3. For each check of the base it computes the content key on the base tree:
   for a check with `paths`, a digest of the entries that match; for a check
   without `paths`, the tree itself.
4. A check of the working tree counts as satisfied, and is not executed, when
   all of these hold:
   - the base declares a check with the same `id`, the same `run` and the same
     `paths`, in the same order;
   - the content key is the same in the base and in the working tree;
   - this clone holds no decisive run (`passed`, `failed` or `timed_out`) of
     that check on this content, of any age, and no record of it that cannot
     be read or is dated in the future.
5. Such a check is printed as
   `satisfied, not run: its checked content is the same as in the base revision`.
   Nothing is recorded for it, and the base counts for that one command only:
   `cw status` and the hooks never use it.

A check without `paths` depends on the whole tree, so the base counts it only
when the working tree is the base tree. In practice `--base` saves the runs of
checks with `paths` that a change does not touch. A check that the base does
not declare, or declares with another `run` or `paths`, always runs.

```text
$ cw run --base origin/main
== unit: satisfied, not run: its checked content is the same as in the base revision
== lint: true
== lint: passed in 0s
every declared check has a passing run on this content
```

Here the pull request changed only `README.md`. `unit` has
`paths = ["src/**"]`, so its checked content is the same as on `origin/main`;
`lint` has no `paths` and ran.

`--base` trusts that the base revision passed its checks. `cw` does not verify
that: it holds no record of the base in a fresh clone. Protect the base branch
so that only content that passed CI reaches it.
