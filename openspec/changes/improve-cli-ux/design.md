# Design: improve-cli-ux

The preferred command tree is an interface over existing behavior. Metadata describes a command
once, and explicit actions perform reads, writes, or terminal interaction after dispatch.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| Command metadata | Describe preferred paths, operands, options, examples, host needs, and guard classifications in `ux_types.go` and `ux_registry.go` | none | yes | Existing `commands()` supplies legacy handlers; retain it as the compatibility surface rather than replacing service behavior |
| Preferred dispatch and help | Resolve root/group/leaf/alias help before setup; execute exact actions and print concise errors | Command metadata, Existing handlers | yes | Existing flag parsing and error types; avoid a second independent help/completion tree |
| Inspection and skill actions | Adapt positional grammar and inspect existing configuration/history; edit only the requested skill scope | Existing handlers, Command metadata | yes | Existing role, skill, adviser, change, and evidence handlers and their validation |
| Private preferences and editors | Resolve host origins, save private host settings, and run explicit Huh v2 forms with guarded preview/save | Existing handlers | yes | Existing role editors, source resolution, atomic writes and locks; CLI preferences stay separate from shared role policy |
| Shell completion | Emit four scripts and readonly suggestions from metadata and supported local names | Command metadata, Existing handlers | yes | Existing local catalogs/configuration; no provider, host, or model execution |
| Existing handlers | Preserve task receipts, role limits, model settings, adviser opt-in, gate evidence, and usage semantics | none | no | Existing `internal/roles`, `internal/skills`, `internal/advisers`, `internal/change`, and check/gate code |
| Role references and diagrams | Lead with preferred commands while preserving workflow order and permissions | Command metadata | no | Existing generic role sources, generated host files, archify JSON and canonical SVG exports |

## Dispatch, help, and compatibility

Root and preferred groups print help and exit 0 without arguments. `-h`, `--help`, `help PATH`,
and `GROUP help SUBPATH` reach help at every supported path and alias before configuration,
repository, operand, or TTY checks. Program arguments after `check add --` remain untouched.
Unknown paths and bad usage exit 2 with a short error, leaf usage, and exact help guidance.
Suggestions are text only. Legacy commands retain their existing handlers, output contracts,
bare-command behavior, and no deprecation chatter.
Alias help retains its own argument rules, including explicit legacy hosts, optional task
inventory, and adviser `--task` operands, while identifying the preferred replacement.

The registry supplies dispatch, help, examples, generated command documentation, completion flags/values, and command guard
classification. Preferred gate/policy mutations receive the same guard decisions as their
aliases. No grouped route can widen a role's write limit or waive a workflow approval.

## Preferences and explicit editors

Preferred commands resolve host from an explicit flag, then nonempty `KITSU_HOST`, then private
worktree `GitDir/kitsu-cli.toml`, then `os.UserConfigDir()/kitsu/config.toml`. Unset remains unset;
invalid choices fail clearly. Read output includes origins. Version 1 preference files hold only
the host, and local writes are private to one worktree; `--global` selects the personal file.
These preferences do not migrate or alter existing personal role skill bindings.
The local file stays outside the gate's evidence directory so gate removal preserves the CLI
preference in primary and linked worktrees. Direct edits retain policy guard protection.

Only `role edit` and `config edit` prompt. Both require stdin and stderr TTYs, place the form on
stderr, and return a plain summary on stdout. Huh v2 model suggestions include built-in and
configured choices plus an exact custom id; skills use multiple selection. Global role and
coordinator scope restrictions remain enforced. Selecting a host inside role editing changes
that edit without saving a preference.

Forms expose F1 contextual help without discarding the draft and an accessible descriptive mode.
Any nonempty `NO_COLOR` suppresses colors in both modes even when force-color settings are present.
Preview precedes Save. Save revalidates references and refuses a changed source configuration;
explicit Cancel exits 0 without creating configuration or lock files. Ctrl+C/context cancellation
in new editors exits 130 without saving. Editors refuse `--json`, while noninteractive
commands never open them as a fallback.

## Task inspection and skill mutations

Task listing combines full folders and saved small histories without inferring a stage from a
small receipt. Show actions expose current role settings and limits, skill metadata including
unavailable entries, adviser configuration/bindings, and declared checks using existing readers.

Skill attach adds a validated source to the effective list. Detach can remove an exact saved
reference after its source disappears. Clear writes explicit `[]`; reset removes only the skill
override, restoring inheritance without changing model or reasoning. General role reset keeps
its existing whole-role behavior. Repository and global skill scope rules remain intact.

## Completion and diagrams

`completion bash|zsh|fish|powershell` prints sourceable shell code. A hidden callback supplies
metadata and readonly local suggestions using explicit word boundaries. It performs no checks,
skill execution, provider requests, host/model calls, or configuration mutation. Completion
must preserve quoting, separators, and values rather than evaluating user text.

All four affected diagrams keep their topology and use preferred command labels. Edit JSON,
run archify `finalize` at `showcase`, export through the actual SVG Auto action, and inspect the
exports in both themes. Commit-ready sources and SVGs move together; SVG is never hand-edited.

## Rules

| Rule | Checked by |
| --- | --- |
| Every preferred and alias help ingress works offline before configuration, Git, operands, and TTY checks | test |
| Root/group no-argument help succeeds, legacy bare behavior remains, and typo suggestions execute nothing | test |
| Dispatch, help, generated command documentation, and completion share metadata and preserve guard classification | test |
| Host precedence and file origins are deterministic, private, and worktree-isolated | test |
| Explicit editors preview, cancel without writes, revalidate on save, and keep scope restrictions | test |
| Skill attach/detach/clear/reset preserve model fields and distinct inheritance behavior | test |
| Inspection reports existing facts and keeps small-task stages unknown | test |
| Completion scripts and callbacks preserve word boundaries and perform only readonly local work | test |
| Generic role text, generated files, and existing prompt budgets remain checked | test |
| Archify JSON/SVG pairs have matching command labels and remain embedded in their documents | test |
| Exported diagrams preserve readable command labels and topology in both themes | review |
| Existing role authority, experimental opt-in, independent review, and human approvals remain intact | review |

CLI presentation code has a separate 3,500-line budget; existing core, workflow, adviser, and
skill budgets stay intact. Existing prompt limits, including the 12,000-character coordinator
cap, remain in force. A
generated script, passing help test, or instruction check proves no live host/model compliance.
