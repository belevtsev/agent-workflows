# Advisers

An adviser answers a bounded question or compares named choices. You choose its model and an
installed command; Kitsu sends the prompt through that command's standard input and records the
answer with the coordinator's decision. The command uses its existing CLI authentication.
Kitsu stores no API keys, makes no provider API calls, and supplies no model fallback or automatic
retry. An adviser can be from the same provider as the role or from another provider.

The coordinator brokers every consultation. A role asks for advice, returns control, and resumes
with the recorded answer. Advice leaves the role's job and write limits in place. A reviewer
first forms its own assessment and receives none of the author's consultations as review guidance.
Advice supplies neither an independent review nor the developer's approval.

## Configure definitions and access

Personal definitions live at `os.UserConfigDir()/kitsu/advisers.toml`. That means
`$XDG_CONFIG_HOME/kitsu/advisers.toml` (normally `~/.config/kitsu/advisers.toml`) on Linux,
`~/Library/Application Support/kitsu/advisers.toml` on macOS, and
`%AppData%\kitsu\advisers.toml` on Windows. The personal file accepts only `version` and
`[advisers.<name>]` definitions.

The repository's optional `kitsu-advisers.toml` defines advisers and owns `[access]` and
`[[checkpoints]]`. A repository definition replaces the whole personal definition with the same
name; fields are never merged. With neither file present, advisers are disabled.

For example, a personal file can define two already authenticated CLIs:

```toml
version = 1

[advisers.claude]
model = "opus"
command = [
  "claude", "--print", "--model", "{model}", "--effort", "high",
  "--input-format", "text", "--output-format", "text", "--tools", "",
  "--safe-mode", "--permission-prompts", "none", "--no-session-persistence"
]
timeout = "5m"

[advisers.codex]
model = "gpt-6.1-sol"
command = [
  "codex", "exec", "--model", "{model}",
  "-c", 'model_reasoning_effort="high"',
  "--sandbox", "read-only", "--ephemeral", "--color", "never",
  "--output-last-message", "{answer_file}", "-"
]
```

These command shapes were checked against Claude Code 2.1.325 and Codex CLI 0.169.0 help; no live
adviser run is claimed. Select a model your CLI account can use. On Windows, use an executable
directly: `.cmd` and `.bat` launchers are refused.

Adviser reasoning is selected through command arguments. It is independent of the requesting
role's effort, and Kitsu does not translate effort settings between providers:

| Adviser CLI | Model field | Reasoning arguments in `command` |
| --- | --- | --- |
| Claude Code | `model = "opus"` | `"--effort", "high"` |
| Codex | `model = "gpt-6.1-sol"` | `"-c", 'model_reasoning_effort="high"'` |

Replace `high` with a supported level to choose another effort. The Codex configuration override
is one argument after `-c`; its double quotes are part of that argument. The TOML single quotes
preserve them without a shell. There is no Kitsu adviser `effort` key. Changing reasoning arguments
changes the definition fingerprint, so an existing task needs an explicit binding refresh.

The repository can then set its policy without repeating those commands:

```toml
version = 1

[access]
default = []
coordinator = ["claude", "codex"]
architect = ["claude"]
reviewer = ["codex"]

[[checkpoints]]
id = "design-alternatives"
stage = "design"
role = "architect"
advisers = ["claude"]

[[checkpoints]]
id = "code-review"
stage = "review"
role = "reviewer"
advisers = ["codex"]
```

Each role's explicit access list replaces `default`, including an empty list. A role without an
entry uses `default`; without either, access is denied. Valid roles are the coordinator and the
seven [specialist roles](roles.md). Each checkpoint names an allowed requester and one or more
defined advisers. Its stage is `spec`, `design`, or `review`.
Small workflows apply only review checkpoints because they have no specification or design stage.

