# Design: add-role-workflow

Status: written on 14 November 2026 for an implementation that runs without
the owner. The twelve Open Questions of the proposal take their recommended
answers; the owner can change any of them afterwards.

## Context

- **Problem.** The owner wants named roles with a model each, a task that goes
  from a specification to tested code in any language, and a count of the
  tokens the workflow itself costs. Today's roles assume Go, check nothing a
  role changed, keep a task as a lock, and are 150,080 characters of text.
- **Outcome.** Three small commands and eight texts on top of the gate.
- **Base.** The branch and the commit of the first change's design. This
  change starts when the gate's commands exist, because it uses the snapshot,
  the differing paths, `cw.toml` and the record store.
- **What cannot be done alone.** Nothing here was run with a model, and
  nothing was checked on Codex. The evaluation (Success measures 1 and 7)
  needs model runs the owner pays for.

## Decision

The workflow is text for the developer's coding agent, and `cw` checks
afterwards what a program can check. Three alternatives were rejected:

- **A `cw` that calls models itself.** It would need keys, a network
  connection and an agent loop, and the gate promises to use no network.
- **Today's task store.** State outside the folder is what blocked finished
  work (H2, H9).
- **Limits enforced before a role writes.** A host can remove a tool from a
  role but cannot limit a shell to some paths, so the check runs after the
  role ends, on the paths that differ between two snapshots.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `internal/roles` | The seven roles, their models, their write limits, `cw-roles.toml`, and the texts | `internal/decl`, `internal/gitx` | yes | `internal/modelpolicy` on `workflow-v3`: it validates profiles and scopes this design does not have; the path patterns of `internal/decl` are reused |
| `internal/change` | Reads a change folder; decides its stage; checks its rules; makes the content ids | `internal/decl` | yes | `internal/skillcheck` on `workflow-v3`: it checks skill files, not change folders |
| `internal/tokens` | Reads the host's record of a session; stores counts; reports them | none | yes | `internal/usagelog` on `workflow-v3`: its two readers are the starting point; prices, budgets and history are not carried |
| `internal/cli` | The commands `cw roles`, `cw change` and `cw tokens`; gathers from git what a stage needs | `internal/roles`, `internal/change`, `internal/tokens`, `internal/gitx`, `internal/store` | no | |
| `internal/decl` | Path patterns and strict TOML decoding | none | no | |
| `internal/gitx` | Snapshot and differing paths | none | no | |
| `internal/store` | The directory `cw/` in the common git directory | none | no | |

## Interfaces

### `cw roles [--json]`

Prints the seven roles: job, what each may write, model and effort on each
host, and the size of its text. JSON: `{"roles": [{"id", "job", "writes",
"text_chars", "models": {"claude": {"model", "effort"}, "codex": {...}}}],
"coordinator_text_chars"}`.

### `cw roles text <role>`

Prints the one text of a role, or of `coordinator`. A host without role files
of its own gives this text to a new worker. An unknown name exits 2. The size
of a text is counted in characters, each Unicode code point as one.

### `cw-roles.toml`

```toml
version = 1

[paths]
tests = ["tests/**", "**/*_test.go"]

[claude.developer]
model = "sonnet"
effort = "high"
```

Hosts are `claude` and `codex`; roles are the seven ids; keys are `model` and
`effort`. Anything else is refused with the file, the line and the rule.

### `cw roles check <role> --from <tree> [--task <task>]`

Takes the snapshot, lists the paths that differ from `<tree>` with their kind
(added, changed, removed), and compares each with the role's limit. Exit 0, or
5 with the paths outside the limit, or 1 when the role needs the test paths
and `cw-roles.toml` does not name them. The test paths are looked up
before any path is judged, so exit 1 comes before exit 5. An unknown role, a
missing `--from`, or a role that needs `--task` without it, exits 2. In a clone whose hooks
git does not run, it prints the warning `cw status` prints.

### The change folder

| File | Written by | Format |
| --- | --- | --- |
| `proposal.md` | `spec-writer` | Free text. |
| `specs/<capability>/spec.md` | `spec-writer` | `## ADDED`, `## MODIFIED` or `## REMOVED Requirements`; `### Requirement: <name>`; a `Source:` line; `#### Scenario: <name>` with one `- **WHEN**` and one `- **THEN**` line. |
| `design.md` | `architect` | Free text, plus a table under `## Building blocks` with the columns Block, Responsibility, Depends on, New, Reuse considered, and a table under `## Rules` with the columns Rule and Checked by. |
| `tasks.md` | `architect`; the coordinator ticks the boxes | `- [ ] N.M <text>. Size: S\|M\|L. Depends on: <ids or none>. Check: <...>. Source: <...>.` under `## N. <group>`. |
| `tests.md` | `quality-engineer` | A table with the columns Scenario, Test file, Text. A row that is not automated holds `not automated` as its file and a reason as its text. |
| `reviews.md` | the coordinator | Three tables: under `## Reviews` the columns Stage, Round, Date, Content; under `## Findings` the columns Id, Stage, Round, Severity, Where, Finding, Disposition, Reason; under `## Approvals` the columns What, Date, Content. Stage is `spec`, `design` or `code`; What is `spec` or `design`; Round is a number from 1. |

