# Find and configure a Kitsu command

Start with `kitsu` or `kitsu help`. Both print help and exit successfully. Commands are grouped
by the thing you work with:

| Group | Commands |
| --- | --- |
| `agent` | `check` |
| `task` | `start`, `brief`, `list`, `show`, `check` |
| `context` | `collect`, `show`; bare `context` keeps session-hook output |
| `role` | `list`, `show`, `set`, `edit`, `reset`, `check`, `text` |
| `skill` | `list`, `show`, `attach`, `detach`, `clear`, `reset`, `resolve` |
| `adviser` | `list`, `show`, `providers`, `bind`, `ask`, `accept`, `reject`, `defer`, `status`, `check` |
| `check` | `list`, `add`, `run`, `stats` |
| [Daily task harness](task-harness.md) | `task open`, `next`, `claim`, `release`, `record`, `test`, `extend`, `commit` |
| `gate` | `init`, `status`, `uninstall` |
| `usage` | `show`, `record` |
| `config` | `show`, `get`, `set`, `reset`, `edit` |
| `completion` | `bash`, `zsh`, `fish`, `powershell` |

`version` reports the build. `context`, `hook`, and `guard` remain available for host and Git
integration. Existing spellings such as `roles`, `skills`, `change`, `tokens`, `consult`, `run`,
and `init` keep working quietly, including their existing bare-command behavior.
`kitsu help --all` lists compatibility aliases and advanced integrations.

Use `task start --resume <task> --host codex|claude` to reopen a daily task and recover its next action. An explicit `--until` at launch pins its stopping point.

## Help works before setup

Every group, leaf, and supported alias accepts `-h` and `--help`. These forms are equivalent:

```sh
kitsu help skill attach
kitsu skill help attach
kitsu skill attach --help
kitsu skill attach -h
```

Help explains operands, options, defaults, and examples. It works offline, outside Git, with
invalid local configuration, and before required arguments are supplied. It opens no editor,
writes no files, and invokes no check, skill, provider, or host. A preferred group with no
subcommand also prints help and exits 0.

A usage error exits 2 and gives the leaf's concise usage and exact help command. A misspelled
command can suggest a correction; Kitsu never executes that suggestion. For `check add`, `--`
separates Kitsu options from the program and its arguments. A later `--help` belongs to that
program.
The generated [command reference](cli-reference.md) mirrors metadata-derived help for reading
all operands, options, defaults, and examples without running a command.

## Opt into compact context

For a daily task, `kitsu task next <task> --brief` returns the current action. For a managed OpenSpec change, `kitsu task show <task> --compact` returns a bounded coordinator snapshot.
Use `kitsu task show <task> --since <snapshot-id>` after a state change to receive
only changed sections; it implies compact JSON. `--max-bytes` defaults to 16384.
Ordinary `task show` and `task show --json` keep their existing output. See the
[snapshot and delta how-to](coordinator-state.md) for reference expansion, limits
and recovery when a baseline expires.

```sh
kitsu task start "Fix login retries" --host codex --compact
kitsu context read README.md --start 1 --end 30
kitsu context search 'retry' internal
kitsu context collect --request context.json --json
kitsu task prepare fix-login --role developer --stage code --round 1 --host codex --workflow small --assignment /absolute/path/assignment.txt
kitsu task finish <dispatch-id> --expect-checks pass
kitsu check run --compact
kitsu context show <artifact-id> --item <item-id> --json
```

The [compact-context guide](compact-context.md) supplies typed read/search examples, the
advanced version 1 request schema, exact byte limits, cursor expansion and freshness rules.
`task prepare` saves the full original brief and tracks a write-check interval; the host
launches the worker. `task finish` validates the saved baseline and existing check evidence.
Use `task brief --save --assignment <file|->` for a saved brief without a managed interval.
Add `--dispatch` to `task prepare` for a generated native launch description.
After the worker stops, `task return <dispatch-id> --request <file|->` can retain
its bounded claims before the separate finish step. See
[coordinator dispatch and returns](coordinator-overhead.md) for the schema,
authority limits and examples. Neither command starts a model.
Every command supports `--help`. `--json`
also selects compact check output. Defaults stay unchanged; bare `kitsu context` and
`kitsu context --host codex|claude` retain their session-hook protocol. They are the exception
to bare groups showing help. Context commands, brief, prepare and finish always emit JSON.

