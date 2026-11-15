# Find and configure a Kitsu command

Start with `kitsu` or `kitsu help`. Both print help and exit successfully. Commands are grouped
by the thing you work with:

| Group | Commands |
| --- | --- |
| `task` | `list`, `show`, `check` |
| `role` | `list`, `show`, `set`, `edit`, `reset`, `check`, `text` |
| `skill` | `list`, `show`, `attach`, `detach`, `clear`, `reset`, `resolve` |
| `adviser` | `list`, `show`, `providers`, `bind`, `ask`, `accept`, `reject`, `defer`, `status`, `check` |
| `check` | `list`, `add`, `run`, `stats` |
| `gate` | `init`, `status`, `uninstall` |
| `usage` | `show`, `record` |
| `config` | `show`, `get`, `set`, `reset`, `edit` |
| `completion` | `bash`, `zsh`, `fish`, `powershell` |

`version` reports the build. `context`, `hook`, and `guard` remain available for host and Git
integration. Existing spellings such as `roles`, `skills`, `change`, `tokens`, `consult`, `run`,
and `init` keep working quietly, including their existing bare-command behavior.
`kitsu help --all` lists compatibility aliases and advanced integrations.

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

## Inspect a task, role, or skill

```sh
kitsu task list
kitsu task show add-order-export --json
kitsu task check add-order-export
kitsu role show developer
kitsu skill list
kitsu skill show project-review --json
kitsu check list
kitsu usage show --task add-order-export
```

Task listing includes full change folders and saved small-task histories. A small task has no
complete stage record: Kitsu identifies saved history and leaves the workflow stage unknown.
It does not turn a receipt into proof that tests, review, or approval happened.

Role inspection shows model, reasoning, selected skills, adviser access, and write limits.
Skill inspection shows original metadata and availability, including entries that cannot be
attached. Discovery remains local and read-only; resolving a task selection can write an
immutable receipt and still leaves loading unverified. See [skills](skills.md).

## Change one setting explicitly

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

Only explicit editor commands prompt:

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
`--choice`. Only an explicit `adviser ask` executes a consultation. Listing providers,
inspection, help, and completion make no provider request. Advice replaces no review or human
approval. [Advisers](advisers.md) covers opt-in, request JSON, checkpoints, and usage limits.

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
and local records, including host token records; it keeps configuration, private CLI preferences,
and shared OpenSpec history.

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