How the files are read:

- A line inside a fenced block is not read, so an example is never taken for
  content. Line ends of any system are read alike.
- A cell may stand in backticks. `\|` is a bar inside a cell. A table is
  found by its heading, and `tests.md` by the column Scenario.
- **Specifications.** The rules apply to requirements under `ADDED` and
  `MODIFIED`. A requirement under `REMOVED` needs only its name. `SHALL` or
  `MUST` counts in the text above the first scenario.
- **Tasks.** A task may go on in indented lines. A task names a requirement
  when its text holds the name. `Depends on:` is read up to the next field.
- **Design.** New is `yes` or `no`. Depends on holds block names, or `none`.
  A dependency on a name the table does not hold is not followed. Checked by
  is `review` or the id of a check; several are joined by commas.
- **Tests.** The test file is a path from the root of the repository. A row
  that is `not automated` without a reason breaks the rule of the table.
- A file that is missing breaks no rule. `cw change status` names it as
  missing at its stage.

### `cw change status [<task>] [--json]`

With a task: the stage, what is missing, the number of scenarios that are not
automated, and the three content ids. JSON: `{"task", "stage", "missing":
[...], "not_automated", "content": {"spec", "design", "code"}}`. Without a
task: one line for each folder, and as JSON `{"changes": [...]}` with one such
object each. It exits 0 at any stage, and 1 when the named folder does not
exist. The stages are `spec`, `design`, `tests`, `code`, `review`, `commit`
and `done`.

A content id is the first 12 characters of a SHA-256:

| Id | Made from |
| --- | --- |
| `spec` | `proposal.md` and every file under `specs/`, each with its path, line ends as LF. |
| `design` | `design.md` and `tasks.md`, line ends as LF, every task mark read as `[ ]`. |
| `code` | The entries of the snapshot of the working tree, as `git ls-tree -r` prints them, for every path outside `openspec/changes/`. |

A review or an approval counts when its Content is the id of the working
tree. A review of the code also counts when `HEAD` holds the folder as the
working tree does and its Content is the `code` id of the last commit that
changed the folder: the task was committed with the code that was reviewed,
and what came later is later work. An empty id, of files that are missing,
matches nothing.

### `cw change check [<task>]`

Exits 0, or 5 with one line for each broken rule: the file, the line where
there is one, and the rule. It reads the files alone and takes no snapshot, so
a repository can declare it as a check. It does not judge whether a review is
of the current content; `cw change status` does.

### Tokens

**Notices.** On both hosts the plugin's hooks for the end of a role
(`SubagentStop`) and the end of the session (`SessionEnd`) run
`cw tokens record --host claude` or `--host codex`. The command reads the
host's notice from standard input: one JSON object. It uses four fields and
ignores the rest:

| Field | Meaning |
| --- | --- |
| `session_id` | The session. In a notice about a role it is the session that started the role. |
| `transcript_path` | The record the host keeps of the session. |
| `agent_id` | The role that ended; absent at the end of the session. |
| `agent_transcript_path` | The record the host keeps of that role; absent at the end of the session. |

For each notice `cw` reads the record of the role, when the notice names one,
and the record of the session, and writes one file for each:
`cw/tokens/<session>/agent-<agent id>.json` and
`cw/tokens/<session>/session.json`, in the common git directory. So the
session's own tokens are current after every role, not only at the end. The
same notice given again writes the same files. The command prints nothing on
standard output, stores nothing where the root holds no `cw.toml` or outside a
repository, and exits 0 in both cases.

**A stored file** holds `version`, `host`, `session`, `agent`, `record` (the
path of the host's file), `task`, `stage` and `round` from the label (absent
without one), `known`, `why` (why the counts are unknown), and `models`: for
each model name the record gives, the four counts `input`, `cache_write`,
`cache_read` and `output`. It holds no text of a brief or an answer.

**A record of Claude Code** has one JSON object in each line:

- A line with `"type": "user"` holds the message in `message.content`: a
  string, or a list of parts of which one has `text`. The first such line of a
  role's record is its brief, and the first line of the brief is the label.