## Select your host once

An explicit `--host` always wins. Otherwise preferred commands use the first configured value:

1. A nonempty `KITSU_HOST` environment variable.
2. This worktree's private `<git-dir>/kitsu-cli.toml`.
3. Your personal `os.UserConfigDir()/kitsu/config.toml`.

Only `codex` and `claude` are valid. `config show` reports effective values and their origins;
`config get host` reads the host value. These preferences select the CLI host, while role model
and skill bindings keep their existing configuration files and scope rules.

```sh
kitsu config set host codex                 # private to this worktree
kitsu config show --json
kitsu config get host
kitsu config set host claude --global       # personal default
kitsu role list                            # uses the effective preference
kitsu role list --host claude               # one-call override
kitsu config reset host                    # remove the worktree override
kitsu config reset host --global            # remove the personal default
```

The local file uses the worktree's Git directory, so linked worktrees can choose different
hosts. Neither preference file is a shared task record. `config` currently supports only the
`host` key. Local writes require a Git worktree; `--global` uses the personal file. Legacy
commands keep their existing explicit-host behavior.
Removing the supporting gate keeps the CLI host preference.

## Check setup and start a task

After [installing the CLI](release-install.md) and [adding the host plugin](release-install.md#register-the-matching-marketplace),
inspect local setup from the directory where you want to work:

```sh
kitsu agent check --host codex
kitsu agent check --host claude --json
kitsu task start "Add an order CSV export" --host codex
```

If the plugin is missing and you have the matching preview bundle, run `kitsu agent setup --host codex --archive-dir .` from its extracted directory. Substitute `claude` for Claude Code. Rerun `agent check` in your working repository after setup. [Preview installer and upgrade instructions](release-install.md#install-a-ci-artifact-before-publication)

`agent check` reads local PATH, the selected host CLI, the installed and enabled Kitsu plugin,
its task skill, and its hook bundle and known disable settings. Each check gives evidence and
next steps. It runs no host, hook, authentication flow, or model and changes no settings.
It needs neither `kitsu.toml` nor the supporting Git gate.

| Finding | What to do |
| --- | --- |
| Local prerequisites available | Start the task; confirm skill loading and hook behavior in the host session. |
| CLI missing from PATH | Follow the installer's PATH step and restart the terminal or desktop app. |
| Plugin, task skill, or hooks missing or disabled | Install or enable Kitsu in that host and follow the diagnostic's configuration step. |
| Hook trust or execution unknown | Inspect the host's hooks and session output; configuration alone does not prove execution. |

On Codex, open `/hooks` to review and trust the exact current Kitsu hooks. Updates can require
another review. This is a manual host action; Kitsu neither accepts trust nor guesses it from
configuration. See the official [hook guide](https://learn.chatgpt.com/docs/hooks).
On Claude Code, inspect Kitsu in `/plugin`; after installing from a shell, start a new session
or use `/reload-plugins`. See the official [plugin guide](https://code.claude.com/docs/en/discover-plugins).

Default `task start` explicitly launches `codex` or `claude` in the current directory with a
prompt to use Kitsu's task skill. All three standard streams must be terminals. Demonstrated
local setup failures prevent launch; unknown trust or runtime behavior remains a stated caveat.
The host controls authentication, model access, permissions, and delegation. On Claude Code the
daily workflow is `/kitsu:task`; the managed workflow is `/kitsu-team:managed` and its workers are `kitsu-team:<role>`. On Codex the host loads Kitsu's `task`
or `managed` skill and the role texts. In a Git checkout, daily startup saves a private intake snapshot before opening the host. Preview and legacy managed compact handoffs retain their existing behavior.

To prepare a handoff for an existing desktop session, or inspect it before installing the host CLI:

```sh
kitsu task start "Add an order CSV export" --host codex --print
kitsu task start "Add an order CSV export" --host claude --json
```

These mutually exclusive modes return the prompt and local diagnostics without launching
anything or creating a task folder, receipt, or usage record. Paste the printed prompt into
your selected host. JSON returns `host`, `ready`, `host_executable`, and `checks`,
plus `prompt` and `argv` for task preparation. `ready` concerns local prerequisites, not verified
trust, authentication, model access, or successful workflow execution.
Checks use `pass` for established local facts, `fail` for blocking setup problems, and `warn`
for caveats such as unknown runtime or optional gate setup. An inconclusive required installation
inventory blocks readiness and explains what could not be established; it does not prove absence.

`agent check` exits 0 when no blocking local problem is found and 5 when local setup is blocked.
Handoff-only modes exit 0 after successful inspection even with `ready: false`; inspect their
diagnostics. Invalid usage, conflicting output modes, or a default launch without terminals
exits 2. A launched host's exit status is preserved. A read or process-start failure exits 1.
Launch requires a native host executable; for a `.cmd` or `.bat` wrapper, use a native installation
or prepare the handoff with `--print`.
Host selection follows the [same preference order](#select-your-host-once) as other preferred commands.

## Inspect a task, role, or skill

```sh
kitsu task list
kitsu task next add-order-export --brief
kitsu task show add-order-export --json
kitsu role show developer
kitsu skill list
kitsu skill show project-review --json
kitsu check list
kitsu usage show --task add-order-export
```

For a reserved feature review, use the returned `review_input` command.
Its short form is `kitsu task show ID --review-ticket TICKET --review-brief`.
The same ticket opens earlier work notes with `kitsu task show ID --review-ticket TICKET`.

Task listing includes daily tasks, full change folders and saved small-task histories. Use `kitsu task check <change>` to check an OpenSpec change folder. A small managed task has no
complete stage record: Kitsu identifies saved history and leaves the workflow stage unknown.
It does not turn a receipt into proof that tests, review, or approval happened.

Role inspection shows model, reasoning, selected skills, adviser access, and write limits.
Skill inspection shows original metadata and availability, including entries that cannot be
attached. Discovery remains local and read-only; resolving a task selection can write an
immutable receipt and still leaves loading unverified. See [skills](skills.md).

## Change one setting explicitly

Create a shared role file with `kitsu role init`. This uses the saved install host. Set a role's host, model and reasoning together with `kitsu role set developer --host claude --model sonnet --reasoning high`, or update individual fields in an existing version 2 file. [Configure roles from either plugin or the terminal](configured-workflow.md#configure-the-roles)

Use `set` in scripts to change only supplied fields:

```sh
kitsu role set developer --model gpt-6.1-sol --reasoning max
kitsu role set reviewer --reasoning high
kitsu role reset developer
```

Repository role reset removes that host/role's model, effort, and skill overrides. Personal
role settings permit only skill bindings; the coordinator's model and effort remain host
session settings. See [roles](roles.md#changing-a-model).

Skill commands make adding, removing, clearing, and inheriting distinct. These examples assume
the skill exists in your catalog:

```sh
kitsu skill attach codex:repo:.agents/skills/project-review --role reviewer
kitsu skill detach codex:repo:.agents/skills/project-review --role reviewer
kitsu skill clear --role reviewer
kitsu skill reset --role reviewer
kitsu skill attach codex:user:project-review --role reviewer --global
```

Attach adds to the selected list. Detach can remove a saved reference after its original source
has disappeared. Clear saves `[]`, suppressing inherited personal skills. Skill reset removes
only the skill override, restoring inheritance and preserving model and reasoning overrides.
These commands preserve unrelated roles, hosts, and configuration.

## Open an editor when you want one

Only explicit editor commands open configuration forms:

```sh
kitsu role edit developer
kitsu role edit reviewer --global
kitsu config edit
kitsu config edit --global --accessible
```

Role editing suggests models from host defaults and configured
choices, accepts an exact custom model id, and supports multiple selected skills. It does not
probe model availability or contact a provider. A host selected inside role editing applies to
that edit and is not saved as a CLI preference.

Review the preview and choose Save to commit the changes. Save revalidates selected skills and
aborts if the edited configuration changed concurrently. Cancel leaves configuration and lock
files untouched. `F1` shows contextual field help while preserving the draft; `--accessible`
uses descriptive accessible prompts. Standard input and standard error must both be terminals.
Editor commands reject `--json`; noninteractive commands never fall back to an editor.
Any nonempty `NO_COLOR` keeps both normal and accessible editors colorless, including when
`CLICOLOR_FORCE` is set.
Explicit Cancel exits 0. Ctrl+C or a canceled editor context exits 130 without saving, so a
following shell command joined with `&&` does not continue after an interrupt.

## Ask and address an adviser

Advisers remain experimental and off by default. After explicitly configuring definitions and
repository role access, bind the task, ask a question, and record a disposition:

```sh
kitsu adviser providers
kitsu adviser list
kitsu adviser show second-opinion
kitsu adviser bind add-order-export --workflow full
kitsu adviser ask second-opinion --task add-order-export --role architect \
  --stage design --request request.json
kitsu adviser accept <consultation-id> --task add-order-export \
  --reason "Use the proposed failure cases."
kitsu adviser status add-order-export
kitsu adviser check add-order-export
```

`reject` and `defer` take the same id, task, and reason arguments; decisions can also name
`--choice`. Use `adviser ask` for a manual consultation. A checkout can separately
opt in to one Jev review at a new feature's intake. Listing providers, inspecting
a record, asking for help, and completion are local operations. Review and human
approval still apply. [Adviser setup and usage](advisers.md)

## Approve repository checks

In a repository that declares checks, run `kitsu check trust` in your own terminal before the Codex or Claude Code plugin runs them. Review the commands and type `yes` to approve the exact `kitsu.toml` declaration. Linked worktrees share this approval for matching declarations. Review a changed declaration before approving it again.

Use `kitsu check trust --verify-order before-review` to approve running eligible
feature checks before the first required review. Use `--verify-order review-first`
to revoke this checkout's ordering grant. Ordinary approval applies separately. [Eligibility and consent](task-harness.md#verify-before-review)

For CI repair, `kitsu check trust --ci ID -- PROGRAM ARGS` approves a named local reproduction under your user configuration. Select it with `kitsu task ci --check ID`. Any declared repository checks still use their declaration approval. [Repair a failed PR check](task-harness.md#repair-a-failed-pr-check)

`kitsu check list` and `kitsu check plan` inspect checks without approval. Direct terminal and CI calls to `kitsu check run` execute the declared commands as before. See [the plugin hook setup](hooks.md#hooks-of-the-agent-plugin).

## Add the Git gate

```sh
kitsu gate init
kitsu check add test -- <your existing test command>
kitsu check run
kitsu gate status
kitsu check stats --since 7d
```

These commands retain the existing [evidence](evidence.md) and [hook](hooks.md) behavior, with
the existing guard decisions for policy and gate changes. `gate uninstall` removes Kitsu hooks
and retains records. After a complete detach, `--purge` deletes the shared check and token store. Task history and configuration remain.

## Enable shell completion

Print a script for the shell you use:

```sh
kitsu completion bash
kitsu completion zsh
kitsu completion fish
kitsu completion powershell
```

Load the generated script through your shell's normal completion setup. For a Bash session,
`source <(kitsu completion bash)` enables it without saving a file. Completion uses the same
command metadata as help, plus read-only local names where supported. It executes no skill,
provider, check, host, or model and needs no network connection.
Directory suggestions end with the native path separator. Home-relative prefixes such as `~/`
and `~/projects/` complete names inside those directories.

For automation, use explicit leaf commands and `--json` where help lists it. JSON output is
machine-readable and opens no UI. [Operations](operations.md) documents configuration, exit
statuses, storage, and the retained command spellings.
