# Operations

This is the reference of `kitsu`: every command, the file `kitsu.toml`, the exit
statuses, where `kitsu` keeps its records, and how to use it in continuous
integration. [The README](../README.md#install) says how to install it, and
[the quick start](quickstart.md) takes a repository to its first gated commit.
The [CLI guide](cli.md) introduces preferred command groups, host preferences, explicit
terminal editors, and shell completion. The detailed transcripts below retain existing alias
spellings; those commands continue to work without deprecation messages.

`kitsu` itself knows no programming language. A check is a program and its
arguments; `kitsu` starts it, waits for it, and records how it ended.

The examples on this page were produced on Linux with git 2.53. Temporary
paths are shortened to `<repository>`.

## Contents

- [Preferred commands](#preferred-commands)
- [Command line](#command-line)
- [kitsu agent check](#kitsu-agent-check)
- [kitsu task start](#kitsu-task-start)
- [kitsu init](#kitsu-init)
- [kitsu add](#kitsu-add)
- [kitsu run](#kitsu-run)
- [kitsu status](#kitsu-status)
- [kitsu stats](#kitsu-stats)
- [kitsu context](#kitsu-context)
- [kitsu hook](#kitsu-hook)
- [kitsu guard](#kitsu-guard)
- [kitsu uninstall](#kitsu-uninstall)
- [kitsu roles](#kitsu-roles)
- [kitsu skills](#kitsu-skills)
- [kitsu change](#kitsu-change)
- [kitsu advisers and consult](#kitsu-advisers-and-consult)
- [kitsu tokens](#kitsu-tokens)
- [kitsu version](#kitsu-version)
- [kitsu.toml](#kitsutoml)
- [Exit statuses](#exit-statuses)
- [Where kitsu keeps its records](#where-kitsu-keeps-its-records)
- [Continuous integration](#continuous-integration)
- [A tree without kitsu.toml](#a-tree-without-kitsutoml)

## Preferred commands

Run `kitsu help <group> <command>` for each leaf's operands, flags, defaults, and examples.
The preferred surface is:

| Path | Operands and purpose |
| --- | --- |
| `agent check` | Inspect local CLI, host, Kitsu plugin, task skill, and hook setup with evidence and recovery steps. |
| `task start` | Launch the selected interactive host with `<request>`, or prepare the handoff with `--print` or `--json`. |
| `task list`, `task show`, `task check` | List full and small histories, show `<task>`, or check `[<task>]` offline. Small histories keep their stage unknown. |
| `role list`, `role show`, `role text` | List selections, inspect `<role>`, or print its original instructions. |
| `role set`, `role edit`, `role reset`, `role check` | Change, explicitly edit, reset, or compare `<role>` with `--from <tree>`. |
| `skill list`, `skill show`, `skill resolve` | Discover, inspect `<skill>`, or resolve `<role>` to original paths and hashes. |
| `skill attach`, `skill detach` | Add or remove `<skill> --role <role>`; detach also works after the source disappears. |
| `skill clear`, `skill reset` | Use `--role <role>` to save `[]` or remove only its skill override and inherit again. |
| `adviser list`, `adviser show`, `adviser providers` | Inspect experimental configuration, one `<adviser>`, or offline provider capabilities. |
| `adviser bind`, `adviser status`, `adviser check` | Use positional `<task>` to save or inspect its policy and checkpoints. |
| `adviser ask` | Consult positional `<adviser>` with `--task`, `--role`, `--stage`, and `--request`; opt-in still applies. |
| `adviser accept`, `adviser reject`, `adviser defer` | Record `<id> --task <task> --reason <text>` with optional `--choice <id>`. |
| `check list`, `check add`, `check run`, `check stats` | Inspect checks, add `<id> -- <program> [args...]`, run `[<id>...]`, or report recorded runs. |
| `check trust` | Inspect and approve this exact declaration's commands for use by the agent plugin. |
| `gate init`, `gate status`, `gate uninstall` | Install hooks, inspect evidence, or detach hooks and retain records. Use `--purge` to delete the shared check and token store after a complete detach. |
| `usage show`, `usage record` | Report `--task <task>` or read a notice from stdin with explicit `--host`. |
| `config show`, `config get` | Inspect CLI preferences and their origins; get the `host` key. |
| `config set`, `config reset`, `config edit` | Set `host codex|claude`, reset `host`, or explicitly edit preferences; `--global` selects personal scope. |
| `completion bash`, `completion zsh`, `completion fish`, `completion powershell` | Print shell completion scripts without installation or writes. |
| `version`, `context`, `hook`, `guard` | Build information and advanced host/Git integration. |

Preferred host-aware commands use explicit `--host`, then nonempty `KITSU_HOST`, private
worktree `<git-dir>/kitsu-cli.toml`, and personal `os.UserConfigDir()/kitsu/config.toml` in that
order. Preferences currently hold only version 1 and the optional host. Default `config set`
and reset edit the worktree; `--global` edits the personal file. Shared role settings and
personal skills stay in their existing files. Legacy ingress keeps its existing host behavior.

Only `role edit` and `config edit` open configuration forms. They require stdin/stderr terminals, reject JSON,
support F1 field help and `--accessible`, preview changes, and revalidate before saving. Cancel
creates no configuration or lock files; concurrent edits abort the save. `task start` separately
launches an interactive host unless `--print` or `--json` is given. See
[the editor guide](cli.md#open-an-editor-when-you-want-one).
For these new editors, explicit Cancel exits 0 and Ctrl+C/context cancellation exits 130;
neither saves changes. Existing commands keep their exit-status behavior.

Existing aliases preserve their execution and output, including bare `roles`, `tokens`, `run`,
and `status`. `advisers resolve --disposition accept|reject|defer` remains available alongside
the separate preferred disposition commands. The sections below document those retained
spellings and their underlying behavior.

## Command line

- `kitsu help` prints the list of commands and the exit statuses, and exits 0.
  `kitsu` and preferred groups without a subcommand also print help and exit 0.
- `kitsu help <path>`, `kitsu <group> help <subpath>`, `kitsu <path> --help`, and
  `kitsu <path> -h` work at every supported group, leaf, and alias. Help runs before Git,
  configuration, required-operand, or terminal checks and never executes an action or opens UI.
- Invalid usage exits 2 with a concise error, leaf usage, and the exact help command. Typo
  suggestions do not execute a corrected action.
- Options may stand before or after the other arguments: `kitsu run lint --force`
  and `kitsu run --force lint` mean the same. An option takes its value as the
  next word or after `=`: `--base main` or `--base=main`.
- Everything after `--` in `kitsu check add` or its `kitsu add` alias is passed on unread.
- An error is printed on standard error with the prefix `kitsu:`.

## kitsu agent check

```text
kitsu agent check [--host codex|claude] [--json]
```

Inspects supported local host configuration without executing the host, model, hook, installer,
or authentication flow. It checks Kitsu on absolute PATH entries, the selected host executable,
Kitsu plugin installation and enablement for the scope, the task skill, the hook bundle, and
known disable settings. Unsupported evidence remains unknown. Any optional Git-gate finding
is separate from coordinator readiness: no kitsu.toml or installed Git hooks is required.

Text gives each component's evidence and next steps. JSON returns `host`, `ready`,
`host_executable`, and `checks`; each check has `id`, `status`, `summary`, `detail`, and `actions`.
Status is `pass`, `fail`, or `warn`. A required installation that cannot be established from the
supported inventory blocks readiness with an explanation, without proving the plugin absent.
`ready` means no known blocking local failure. It does not prove authentication, model access,
skill loading, hook trust, or actual hook execution. Use Codex `/hooks` to review and trust the
current exact definitions; [plugin hooks](hooks.md#verify-plugin-hook-setup) explains recovery.

Exit statuses: 0 no blocking local failure, including remaining runtime caveats; 5 local setup
is blocked; 2 invalid usage or no effective supported host; 1 inspection could not complete.
Host selection follows the preferred-command precedence above. The
[CLI guide](cli.md#check-setup-and-start-a-task) shows the install/check/start flow.

## kitsu task start

```text
kitsu task start <request> [--host codex|claude] [--print|--json]
```

Prepares a prompt that explicitly invokes Kitsu's task skill with the coding request. By default,
it runs local preflight, requires terminal stdin/stdout/stderr, then starts the selected interactive
`codex` or `claude` in the current directory. The prompt is a process argument, never a shell
program. The host controls authentication, models, permissions, and role execution; local checks
cannot prove its session behavior. Claude Code receives `/kitsu:task`; Codex is told to load the
installed Kitsu task skill. A new interactive daily launch in a Git worktree saves the request and a resumable task ID after preflight, before opening the host.

`--print` and `--json` are mutually exclusive handoff-only modes. They require no terminals or
installed host CLI, execute nothing, and return the prepared prompt and local diagnostics even
when setup is blocked. Print mode is suitable for pasting into a desktop session. JSON adds
`prompt` and `argv` to the diagnostic object; `argv` is the proposed executable and prompt.
Inspect `ready` and `checks`; successful preparation is not verified workflow execution.

Exit statuses: handoff-only modes return 0 after successful inspection, even when `ready` is
false; default launch returns 5 for failed local preflight or 2 for missing terminals; invalid
usage returns 2, inspection/process-start failure returns 1, and a launched host's status is
preserved. No host is executed by command help.

The direct launcher accepts native executables; `.cmd` and `.bat` host wrappers require a
native installation or a manual `--print` handoff.

## kitsu init

Creates `kitsu.toml` when the root of the working tree has none, installs the
three git hooks, and sets the mode of the gate. An existing `kitsu.toml` is kept
byte for byte.

```text
kitsu init [--observe]
```

| Option | Meaning |
| --- | --- |
| `--observe` | The hooks record what they would decide and allow every commit and push. Without it, `kitsu init` sets the gate to enforce. |

`kitsu init` writes the hooks `pre-commit`, `pre-merge-commit` and `pre-push` into
the `hooks` directory of the common git directory. It does not change a hook
file that another tool wrote, and it writes no hook at all when
`core.hooksPath` is set. [Hooks](hooks.md) explains both cases. Running
`kitsu init` again leaves the same hooks in place and only changes the mode.

```text
$ kitsu init
created kitsu.toml with no checks
installed the hooks pre-commit, pre-merge-commit, pre-push
the gate enforces: a commit or a push needs a passing run of every declared check
next: kitsu add <id> -- <program> [<argument>...], then kitsu run
```

In a clone whose `kitsu.toml` already declares checks, the first line is
`kitsu.toml is kept as it is` and the last is `next: kitsu run`.

When a hook is left for you to edit, the line to add is printed on standard
error:

```text
kitsu: these hooks were written by another tool, and kitsu did not change them.
kitsu: add the line shown to each hook. It must be the last command of that hook:
  <repository>/.git/hooks/pre-commit
      kitsu hook pre-commit "$@"
```

Exit statuses: 0 done; 1 not inside a git repository, or a file cannot be
written; 2 an argument other than `--observe`; 4 a hook of another tool does
not call `kitsu` yet. With 4, `kitsu.toml`, the mode and the free hooks are written.

## kitsu add

Appends one check to `kitsu.toml`. No earlier byte of the file changes.

```text
kitsu add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]
```

| Option | Meaning |
| --- | --- |
| `--paths <pattern>` | A path pattern the check depends on. Give it once for each pattern. Without it, the check depends on every path. |
| `--timeout <duration>` | The time the check may take, from `1s` to `6h`, such as `90s` or `5m`. Without it, the check gets the default of 10 minutes. |

The words after `--` become `run` exactly as the shell gave them to `kitsu`. `kitsu`
does not split or quote them again. The result must be a valid declaration:
when it is not, nothing is written and the reason is printed.

```text
$ kitsu add unit --paths 'src/**' --paths unit.sh -- sh unit.sh
added the check `unit` to kitsu.toml
$ kitsu add unit -- true
kitsu: the check was not added, because the file would not be a valid declaration:
kitsu.toml:15: the id `unit` is declared twice, at lines 5 and 15
`kitsu add` only appends a check; to change or remove one, edit kitsu.toml
```

`kitsu add` only appends. To change or remove a check, edit `kitsu.toml` by hand;
the file is the interface.

The first command appends this block:

```toml
[[check]]
id = "unit"
run = ["sh", "unit.sh"]
paths = ["src/**", "unit.sh"]
```

`kitsu add` does not check that the program exists, with one exception: a
program that is one word with a space in it, for which no such program exists,
is refused, because no shell reads `run`:

```text
$ kitsu add test -- 'sh test.sh && echo done'
kitsu: the program `sh test.sh && echo done` is one word with a space in it, and no such program exists; write the program and each argument as words of their own, with no quotes around the whole command
```

Any other program that cannot be started makes the run end `failed`. An option
that `kitsu add` does not know, given before `--`, names the place of `--`:

```text
$ kitsu add unit python3 -m unittest
kitsu: unknown option: -m; the program and its arguments go after `--`; usage: kitsu add <id> [--paths <pattern>]... [--timeout <duration>] -- <program> [<argument>...]
```

Exit statuses: 0 added; 1 no `kitsu.toml`, or the file would not be valid; 2 no
id, more than one id, nothing after `--`, an unknown option, or a program that
is one word with a space.

## kitsu run

Executes each declared check that has no passing run on the content of the
working tree, one at a time in the order of `kitsu.toml`, and records each run.
[Evidence](evidence.md) explains what "the content" is and when a run counts.

```text
kitsu run [<id>...] [--force] [--base <rev>]
```

| Option | Meaning |
| --- | --- |
| `<id>...` | Consider only these checks. An id that `kitsu.toml` does not declare is a usage error. |
| `--force` | Execute the selected checks even when they have a passing run on this content. |
| `--base <rev>` | Count a check as passed when its checked content is the same as in the revision `<rev>`. For continuous integration; see [Continuous integration](#continuous-integration). |

For each check, `kitsu run` takes a snapshot of the working tree: the tree that
`git add -A` followed by `git commit` would record. The index and the files on
disk do not change. The program runs from the root of the working tree,
without a shell, with the environment of `kitsu run` and no standard input. Its
output goes to the terminal and to a log. After it exits, `kitsu` takes the
snapshot again. A run ends with one outcome:

| Outcome | When |
| --- | --- |
| `passed` | The program exited 0 and the snapshot after it equals the snapshot before it. |
| `failed` | The program exited with another status, could not be started, or was ended by something outside `kitsu`, such as a signal. The text of the run says which. |
| `timed_out` | The timeout passed first. `kitsu` stopped the program and the processes it started. |
| `source_changed` | The source or another verified input changed during the run. The receipt retains the process exit status when available. For a changed tree, the log names up to 20 paths. Inspect tracked edits and keep generated output outside the verified files. |
| `interrupted` | `kitsu` received SIGINT, SIGTERM or SIGHUP, or on Windows Ctrl+C, or the `kitsu` process died. |

```text
$ kitsu run
== unit: sh unit.sh
unit ok
== unit: passed in 0s
== lint: true
== lint: passed in 0s
every declared check has a passing run on this content
$ kitsu run
== unit: satisfied, not run again: run 20261114T144211Z-f48b9af5 passed on this content
== lint: satisfied, not run again: run 20261114T144211Z-44b14087 passed on this content
every declared check has a passing run on this content
```

A failure names the log:

```text
== unit: sh unit.sh
app is not ok
== unit: failed with exit status 3
   log: <repository>/.git/kitsu/logs/20261114T144217Z-80b277b8.log
== lint: true
== lint: passed in 0s
not verified: 1 of 2 checks did not pass; fix what each log shows, then run `kitsu run` again
```

A run in which a check did not pass ends with that last line and exits 5.

When a named subset passes and other checks still lack a run, the last line is
`not yet verified; still missing:` and their ids, and the exit status is 0.
When no check is declared, `kitsu run` prints
`no checks are declared, so there is nothing to run`.

`kitsu run` also removes old records: see
[Where kitsu keeps its records](#where-kitsu-keeps-its-records).

On Windows the code looks the program up itself, a bare name on `PATH` and a
path from the root of the repository, also when `kitsu run` starts in a
subfolder, and starts it by the full path it found, so that `cmd.exe` does not
look a batch file up again. It refuses a `.bat` or `.cmd` program whose arguments hold a character other than
letters, digits and `_ . / : = + , @ \ -`, because `cmd.exe` could read such an
argument as a second command. Ctrl+C in the console reaches `kitsu`, not the
check, and the run is recorded as `interrupted`. This code has not been run on
Windows by hand.

Exit statuses: 0 every check it executed passed, or nothing needed a run; 1 the
declaration is missing or invalid, the working tree cannot be recorded, the
records cannot be written, or `--base` names no tree, or on Windows a batch
file was refused; 2 an unknown id, an unknown option, or `--`; 5 a check it
executed did not end `passed`.

## kitsu status

Says whether a commit of the working tree would be allowed, lists each
declared check with its state, and compares the index with the working tree.
It executes no check.

```text
kitsu status [--json]
```

| Option | Meaning |
| --- | --- |
| `--json` | Print one JSON object instead of text. |

The state of a check is `satisfied`, `not run`, `failed` or `timed out`.
[Evidence](evidence.md#what-kitsu-status-says) lists what each state means and the
text that goes with it.

```text
$ kitsu status
not verified: a commit of this content would be denied
  unit  failed with exit status 3; log: <repository>/.git/kitsu/logs/20261114T144217Z-80b277b8.log
  lint  satisfied
index: differs from the working tree; not staged:
  README.md
  src/app.txt
a commit of the index alone would be allowed
run `kitsu run` to produce the missing evidence
```

The lines that start with `index:` compare the tree of the index, which is what
`git commit` records, with the snapshot of the working tree. When they differ,
up to 20 paths that are not staged are listed, and `kitsu status` says whether a
commit of the index alone would be allowed. In observe mode a line
`mode: observe` is added. When the hooks of `kitsu` are not installed in this
clone, a warning that names `kitsu init` is printed on standard error, and a first
line that would say `not verified: a commit of this content would be denied`
says instead `not verified, but no hook of kitsu judges a commit in this clone yet`.

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
      "log": "<repository>/.git/kitsu/logs/20261114T144217Z-80b277b8.log"
    },
    {
      "id": "lint",
      "state": "satisfied",
      "run_id": "20261114T144217Z-84d464ba",
      "tree": "92c70cfe788ecf26569e0146068a231b83c1ce1b",
      "outcome": "passed",
      "exit_status": 0,
      "ended_at": "2026-11-14T14:42:17Z",
      "log": "<repository>/.git/kitsu/logs/20261114T144217Z-84d464ba.log"
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
| `hooks` | `installed` when each of the three hook files git would run exists, can be executed and calls `kitsu hook`; otherwise `missing`. |
| `checks` | One entry per declared check: `id`, `state` (`satisfied`, `not_run`, `failed`, `timed_out`), `running` when a run executes now, and the fields of the run that decides the state, when one does. |
| `not_staged`, `not_staged_more` | Up to 20 paths in which the index differs from the working tree, and the count of the rest. |
| `warnings` | Warnings, such as a record that cannot be read. |

Exit statuses: 0 the working tree is verified, or no check is declared; 1 the
declaration is missing or invalid, the working tree cannot be recorded, or the
records cannot be read; 2 an argument; 5 not verified. Whether the hooks are
installed does not change the exit status.

## kitsu stats

Counts what the records of this repository hold: runs of each check by
outcome, passes after a failure on the same checked content, and hook
decisions.

```text
kitsu stats [--since <period>] [--json]
```

| Option | Meaning |
| --- | --- |
| `--since <period>` | Count the records of this period: a number and a unit, `m`, `h`, `d` or `w`, such as `90m`, `36h`, `7d` or `2w`. The default is 30 days. |
| `--json` | Print one JSON object instead of text. |

```text
$ kitsu stats
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
- A hook decision is recorded when the hook runs, before git goes on. git runs
  `pre-commit` also when it then finds nothing to commit, so such an attempt
  counts as `allowed` although no commit was made. A push records one decision
  for each ref it updates.

`--json` prints the same as one object with the fields `since`, `until`,
`runs`, `pass_after_failure`, `decisions` and `unreadable`.

`kitsu stats` reads no network and sends nothing anywhere: the counts stay in the
repository.

Exit statuses: 0 printed; 1 not inside a git repository, or the records cannot
be read; 2 an unknown option, a period that is not valid, or an argument.

## kitsu context

Introduces Kitsu's task workflow at the start of a session in a Git repository, followed by
the supporting gate's state when configured. The workflow introduction names the host's task
skill, `kitsu agent check`, and `kitsu task start`; it does not start work. The agent plugin runs
it; see [Hooks](hooks.md#hooks-of-the-agent-plugin).

```text
kitsu context [--host claude|codex]
```

| Option | Meaning |
| --- | --- |
| `--host claude`, `--host codex` | Read the host's notice, a JSON object, from standard input, and judge the directory its `cwd` names. The plugin passes it, because a host does not always start a hook in the directory the session works in. |

Without `--host`, `kitsu context` reads nothing and judges the working directory.

```text
$ kitsu context
Kitsu verifies coding tasks: select Kitsu's task skill in your host. Setup: `kitsu agent check --host <host>`.

This repository is gated by kitsu. git allows a commit or a push only when every kitsu.toml check has a passing run on exactly that content.
- `kitsu run` records missing checks and `kitsu status` lists them. Approval is the developer's: `kitsu check trust`.
- Never bypass a denied commit or push.
The hooks are installed.
```

In observe mode the last line says that the gate only observes. When the hooks
are not installed in this clone, the workflow introduction stays first, and the first gate
paragraph is this one; the installed-hooks line is left out:

```text
This repository declares checks for kitsu, but the hooks are not installed in this clone, so nothing is gated here yet; `kitsu init` installs them. Once they are installed, git allows a commit or a push only when every kitsu.toml check has a passing run on exactly that content.
```

Without `kitsu.toml`, a Git repository still gets the workflow introduction, followed by a line
that no checks are declared and passing evidence has not been established. The introduction and
the gate paragraph stay within 620 characters. A host session adds one line with its ID. Outside a
Git repository, it prints nothing.

Exit statuses: 0 always, also outside a repository; 2 an argument.

## kitsu hook

Decides for git whether a commit, a merge commit or a push goes through. The
hook files that `kitsu init` writes call it; you do not run it yourself. It
decides from the records alone: it executes no check and waits for none.
[Hooks](hooks.md) describes each decision.

```text
kitsu hook <name> [<argument>...]
```

`<name>` is `pre-commit`, `pre-merge-commit` or `pre-push`. The arguments and
the standard input are the ones git gives the hook. For `pre-push`, the first
argument is the name of the remote, and the standard input lists the refs that
the push updates.

A denial is printed on standard error:

```text
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  lint: not run on this content
kitsu: to produce the missing evidence, run:
  kitsu run
```

Exit statuses: 0 allowed, or observe mode; 2 no name or an unknown name; 3
denied. Outside a git repository the hook denies with 3.

## kitsu guard

Reads the notice that Claude Code or Codex gives a hook before a tool call. It checks for developer approval before recognized Kitsu check execution and guards edits to local gate settings, hooks, configuration and records. Git hooks decide whether to accept a commit or push. See [Hooks](hooks.md#hooks-of-the-agent-plugin).

```text
kitsu guard [--host claude|codex]
```

| Option | Meaning |
| --- | --- |
| `--host claude` | The default. The answer asks the user. |
| `--host codex` | The answer denies the call with the reason, because Codex does not ask from a hook. The reason adds that the user can run the call themselves. |

Both hosts deny check execution when the declaration still needs developer approval. Run `kitsu check trust` in your terminal and inspect its commands before approving.

Both hosts also deny these shell commands of an agent, in any repository:

- `kitsu check trust`, which is the developer's approval;
- `kitsu tokens record` and `kitsu usage record`, which only the host's hooks run to report sessions and reviewers;
- a command line that names the task ledger (`kitsu-stages` in the git directory) or the approvals folder (`kitsu/check-trust` in your user configuration) and does more than read it. Reading means `cat`, `head`, `tail`, `less`, `more`, `grep`, `rg`, `ls`, `wc`, `jq`, `stat`, `diff` or `cmp` with no redirection. Any other command on the same line counts as a write.

The notice is a JSON object on standard input with `tool_name`, `cwd` and
`tool_input`. The guard reads `command` of the tools `Bash` and `PowerShell`,
also when it is a list of words, `file_path` of the tools `Edit`, `Write` and
`MultiEdit`, and the file names in the patch of `apply_patch`. Where the
working directory is not in a git repository whose root holds `kitsu.toml`, it
prints nothing.

### What it asks about

The guard splits a shell command into simple commands and words, as a shell
does: quotes, backslashes, `;`, `&&`, `||`, `|`, `&`, newlines, `(`, `)`, `$(`
and backticks. It steps over `NAME=value` words and the wrappers `env`,
`command`, `sudo`, `time`, `nice`, `xargs`, `timeout`, `exec`, `nohup`,
`script`, `unbuffer`, `setsid` and `stdbuf`. It reads the words after `eval`,
the text after `-c` of `sh`, `bash`, `zsh` and `dash`, and a wrapper's quoted
command such as `script -c "…"`, as a command, to a depth of three. A command speaks up when:

- a call of `git`, `/path/git` or `git.exe` skips the hooks:
  - a word after the subcommand is `--no-verify`, or a prefix of it at least
    as long as `--no-veri`, which git takes for the whole option;
  - the subcommand is `commit` and a cluster of short options holds `n`, as in
    `-n`, `-an` or `-nm`; the cluster ends at `m`, `F`, `C`, `c` or `t`, which
    take a value, and at `S` or `u`, which take one only when it is attached;
  - an option before the subcommand, `-c` or `--config-env`, sets
    `core.hooksPath`, in any case;
  - the subcommand is `config`, a word names `core.hooksPath`, in any case, and
    a value follows the name or the call unsets it;
- a `NAME=value` word before the program holds `core.hooksPath`;
- it runs `kitsu uninstall`, or `kitsu init` with `--observe`;
- it holds a patch, a line `*** Update File:`, `*** Add File:`,
  `*** Delete File:` or `*** Move to:`, that names a guarded file.

Words after `--` are not options, and the value of `-m`, `-F`, `--message`,
`--file`, `--author` and other options of `commit` that take one is not an
option. A command with an unclosed quote is judged by its text: it speaks up
when it holds `--no-verify`.

An edit, or a patch, speaks up when it names one of these, or a file or folder
inside it:

- `kitsu.toml`, `kitsu-roles.toml`, or `kitsu-advisers.toml` at the root of the working tree;
- `hooks/` of the common git directory, and the directory `core.hooksPath`
  names;
- `config` of the common git directory;
- `kitsu/` of the common git directory, which holds the records.

A path is compared as the file system sees it, with each folder above it, so
another spelling of a name, a symbolic link to a guarded file, and a file that
does not exist yet inside a guarded folder are judged like the place they lead
to.

```text
$ echo '{"tool_name":"Bash","cwd":"<repository>","tool_input":{"command":"git commit -n -m x"}}' | kitsu guard
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"ask","permissionDecisionReason":"This command gives git commit the option -n, the short form of --no-verify, which skips the commit gate: the git hooks of kitsu would not deny the commit or the push."}}
$ echo '{"tool_name":"Bash","cwd":"<repository>","tool_input":{"command":"kitsu uninstall"}}' | kitsu guard --host codex
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"This command runs `kitsu uninstall`, which skips the commit gate: the git hooks of kitsu would not deny the commit or the push. This host cannot ask from a hook, so the call is refused. Ask the user; they can run it themselves."}}
```

### What it misses

The guard reads words; it runs nothing and expands nothing. It misses, on
purpose:

- a shell command that writes a guarded file itself, such as a redirection
  (`echo x >> kitsu.toml`), `sed -i` or an editor. The task ledger and the
  approvals folder are the exception: a command line that names them is denied;
- a program or script that the agent writes and then runs. The guard reads the
  command line, not the file, so such a script can send a review notice or
  write the ledger. A host-observed review and a terminal approval are
  evidence, not proof against an agent that sets out to forge them;
- a file of another repository named by an absolute path: only the files of
  the repository of the notice's `cwd` are guarded;
- input sent to a program that already runs, such as `write_stdin` on Codex,
  for which Codex runs no hook;
- variables, aliases and functions: `git commit $FLAGS`, or a git alias that
  adds `--no-verify`, is not recognised.

In each case the git hooks still decide about the commit, and the push rule
judges the commit when it is pushed through the hooks.

Exit statuses: 0 in every case it reads, also when the notice is not JSON; 2 an
argument or a host other than `claude` and `codex`. The commands of the plugin
end with exit status 0 whatever `kitsu` returns.

## kitsu uninstall

Removes the hook files that `kitsu` wrote. Check and token records are retained by default. After complete detachment, `--purge` deletes the shared check store. `kitsu.toml` and every hook file of another tool stay as they are. In
`hooks/` of the common git directory, a hook file that holds the marker line of
`kitsu` is removed. In a directory that `core.hooksPath` names, which belongs to
its owner, only a file that is exactly the script `kitsu` writes is removed; every
other file stays, also one with the marker that was edited.

```text
kitsu uninstall
```

```text
$ kitsu uninstall
removed the hooks kitsu wrote, 3 in all
retained check and token records in <repository>/.git/kitsu
kitsu.toml is left as it is
```

When a hook file that it leaves still calls `kitsu hook`, `kitsu uninstall` names it
and prints the line of that file that holds the call:

```text
these hooks are not as kitsu wrote them and still call it; remove the call from each:
  <repository>/.git/hooks/pre-commit
      kitsu hook pre-commit "$@"
```

Exit statuses: 0 done; 1 not inside a git repository, or a file cannot be
removed; 2 an argument; 4 a hook of another tool still calls `kitsu`.

## kitsu roles

Prints or configures the seven roles of the task workflow, checks which paths a role changed
outside its limit, or prints the text of one role. [Roles](roles.md) describes
them.

```text
kitsu roles [--host claude|codex] [--task <task>] [--json]
kitsu roles set <role> --host claude|codex [--model <model>] [--reasoning <effort>] [--skills <ids>] [--root <directory>] [--global] [--json]
kitsu roles reset <role> --host claude|codex [--global] [--json]
kitsu roles check <role> --from <tree> [--task <task>] [--finalize]
kitsu roles text <role>
```

`--finalize` is only for `spec-writer` with `--task`, after a finished change
has shipped. It replaces the ordinary write limit with the exact current
specification paths for that task's capabilities in the `--from` tree; see
[finalization](workflow.md#the-archive).

`--host` selects a compact role/model/reasoning/advisers table with a skills line for each role,
including the coordinator's host session. `--task` reads adviser access from a saved task binding
without personal adviser configuration.

`set` saves supplied overrides in the repository's `kitsu-roles.toml`; an omitted option keeps
its current setting. `--skills` is a comma-separated list; `--skills ''` saves an empty list,
suppressing inherited personal skills. `--root` is repeatable and needs `--skills`.
`reset` removes the selected role/host's repository model, effort, and skill overrides.
Both print the resulting selection. `--global` instead edits only personal skill bindings at
`os.UserConfigDir()/kitsu/roles.toml` and rejects model or reasoning options. Repository skills
replace personal lists. The coordinator accepts skills while its model stays in the host session.
See [roles](roles.md#changing-a-model) and [skills](skills.md) for examples and file handling.

Exit statuses: 0 done, or the role stayed within its limit; 1 the role
configuration cannot be read, `--from` names no tree, or the working tree
cannot be recorded, or finalization finds no capability specification for
the task in the starting tree; 2 a wrong command line or an unknown role;
5 the role changed a path outside its limit.

## kitsu skills

Discover existing local skill sources or resolve a role's selected originals for a worker brief.
[Skills](skills.md) describes the supported sources, qualified ids, layered bindings, and limits.

```text
kitsu skills list --host codex|claude [--root <directory>] [--json]
kitsu skills resolve <role> --host codex|claude [--root <directory>] [--task <task>] [--workflow full|small] [--json]
```

`--root` is repeatable. These commands do not install, copy, or execute skills and start no host
or model process. Resolution refuses missing, ambiguous, invalid, host-disabled, and manual-only
selections and returns original source paths and SHA256 hashes. Without `--task` it is read-only.
With a task it appends a portable immutable receipt, defaulting to `full`, which requires an
existing task folder. Use `--workflow small` explicitly for a small task. Shared receipts omit
machine absolute paths and skill bodies and record `load_status: unverified`.

Exit statuses: 0 printed or resolved; 1 discovery, selection, configuration, or record failure;
2 invalid arguments, host, role, task, or workflow. A successful selection proves no loading.

## kitsu change

Prints the stage of a task's change folder under `openspec/changes/`, or checks
the rules its files must obey. [The task workflow](workflow.md) describes the
folder and the rules.

`change check` also discovers small-task adviser histories under `openspec/changes/.advice/`
and skill receipts under `openspec/changes/.skills/`, as well as full tasks' `skills/`.
Naming a small task checks its saved history without requiring a full change folder. It validates
structure and references, without requiring future checkpoint answers. `change status` remains
full-path only; use `advisers status` for small tasks.

```text
kitsu change status [<task>] [--json]
kitsu change check [<task>]
```

Exit statuses: 0 done, or no rule is broken; 1 the folder does not exist or
cannot be read; 2 a wrong command line or a task name that is not lower-case
words joined by hyphens; 5 `kitsu change check` found a broken rule.

## kitsu advisers and consult

Configure optional, experimental native decision providers or adviser CLIs, bind their policy to a task, ask a question,
and record how the answer was addressed. [Advisers](advisers.md) describes the two configuration
locations, role access, version 1 and 2 request JSON, shared records, and required checkpoints.
No advisers or required adviser checkpoints are enabled by default. Opt in with a definition
and repository role access; credentials or provider discovery alone enable nothing.

```text
kitsu advisers providers [--json]
kitsu advisers list [--role <role>] [--task <task>] [--json]
kitsu advisers bind --task <task> --workflow full|small [--refresh] [--json]
kitsu consult --task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]
kitsu advisers resolve --task <task> <id> --disposition accept|reject|defer --reason <text> [--choice <id>] [--json]
kitsu advisers status --task <task> [--checkpoint <id>] [--json]
kitsu advisers check --task <task> [--checkpoint <id>] [--json]
```

Only `kitsu consult` executes an adviser, once, with the configured CLI's authentication or a native
provider API key from its configured environment variable. `providers` is an offline catalog and
needs no repository. Its JSON includes `experimental: true` and `enabled_by_default: false`.
Native configuration uses version 2; existing CLI configuration remains valid.
The coordinator runs it on a role's behalf after checking that role's writes. Its request names
explicit repository files; a failed attempt remains recorded. No API keys are saved, and there
is no automatic retry or model fallback. Resolving an answer requires a reason; an optional
`--choice` must name an offered option. Adviser dispositions replace no review or human approval.

Exit statuses: 0 the operation completed, including a status report whose checkpoints are not
ready; 1 configuration, record, execution, or file failure; 2 invalid arguments or request JSON;
5 `advisers check` found an unsatisfied checkpoint.

## kitsu tokens

Reports the tokens a task used and the share the workflow itself took, or
records them from the notice of a host. [Tokens](tokens.md) says what is
counted.

```text
kitsu tokens [--task <task>] [--json]
kitsu tokens record [--host claude|codex]
```

`kitsu tokens record` reads the notice of the host on standard input. It records
nothing outside a git repository whose root holds `kitsu.toml`.

Exit statuses: 0 done; 1 no start is recorded for the task, the notice is not a
JSON object, or the records cannot be read or written; 2 a wrong command line.

## kitsu version

Prints the version, the revision, the build time, and the system and the
architecture it was built for. `kitsu --version` does the same.

```text
kitsu version
```

```text
$ kitsu version
kitsu v3.0.0-20261114142910-90aee1311da8+dirty
revision 90aee1311da859414c55b106f7a5655475bc2bc9
built 2026-11-14T14:29:10Z
linux/amd64
```

Exit statuses: 0 printed; 2 an argument.

## kitsu.toml

`kitsu.toml` at the root of the working tree declares the checks of the
repository. It is committed like any other file. `kitsu` reads it from the tree it
judges and from no other place: no file in the home directory, no environment
variable and no option adds, removes or changes a check. `kitsu run` and
`kitsu status` read it from the snapshot of the working tree, so a `kitsu.toml` that
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

The programs above are examples from three ecosystems. `kitsu` treats each of
them the same way: as a program and its arguments.

| Key | Required | Meaning |
| --- | --- | --- |
| `version` | yes | The integer `1`. |
| `[[check]]` | no | One table per check. A file with no check is valid, and then every commit is allowed. |
| `id` | yes | The name of the check: a lower-case letter or a digit, then up to 39 lower-case letters, digits, `_` or `-`. Unique in the file. Not `con`, `prn`, `aux`, `nul`, `com0` to `com9` or `lpt0` to `lpt9`, which Windows reads as devices; they are refused on every system. |
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
- A symbolic link is one entry, matched by its own path. A submodule is one
  entry too, and it counts for every check that has `paths`, whatever the
  patterns are.
- A pattern matches files. A pattern that names a folder of the tree, such as
  `src`, is an invalid declaration; write `src/**`.

[Evidence](evidence.md#what-paths-changes) says what `paths` changes and what
it promises.

`kitsu` refuses a declaration that breaks a rule. Every broken rule is printed
with the file name and the line, and no check runs. A hook denies. The messages
are these:

| Problem | Message |
| --- | --- |
| Not valid TOML | `kitsu.toml:4: not valid TOML: toml: array is incomplete`, with the reason of the TOML reader |
| `version` missing, not an integer, or not 1 | `` kitsu.toml:1: `version` must be 1 `` |
| A key `kitsu` does not know | ``kitsu.toml:5: unknown key `tiemout` `` |
| No `id` | ``kitsu.toml:2: a check needs an `id` `` |
| An `id` of the wrong form | ``kitsu.toml:16: `id` must match [a-z0-9][a-z0-9_-]{0,39} `` |
| The same `id` twice | ``kitsu.toml:15: the id `unit` is declared twice, at lines 5 and 15`` |
| No `run` | ``kitsu.toml:2: a check needs `run`, an array of the program and its arguments`` |
| `run` as one string, empty, or with an empty program | ``kitsu.toml:4: `run` must be an array of strings that begins with a program, such as ["go", "test", "./..."]`` |
| `paths` empty or not an array of strings | ``kitsu.toml:5: `paths` must be an array of one or more patterns`` |
| A pattern that is empty, starts or ends with `/`, or holds `//` | ``kitsu.toml:5: the path pattern `/src/**` must be relative to the repository root, with no empty segment`` |
| A pattern that names a folder of the tree | ``kitsu.toml:9: the pattern `src` of the check `folder` names a folder, and a pattern matches files; for everything in the folder write `src/**` `` |
| An `id` that Windows reads as a device | ``kitsu.toml:10: `id` must not be `nul`, which Windows reads as the name of a device`` |
| `timeout` not a string, not a duration, or outside 1s to 6h | ``kitsu.toml:5: `timeout` must be a duration from 1s to 6h, such as "5m"`` |
| No `kitsu.toml` at the root of the tree | ``the tree has no declaration: there is no kitsu.toml at its root; `kitsu init` creates one``; a hook says instead that the tree holds no `kitsu.toml` and names [A tree without kitsu.toml](#a-tree-without-kitsutoml) |

A `kitsu.toml` that is not a regular file in the tree, such as a symbolic link, is
an invalid declaration.

## Exit statuses

Kitsu uses these statuses for its own decisions.

| Status | Meaning |
| --- | --- |
| 0 | Done: `kitsu` did what was asked, and the answer is yes. |
| 1 | Could not: `kitsu` could not do what was asked, and says why. |
| 2 | Wrong command line: an unknown command, option or check id, or a missing argument. |
| 3 | A hook denies the commit, the merge or the push. |
| 4 | A hook of another tool is left for you to edit: `kitsu init` or `kitsu uninstall` printed the line to add or to remove. |
| 5 | Not verified: a check did not pass, a role/change rule or adviser checkpoint is unsatisfied, or agent onboarding has a blocking local failure. |
| 130 | A new explicit role/config editor was interrupted by Ctrl+C or context cancellation; nothing was saved. |

An explicitly launched `task start` host retains its own exit status. Handoff-only task
preparation returns 0 when its report is produced; inspect `ready` and component diagnostics.

## Where kitsu keeps its records

`kitsu` keeps local gate evidence and measured host usage under `kitsu/` in the common git directory. In a
plain clone that is `.git/kitsu/`. All linked worktrees of a repository share the
common git directory, so they share the records: a run made in one worktree
counts in another for the same content. Kitsu preserves embedded newlines in checkout and Git directory names when resolving these paths. If Git cannot read the repository configuration, Kitsu reports the Git error.

Shared adviser bindings, prompts, answers, and dispositions live in the worktree instead:
`openspec/changes/<task>/advisers/` on the full path or
`openspec/changes/.advice/<task>/` on the small path. Commit that history with the task; see
[adviser records](advisers.md#shared-history-and-usage).
Skill selection receipts similarly live under `openspec/changes/<task>/skills/` or
`openspec/changes/.skills/<task>/`. These records hold ids and hashes, not installed skill bodies
or machine absolute paths; see [skills](skills.md#record-a-worker-selection).
Private CLI preferences use the worktree Git directory's `kitsu-cli.toml` and the personal
`os.UserConfigDir()/kitsu/config.toml`. They select a CLI host and contain no task stage or
shared role policy; see [host preferences](cli.md#select-your-host-once).
Gate removal deletes its evidence and hooks while retaining these CLI preferences.

```text
kitsu/mode                                      enforce or observe
kitsu/runs/<check id>/<content key>/<run id>.json   one record per run
kitsu/runs/<check id>/<content key>/<run id>.lock   held while the run executes
kitsu/runs/<check id>/<content key>/seq.lock        held while the end of a run is recorded
kitsu/logs/<run id>.log                         the output of one run
kitsu/decisions/<yyyy-mm>.jsonl                 one line per hook decision
kitsu/decisions/write.lock                      held while a decision is appended
kitsu/gc.lock                                   held while old records are removed
kitsu/tmp/                                      copies of the index, only while git works on them
kitsu/tokens/                                   token records; see tokens.md
```

- A run id is the start time in UTC and eight random hex digits, such as
  `20261114T144211Z-f48b9af5`.
- Logs, run records, decision files, the mode file and the token records are
  created readable and writable by the user only (`0600`). The folder `kitsu/`,
  when `kitsu init` or `kitsu run` creates it, and the folders of runs, logs and
  decisions get `0700`; the folders under `kitsu/tokens/` get `0755`.
- A log keeps at most the first 1 MiB and the last 9 MiB of the output, with a
  line between them that says how many bytes were dropped.
- A record that cannot be read is renamed to `<name>.damaged` by the next
  decisive run of its check on the same content.
- `kitsu run` removes logs older than 30 days, records of runs and files of
  decisions older than 365 days, and index copies older than one day. The age
  is the time the file was last written. For an index copy it is the time its
  command started, so a copy in use stays. When another `kitsu run` is already
  removing, this one skips it.

The gate/check commands keep their worktree declaration in `kitsu.toml`: `kitsu init` creates it,
and `kitsu add` writes the longer file to a temporary `.tmp-*` file beside it, syncs
it to disk and renames that over it. Role, task, skill, and CLI settings use the locations
described above. The hook files are in the git directory. A check's program runs in the working tree
and can write there; `kitsu` records such a run as `source_changed`.

`kitsu uninstall` removes the hook files that hold the marker line of `kitsu` in
`hooks/` of the common git directory, and the files that are exactly the
script of `kitsu` in a directory that `core.hooksPath` names. Only an explicit `--purge` after successful detachment removes the common `kitsu/` directory with runs, logs, decisions, mode and token records. It
leaves configuration, private CLI preferences, shared OpenSpec history, every other hook file,
and the agent plugin.

Only explicit native adviser consultations start provider API connections. Adviser CLIs and
declared check commands may also use the network. Discovery, record checks, gate decisions, and
token collection remain offline. Every git call of `kitsu` sets
`GIT_NO_LAZY_FETCH=1`, so that git fetches no missing object in a partial
clone; `kitsu` then reports the object as missing.

## Continuous integration

The git hooks run only in a clone where `kitsu init` ran, and
`git commit --no-verify` skips them. A job in continuous integration runs the
checks again on the content of the pull request, so a commit that went around
the hooks still meets them there. Records are not shared between clones, so a
fresh clone in CI starts with none.

CI runs what the pushed `kitsu.toml` declares. A commit that removes a check or
weakens it, for example with `run = ["true"]` or narrower `paths`, passes CI as
well. The commit hook prints a notice that names each check that is removed or
changed, and that notice is all `kitsu` does about it. Protect `kitsu.toml` by
review, for example by naming its owners in a code owners file of the hosting
service.

```sh
git fetch origin main
kitsu run --base origin/main
```

`kitsu run` needs no `kitsu init` and no hooks. It exits 0 when every check it
executed passed and 5 when one did not, so the job fails on 5. The hook
warning of `kitsu status` does not appear, because `kitsu run` does not print it.

A job that only runs does not stop a merge. Make it a required status check
in the branch protection of the hosting service, so that a change whose job
failed, or never ran, cannot be merged. That setting is the only part of the
gate that a person or an agent with write access to a clone cannot skip:
some tools commit without the hooks, and a hook directory can be replaced. It
does not protect the declaration itself: a weakened `kitsu.toml` is what the job
runs.
[What other tools taught us](other-tools.md#what-speaks-against-parts-of-kitsu)
names the reports.

`--base <rev>` does this, according to the code:

1. It resolves `<rev>` to a tree. When `<rev>` names none, `kitsu run` exits 1 with
   ``the base revision `<rev>` names no tree in this repository``.
2. It reads `kitsu.toml` from that tree. When the base has no `kitsu.toml`, or an
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
   `kitsu status` and the hooks never use it.

A check without `paths` depends on the whole tree, so the base counts it only
when the working tree is the base tree. In practice `--base` saves the runs of
checks with `paths` that a change does not touch. A check that the base does
not declare, or declares with another `run` or `paths`, always runs.

```text
$ kitsu run --base origin/main
== unit: satisfied, not run: its checked content is the same as in the base revision
== lint: true
== lint: passed in 0s
every declared check has a passing run on this content
```

Here the pull request changed only `README.md`. `unit` has
`paths = ["src/**"]`, so its checked content is the same as on `origin/main`;
`lint` has no `paths` and ran.

`--base` trusts that the base revision passed its checks. `kitsu` does not verify
that: it holds no record of the base in a fresh clone. Protect the base branch
so that only content that passed CI reaches it.

## A tree without kitsu.toml

A hook denies a commit or a push whose tree holds no `kitsu.toml` at its root,
also when that is on purpose. The denial names this section:

```text
kitsu: this commit is denied: the tree has no declaration: it holds no kitsu.toml at its root. When that is on purpose, see https://github.com/belevtsev/kitsu/blob/main/docs/operations.md#a-tree-without-kitsutoml
```

### A project that stops using kitsu

The commit that removes `kitsu.toml` is denied in every clone that has the hooks.
The person who removes it runs `kitsu uninstall` in their clone first, and then
commits:

```text
$ git rm -q kitsu.toml
$ git commit -m "stop using kitsu"
kitsu: notice: this commit changes the required checks: `unit` is no longer declared
kitsu: this commit is denied: the tree has no declaration: it holds no kitsu.toml at its root. When that is on purpose, see https://github.com/belevtsev/kitsu/blob/main/docs/operations.md#a-tree-without-kitsutoml
$ kitsu uninstall
removed the hooks kitsu wrote, 3 in all
retained check and token records in <repository>/.git/kitsu
$ git commit -m "stop using kitsu"
```

The commit is made, and the push goes through: no hook of `kitsu` is left in that
clone.

Every teammate runs `kitsu uninstall` in their own clone after they pull. Until
then each commit and each merge in that clone is denied with "the tree has no
declaration". A pull that merges is denied, and the merge stays in progress;
`kitsu uninstall` and then `git commit --no-edit` conclude it:

```text
$ git pull --no-rebase --no-edit
kitsu: notice: this commit changes the required checks: `unit` is no longer declared
kitsu: this merge is denied: the tree has no declaration: it holds no kitsu.toml at its root. When that is on purpose, see https://github.com/belevtsev/kitsu/blob/main/docs/operations.md#a-tree-without-kitsutoml
Not committing merge; use 'git commit' to complete the merge.
$ kitsu uninstall
removed the hooks kitsu wrote, 3 in all
retained check and token records in <repository>/.git/kitsu
$ git commit --no-edit
```

Do not run `kitsu init` there: it would create `kitsu.toml` again.

### A branch with other content

A branch that holds other content than the project, such as a documentation
site on an orphan branch, needs a `kitsu.toml` that declares no check. Commit one
that holds only:

```toml
version = 1
```

A tree whose `kitsu.toml` declares no check is allowed, so commits and pushes of
that branch go through, and `kitsu status` there prints
`no checks are declared, so every commit is allowed`.

### Notes

A push of a ref under `refs/notes/`, such as `git push origin refs/notes/commits`,
is not judged: notes hold no content of the project. See
[Hooks](hooks.md#pre-push).