- A line with `"type": "assistant"` holds `message.id`, `message.model` and
  `message.usage` with `input_tokens`, `cache_creation_input_tokens`,
  `cache_read_input_tokens` and `output_tokens`. Lines with the same
  `message.id` are one answer. It is counted once, with the largest count of
  each kind. An answer of the model `<synthetic>` is not counted.

**A record of Codex** has one JSON object in each line, with `type` and
`payload`: `turn_context` gives `payload.model`; `response_item` with
`payload.type` `message` and `payload.role` `user` gives the brief in
`payload.content`; `token_usage_record` gives `payload.response_id` and
`payload.usage` with `input_tokens`, `cached_input_tokens`,
`cache_write_input_tokens` and `output_tokens`. Uncached input is
`input_tokens` less the other two. A running total that names no request is
not counted. This is the shape the previous reader of this project knew; the
Codex documentation describes no line and calls the format unstable.

**Unknown.** The tokens of a start are unknown when its record does not
exist or cannot be read, holds no count, holds a whole line that is not a
JSON object, or holds an answer without its input or output count. A last
line that does not end with a line end is skipped: the host may still be
writing it.

**The label** is the first line of the brief:
`cw-task: <task> <stage> <round>`. The stage gives the role: `lookup` is
`lookup`; `spec` is `spec-writer`; `design` is `architect`; `tests` is
`quality-engineer`; `code` is `developer`; `spec-review`, `design-review` and
`review` are `reviewer`; `commit` is `git`.

**`cw tokens [--task <task>] [--json]`** reads the stored files. Without a
task it prints, as JSON, `{"tasks": [...], "unlabelled": [...], "sessions":
[...]}`. With a task it prints the object of that task, with `unlabelled` and
`sessions` narrowed to the sessions that ran it, and exits 1 when no start
names the task.

| Field | Content |
| --- | --- |
| `task` | The name of the task. |
| `total` | The tokens of every start of the task that is known, and of the session when it ran this task alone. A number. |
| `complete` | `false` when a start of the task, or its session, is unknown: `total` and every sum is then at least the number shown. |
| `starts` | The number of role starts. |
| `writing` | The tokens of `spec`, `design`, `tests` and `code` in round 1. |
| `flow` | An object: `total`, and its parts `coordination`, `review`, `lookup`, `git` and `rework`. |
| `flow_share` | `flow.total` as a share of `total`, in whole per cent, rounded. |
| `roles` | One entry for each role that started: `role`, `starts`, `unknown_starts` when there is one, `input`, `cache_write`, `cache_read`, `output`, `total`, `models` (the names). `total` is the string `unknown` when no start of the entry is known. |
| `stages` | One entry for each stage and round, with `stage`, `round`, `role` and the fields of a role entry. |
| `unknown_records` | The records whose tokens are unknown. |
| `shared_sessions` | The sessions that ran another task too; their own tokens are in no task's total. |
| `unlabelled` | One entry for each start without a label: `session`, `agent`, `record`, and the fields of a role entry without `role`. |
| `sessions` | One entry for each session: `session`, `host`, `record`, `tasks` (the tasks it ran), `shared` (`true` with more than one task), and the counts and `total` of the session itself. |

In words, `cw tokens --task <task>` prints the total, the sentence "N of M
tokens, P%, went to the workflow and not to the first writing of the
specification, the design, the tests and the code", the split, one line for
each role and for each stage and round, and, when a start is unknown, how many
are and that every sum is at least the number shown. Numbers have a comma
between their thousands. It prints no price.

## Rules` with the columns Rule and Checked by. |
| `tasks.md` | `architect`; the coordinator ticks the boxes | `- [ ] N.M <text>. Size: S\|M\|L. Depends on: <ids or none>. Check: <...>. Source: <...>.` under `## N. <group>`. |
| `tests.md` | `quality-engineer` | A table with the columns Scenario, Test file, Text. A row that is not automated holds `not automated` as its file and a reason as its text. |
| `reviews.md` | the coordinator | Three tables: under `## Reviews` the columns Stage, Round, Date, Content; under `## Findings` the columns Id, Stage, Round, Severity, Where, Finding, Disposition, Reason; under `## Approvals` the columns What, Date, Content. Stage is `spec`, `design` or `code`; What is `spec` or `design`; Round is a number from 1. |

How the files are read:

- A line inside a fenced block is not read, so an example is never taken for
  content. Line ends of any system are read alike.
- A cell may stand in backticks. `\|` is a bar inside a cell. A table is
  found by its heading, and `tests.md` by the column Scenario.