Configuration is strict version 1 TOML: unknown fields, unknown references, duplicates, empty
models, and invalid timeouts are errors. Adviser names and checkpoint ids use 1 to 40 lowercase
letters, digits, underscores, or hyphens, starting with a letter or digit. `command` is an argument
array, executed directly without shell expansion. Exactly one argument must be the literal
`{model}`; an optional argument may be `{answer_file}`. Each placeholder occupies its whole
argument after the program. The timeout defaults to five minutes and must be from one second to
one hour.

## See the selection in a table

```sh
kitsu roles --host codex
kitsu roles --host claude
kitsu advisers list --role architect
```

The compact roles table shows **Role**, **Model**, **Reasoning**, and **Advisers** for the selected
host. Its coordinator row identifies the host session: Kitsu has no configured coordinator model
or effort. Specialist model and effort settings come from `kitsu-roles.toml`; adviser access comes
from `kitsu-advisers.toml`, or from the saved binding when `--task` is given. See the
[default selection table](roles.md#the-roles-at-a-glance) and [role configuration](roles.md#kitsu-rolestoml).
Without `--host`, `kitsu roles` shows both hosts. There is no interactive model picker.

## Bind a task

```sh
kitsu advisers list --json
kitsu advisers bind --task add-order-csv-export --workflow full
kitsu roles --host claude --task add-order-csv-export
kitsu advisers list --task add-order-csv-export --role architect --json
```

Binding saves the task's workflow, adviser model names and definition fingerprints, access lists,
and checkpoints. It copies neither command arguments nor environment variables. The shared
binding lets another developer or offline CI inspect the record without your personal file or
provider CLI.

An existing binding keeps its policy when current configuration changes. To adopt those changes,
run `kitsu advisers bind --task <task> --workflow full|small --refresh`. Refresh creates a new
revision and preserves the history. A consultation can run only when the locally available
adviser's model and definition fingerprint match the active binding. A changed model or command
cannot silently execute an older selection.

Full tasks keep records under `openspec/changes/<task>/advisers/`; small tasks use
`openspec/changes/.advice/<task>/`. To promote a small task, create its full change folder and run
`kitsu advisers bind --task <task> --workflow full --refresh`. This moves the whole adviser history
before refreshing the binding. The destination adviser folder must be absent. Two existing
histories for one task are an error.

## Ask a question or compare choices

The request is one UTF-8 JSON document with `version`, `kind`, `question`, and optional `context`.
An `advice` request has no choices. A `decision` request has at least two distinct choices, each
with a nonempty `id` and `text`:

This example assumes the full task's `proposal.md` and `design.md` already exist; binding creates
neither file. For a task without those files, start with the
[context-free advice example](../README.md#ask-an-adviser).

```json
{
  "version": 1,
  "kind": "decision",
  "question": "Which approach meets the requirements with less operational work?",
  "choices": [
    {"id": "reuse", "text": "Extend the existing worker."},
    {"id": "separate", "text": "Add a separate worker."}
  ],
  "context": [
    "openspec/changes/add-order-csv-export/proposal.md",
    "openspec/changes/add-order-csv-export/design.md"
  ]
}
```

Save this as `request.json` outside the worktree, then let the coordinator run:

```sh
kitsu consult --task add-order-csv-export --role architect --stage design \
  --adviser claude --checkpoint design-alternatives --request /tmp/request.json
kitsu advisers resolve --task add-order-csv-export <consultation-id> \
  --disposition accept --choice reuse --reason "Reuses the existing retry and shutdown behavior."
```

`--request -` reads the JSON from standard input. `kitsu consult --help` describes the schema.
`--role` identifies the requester, even though the coordinator executes the call. Ad hoc requests
may use any existing task stage: `lookup`, `spec`, `spec-review`, `design`, `design-review`, `tests`,
`code`, `review`, or `commit`. Only a call with the matching `--checkpoint` can satisfy that
checkpoint.

Context consists only of explicitly listed repository-relative regular files. Absolute paths,
paths outside the repository, duplicate paths, and symlinks that escape it are refused. Kitsu
renders those files into the prompt and sends no other file automatically. The request and
rendered prompt must each fit in 1 MiB. The answer must be nonempty UTF-8 and fit in 1 MiB.
With `{answer_file}`, the CLI must create the supplied new file; stdout is not a fallback.

The coordinator checks the requesting role's writes with `kitsu roles check` before the call so
adviser records do not become that role's writes. It sends the answer and consultation id back
to the requester, then records `accept`, `reject`, or `defer` with a nonempty reason. `--choice` is
optional; when supplied, it must name an offered choice. Record it when choosing a named option;
a free-form answer does not imply a selection. Dispositions append to the history; the original
prompt, answer, and execution metadata stay unchanged.

The record preserves failed calls, timeouts, cancellations, empty answers, and output overflows.
They satisfy no checkpoint. Kitsu attempts no retry and selects no replacement adviser. Correct
the cause and make an explicit new call when appropriate.

## Check required advice

```sh
kitsu advisers status --task add-order-csv-export --json
kitsu advisers check --task add-order-csv-export --checkpoint design-alternatives
kitsu advisers check --task add-order-csv-export
```

A required receipt must match the active binding, checkpoint, requester, adviser identity, and
stage's current content id. It needs a successful answer and an `accept`, `reject`, or `defer`
disposition with a reason. Changed content makes the receipt stale. A checkpoint with several
advisers needs a receipt for each one. Rejecting advice satisfies the requirement to address it;
it does not erase the answer.

Kitsu reads the task's artifact ids before rendering the prompt and checks them again before
calling the provider. A detected change is refused without a provider call or pending history.
It checks again after execution; changed artifacts prevent a successful receipt.

`kitsu change status` holds a full task at the corresponding `spec`, `design`, or review stage until
its checkpoints are satisfied. `kitsu change check` validates adviser record structure on both
paths without requiring advice for future stages. With no task named it also discovers small
histories under `.advice/`; `kitsu change check <small-task>` needs no full change folder.
`kitsu change status` applies to full tasks; use `kitsu advisers status` for small-task readiness.
`kitsu advisers check` compares receipts with the current
artifacts; status can recognize a completed task from its committed baseline. Offline checks use
the shared binding and saved records.

Adviser dispositions do not waive a `blocker` or `major` review finding, replace required reviews,
or grant human approval. Apply the existing [review rules](workflow.md#review-rounds) to real
findings. Verify adviser claims against the sources and treat the answer as evidence.

## Shared history and usage

The task's adviser folder keeps binding revisions, immutable consultation records with their
exact rendered prompts and answers, execution outcomes, and appended dispositions. Commit that
folder with the task on either workflow path. Prompts and answers may contain the context you
explicitly supplied; inspect them before publishing. Command arguments, environment variables,
and raw CLI diagnostics are not copied into shared records.

The saved model is the model requested from the CLI. Kitsu cannot verify which model the provider
actually ran. Actual model identity and adviser token usage are `unknown`; the configured model
is never evidence of either. [`kitsu tokens`](tokens.md) reports consultations separately as
unmeasured usage, leaving measured host totals unchanged. No live model evaluation, comparison of
adviser quality, or claim of token savings accompanies this feature.

## Command reference

```text
kitsu advisers list [--role <role>] [--task <task>] [--json]
kitsu advisers bind --task <task> --workflow full|small [--refresh] [--json]
kitsu consult --task <task> --role <role> --stage <stage> --adviser <name> --request <file|-> [--checkpoint <id>] [--json]
kitsu advisers resolve --task <task> <id> --disposition accept|reject|defer --reason <text> [--choice <id>] [--json]
kitsu advisers status --task <task> [--checkpoint <id>] [--json]
kitsu advisers check --task <task> [--checkpoint <id>] [--json]
```

`list`, `bind`, and `status` do not execute a provider CLI. `check` changes no records. A failed
consultation is reported as a failure and retained for inspection; successful output alone never
records a disposition.
