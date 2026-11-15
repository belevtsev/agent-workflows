# Design: add-configurable-advisers

The coordinator remains the owner of workflow decisions. Adviser execution is a bounded subprocess
operation beside the existing roles, review ledger, and gate. Provider APIs and a general agent
runtime are outside Kitsu.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `internal/advisers` | Parse definitions and policy, render requests, execute a bounded command, preserve records, and check adviser receipts | `internal/roles`, `internal/change`, `internal/proc`, `internal/fslock` | yes | `internal/decl` provides strict TOML conventions; `internal/runner` is check-specific, so reuse `internal/proc` directly; `internal/store` holds local evidence and cannot hold shared task history |
| `internal/roles` | Keep specialist models and instructions, generate host files, and retain write limits | none | no | Existing role texts, model configuration, and host generation |
| `internal/change` | Derive artifact content ids and existing review and approval state | none | no | Existing `Content` and stage computation; no dependency on advisers to avoid an import cycle |
| `internal/cli` | Expose commands, coordinate task snapshots and checkpoint stage reporting, and print role selection and unmeasured usage | `internal/advisers`, `internal/roles`, `internal/change`, `internal/tokens`, `internal/gitx` | no | Existing flag parsing, JSON output, snapshot, and change commands |
| `internal/tokens` | Read and sum measured host usage | none | no | Existing reports remain measured host reports; the CLI lists adviser calls separately |
| `internal/proc` | Bound time, output, cancellation, and process cleanup | none | no | Existing process-group and Windows job-object behavior |
| `internal/fslock` | Serialize mutations to one task's adviser history | none | no | Existing cross-platform locks rather than persistent task-state locks |
| `internal/gitx` | Snapshot repository artifacts without changing the real index | none | no | Existing snapshots and differing-path semantics |

## Configuration and requests

The personal file holds only definitions. Repository definitions replace whole personal
definitions; repository access and checkpoints own policy. Per-role access replaces `default`.
The merged configuration is validated strictly, including checkpoint access and references.
Missing files produce empty definitions and no access.

Each definition has `model`, a `command` argument array with exactly one literal `{model}`, and an
optional duration `timeout` from 1s to 1h, default 5m. `{answer_file}` is optional. Commands run
directly with the repository as working directory, prompt on stdin, and the existing environment
for CLI authentication. Kitsu's recursion marker prevents nested adviser execution. Shared
records contain no command arguments, environment variables, API keys, or raw diagnostics.

The version 1 request has `kind = advice|decision`, `question`, optional named `choices`, and an
explicit `context` file list. Decisions need two or more distinct choices. Unknown and duplicate
JSON keys are refused. Context reads remain rooted in the repository; the request, rendered
prompt, and answer each have a 1 MiB bound. The command runs once; a requested answer file is the
sole answer source. Actual model and token usage stay unknown.

## Binding and history

Full records use `openspec/changes/<task>/advisers/`, small records use
`openspec/changes/.advice/<task>/`. A binding snapshots nonsecret model names, definition
fingerprints, role access, and checkpoints. The active pointer names an immutable revision.
Current local configuration is needed only for a new binding, explicit refresh, or execution;
execution refuses a definition that differs from its binding. Offline readers use saved policy.

Each attempt reserves an id and saves the rendered prompt before executing. Its outcome and answer
are preserved after execution, including failures. Dispositions append as separate records; a
nonempty rationale is mandatory; an optional choice must name a supplied option. Refresh
adds a revision without rewriting prior records. Promotion requires `--workflow full --refresh`,
an absent full adviser destination, and a move of the whole small history. Two histories fail.

## Checkpoints and workflow

Checkpoint stages are `spec`, `design`, and `review`. A receipt matches its active binding,
checkpoint id, requester, adviser model and fingerprint, stage, and current artifact content id.
It requires success plus an addressed disposition for every adviser named by the checkpoint.
`advisers check` compares current artifacts; completed status may recognize the committed task
baseline. `change check` validates structure without requiring missing advice for future stages;
`change status` holds the corresponding stage when its required receipt is missing or stale.

Every specialist returns requests to the coordinator, which checks its writes before creating
adviser records and resumes it with the answer. Required advice precedes ending the stage.
Reviewers form their own assessment before asking and receive no author's consultations as
guidance. Advice never writes an approval or changes the existing severity and review rules.

## Model routing

| Role | Codex model | Effort | Claude Code model | Effort |
| --- | --- | --- | --- | --- |
| `lookup` | `gpt-6-luna` | `medium` | `haiku` | `medium` |
| `spec-writer` | `gpt-6.1-sol` | `high` | `opus` | `high` |
| `architect` | `gpt-6.1-sol` | `max` | `opus` | `max` |
| `quality-engineer` | `gpt-6.1-sol` | `high` | `sonnet` | `high` |
| `developer` | `gpt-6.1-terra` | `high` | `sonnet` | `high` |
| `reviewer` | `gpt-6.1-sol` | `max` | `opus` | `max` |
| `git` | `gpt-6-luna` | `low` | `haiku` | `low` |

The coordinator is the host session: recommended Sol/max or Opus/xhigh, selected in the host.
Kitsu prints host session rather than inventing a configured model. `kitsu roles --host` prints
Role, Model, Reasoning, and Advisers; `--task` uses the saved binding's access offline. Repository
model overrides still use `kitsu-roles.toml`. Recommendations are job-based, not benchmarked;
complex debugging, concurrency, and merge conflicts may warrant explicit stronger settings.
Kitsu never escalates automatically, and the developer's explicit model selection takes priority.

## Rules

| Rule | Checked by |
| --- | --- |
| Configuration and JSON requests are strict, bounded, and validated before model execution | test |
| Only one configured CLI is attempted; cancellation, timeout, output limits, and cleanup retain the attempt | test |
| Adviser commands reuse `internal/proc`, with no new network dependency | test |
| Bound policy and shared history can be checked without personal configuration or installed provider CLIs | test |
| Refresh and promotion preserve earlier history and never create competing records | test |
| Matching current successful addressed receipts are required at their own stages | test |
| Generated host files match source texts, and text budgets remain unchanged | test |
| Role defaults match the model route and allow repository overrides | test |
| Reviewer independence, human approvals, and finding severity rules remain explicit instructions | review |
| Adviser usage is unknown and separate from measured host totals | test |

Production Go budgets are 6,250 core lines, 4,000 workflow lines, and 3,500 adviser lines; prompt
budgets stay 8,000 characters per role, 12,000 for coordinator, and 60,000 together. The repository's
existing checks enforce implementation size and generated files. No claim is made that a model
follows its instructions solely because those checks pass.