- **Specifications.** The rules apply to requirements under `ADDED` and
  `MODIFIED`. A requirement under `REMOVED` needs only its name. `SHALL` or
  `MUST` counts in the text above the first scenario.
- **Tasks.** A task may go on in indented lines. A task names a requirement
  when its text holds the name. `Depends on:` is read up to the next field.
- **Design.** New is `yes` or `no`. Depends on holds block names, or `none`.
  A dependency on a name the table does not hold is not followed. Checked by
  is `review` or the id of a check; several are joined by commas.
- **Tests.** The test file is a path from the root of the repository. A row
  that is `not automated` without a reason breaks the rule of the table.
- A file that is missing breaks no rule. `cw change status` names it as
  missing at its stage.

### `cw change status [<task>] [--json]`

With a task: the stage, what is missing, the number of scenarios that are not
automated, and the three content ids. JSON: `{"task", "stage", "missing":
[...], "not_automated", "content": {"spec", "design", "code"}}`. Without a
task: one line for each folder, and as JSON `{"changes": [...]}` with one such
object each. It exits 0 at any stage, and 1 when the named folder does not
exist. The stages are `spec`, `design`, `tests`, `code`, `review`, `commit`
and `done`.

A content id is the first 12 characters of a SHA-256:

| Id | Made from |
| --- | --- |
| `spec` | `proposal.md` and every file under `specs/`, each with its path, line ends as LF. |
| `design` | `design.md` and `tasks.md`, line ends as LF, every task mark read as `[ ]`. |
| `code` | The entries of the snapshot of the working tree, as `git ls-tree -r` prints them, for every path outside `openspec/changes/`. |

A review or an approval counts when its Content is the id of the working
tree. A review of the code also counts when `HEAD` holds the folder as the
working tree does and its Content is the `code` id of the last commit that
changed the folder: the task was committed with the code that was reviewed,
and what came later is later work. An empty id, of files that are missing,
matches nothing.

### `cw change check [<task>]`

Exits 0, or 5 with one line for each broken rule: the file, the line where
there is one, and the rule. It reads the files alone and takes no snapshot, so
a repository can declare it as a check. It does not judge whether a review is
of the current content; `cw change status` does.

### Tokens

- The plugin's hooks for the end of a role and the end of a session run
  `cw tokens record`, which reads the host's notice from standard input.
- For Claude Code the notice names the transcript of the role or of the
  session. `cw` sums the `usage` of each answer once, by its message id, reads
  the model from the answer and the label from the first line of the first
  message, and writes `cw/tokens/<session>/<agent>.json`. Writing the same
  notice twice gives the same file.
- `cw tokens [--task <task>] [--json]` reads those files. JSON: `task`,
  `total`, `complete`, `writing`, `flow` (`coordination`, `review`, `lookup`,
  `git`, `rework`), `flow_share`, `roles`, `stages`, `unlabelled`, `sessions`.

## Rules

| Rule | Checked by |
| --- | --- |
| No package outside `internal/cli` imports `internal/cli` | review |
| `cw` links no network package | review |
| A role text names no language, build tool, test framework or package manager | review |
| A host's role file is generated from the role text and never edited by hand | review |

"review" stands for a Go test in each case; this repository's `cw.toml` does
not exist yet, so no check id can be named. Task 6.3 replaces the word when it
does.

## Validation

| What | How | Where it runs | What it shows, and what it does not |
| --- | --- | --- | --- |
| The scenarios without "(evaluation)" | Tests that drive `cw` in temporary repositories, and tests of the plugin's files | Linux, locally | The commands and the files. |
| The token report | Recorded sessions kept as test data, built to the shape today's reader expects | Linux | That the sums are right for that shape. Not that a current host still writes it. |
| The scenarios with "(evaluation)" | Model runs of the two paths in three repositories | Not run | Nothing yet. This is the owner's step. |
| Codex | Nothing | Not run | Nothing. |

## Failure behaviour

| Condition | What `cw` does |
| --- | --- |
| `cw-roles.toml` is invalid | Every `cw roles` command exits 1 with the file, the line and the rule. |
| A role's record cannot be read | Its tokens are `unknown`, and every sum says that it is incomplete. |
| `cw tokens record` fails | It exits 1 with the reason on standard error. The hook command ends with exit 0 all the same, so that it never stops a role or a turn. |
| A brief has no label | Its tokens are listed as `unlabelled`. |
| A change folder is half written | `cw change check` passes when the files it has obey their rules; `cw change status` names the stage. |
| The repository has no `cw.toml` | `cw tokens record` stores nothing; the other commands work. |
