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
  the differing paths, `kitsu.toml` and the record store.
- **What cannot be done alone.** Nothing here was run with a model, and
  nothing was checked on Codex. The evaluation (Success measures 1 and 7)
  needs model runs the owner pays for.

## Decision

The workflow is text for the developer's coding agent, and `kitsu` checks
afterwards what a program can check. Three alternatives were rejected:

- **A `kitsu` that calls models itself.** It would need keys, a network
  connection and an agent loop, and the gate promises to use no network.
- **Today's task store.** State outside the folder is what blocked finished
  work (H2, H9).
- **Limits enforced before a role writes.** A host can remove a tool from a
  role but cannot limit a shell to some paths, so the check runs after the
  role ends, on the paths that differ between two snapshots.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `internal/roles` | The seven roles, their models, their write limits, `kitsu-roles.toml`, and the texts | `internal/decl`, `internal/gitx` | yes | `internal/modelpolicy` on `workflow-v3`: it validates profiles and scopes this design does not have; the path patterns of `internal/decl` are reused |
| `internal/change` | Reads a change folder; decides its stage; checks its rules; makes the content ids | `internal/decl` | yes | `internal/skillcheck` on `workflow-v3`: it checks skill files, not change folders |
| `internal/tokens` | Reads the host's record of a session; stores counts; reports them | `internal/roles`, for the names of the roles | yes | `internal/usagelog` on `workflow-v3`: its two readers are the starting point; prices, budgets and history are not carried |
| `internal/cli` | The commands `kitsu roles`, `kitsu change` and `kitsu tokens`; gathers from git what a stage needs | `internal/roles`, `internal/change`, `internal/tokens`, `internal/gitx`, `internal/store` | no | |
| `internal/decl` | Path patterns and strict TOML decoding | none | no | |
| `internal/gitx` | Snapshot and differing paths | none | no | |
| `internal/store` | The directory `kitsu/` in the common git directory | none | no | |

## Interfaces

### `kitsu roles [--json]`

Prints the seven roles: job, what each may write, model and effort on each
host, and the size of its text. JSON: `{"roles": [{"id", "job", "writes",
"text_chars", "models": {"claude": {"model", "effort"}, "codex": {...}}}],
"coordinator_text_chars"}`.

### `kitsu roles text <role>`

Prints the one text of a role, or of `coordinator`. A host without role files
of its own gives this text to a new worker. An unknown name exits 2. The size
of a text is counted in characters, each Unicode code point as one.

### `kitsu-roles.toml`

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

### `kitsu roles check <role> --from <tree> [--task <task>]`

Takes the snapshot, lists the paths that differ from `<tree>` with their kind
(added, changed, removed), and compares each with the role's limit. Exit 0, or
5 with the paths outside the limit, or 1 when the role needs the test paths
and `kitsu-roles.toml` does not name them. The test paths are looked up
before any path is judged, so exit 1 comes before exit 5. An unknown role, a
missing `--from`, or a role that needs `--task` without it, exits 2. In a clone whose hooks
git does not run, it prints the warning `kitsu status` prints.

### The change folder

| File | Written by | Format |
| --- | --- | --- |
| `proposal.md` | `spec-writer` | Free text. |
| `specs/<capability>/spec.md` | `spec-writer` | `## ADDED`, `## MODIFIED` or `## REMOVED Requirements`; `### Requirement: <name>`; a `Source:` line; `#### Scenario: <name>` with one `- **WHEN**` and one `- **THEN**` line. |
| `design.md` | `architect` | Free text, plus a table under `## Building blocks` with the columns Block, Responsibility, Depends on, New, Reuse considered, and a table under `## Rules` with the columns Rule and Checked by. |
| `tasks.md` | `architect`; the coordinator ticks the boxes after the review of the code has no open finding | `- [ ] N.M <text>. Size: S\|M\|L. Depends on: <ids or none>. Check: <...>. Source: <...>.` under `## N. <group>`. |
| `tests.md` | `quality-engineer` | A table with the columns Scenario, Test file, Text. A row that is not automated holds `not automated` as its file and a reason as its text. Under `## Changed tests`, one line for each existing test it changed or removed, with the requirement or the reason; `kitsu` does not read it. |
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
- **Tests.** The test file is a path from the root of the repository; a path
  that is absolute or leaves the root, also through a symbolic link, breaks
  the rule of the table and is not read. A row
  that is `not automated` without a reason breaks the rule of the table.
- A file that is missing breaks no rule. `kitsu change status` names it as
  missing at its stage.

### `kitsu change status [<task>] [--json]`

With a task: the stage, what is missing, the number of scenarios that are not
automated, the three content ids, and the number of rules `kitsu change check`
finds broken, because a stage says how far the record is and not that its
files are valid. JSON: `{"task", "stage", "missing": [...], "not_automated",
"content": {"spec", "design", "code"}, "broken_rules"}`. Without a
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

### `kitsu change check [<task>]`

Exits 0, or 5 with one line for each broken rule: the file, the line where
there is one, and the rule. It reads the files alone and takes no snapshot, so
a repository can declare it as a check. It does not judge whether a review is
of the current content; `kitsu change status` does.

### Tokens

**Notices.** On both hosts the plugin's hooks for the end of a role
(`SubagentStop`) and the end of the session (`SessionEnd`) run
`kitsu tokens record --host claude` or `--host codex`; without `--host` the host
is `claude`. The command reads the host's notice from standard input: one JSON
object. It uses six fields and ignores the rest:

| Field | Meaning |
| --- | --- |
| `session_id` | The session. In a notice about a role it is the session that started the role. |
| `transcript_path` | The record the host keeps of the session. |
| `agent_id` | The role that ended; absent at the end of the session. |
| `agent_transcript_path` | The record the host keeps of that role; absent at the end of the session. |
| `agent_type` | The name of the role as the host knows it: `architect`, or `kitsu:architect` for the role of a plugin. `kitsu` takes the text after the last colon, and uses it only when it is one of the seven roles (`roles.Known`). Claude Code ends its own agents, such as a prompt suggestion, with an empty `agent_type`; such a notice of Claude Code stores no start. |
| `cwd` | The directory the host works in. `kitsu` finds the repository from it, and from its own working directory when it is empty. |

For each notice `kitsu` reads the record of the role, when the notice names one,
and the record of the session, and writes one file for each:
`kitsu/tokens/<session>/agent-<agent id>.json` and
`kitsu/tokens/<session>/session.json`, in the common git directory. So the
session's own tokens are current after every role, not only at the end. The
same notice given again writes the same files. The command prints nothing on
standard output, stores nothing where the root holds no `kitsu.toml` or outside a
repository, and exits 0 in both cases; a notice that is not JSON is then not
reported.

At each notice `kitsu` also reads again the record of every start already stored
for that session whose record still exists and whose size has changed, and
replaces its counts when the new reading is known. Claude Code writes its
transcript asynchronously, so at `SubagentStop` or `SessionEnd` it may not
yet hold the last messages; Codex runs `SubagentStop` at the end of every turn
of a sub-agent, before it flushes the record. The last notice of a session has
no later one, so its last turn can still be missing.

Codex gives a hook at the end of a session between 1 and 3 seconds, so that
hook has a timeout of 3 in `hooks/codex-hooks.json`. The hook for the end of a
role has 30 on both hosts. Claude Code gives the `SessionEnd` hooks of a
plugin 1.5 seconds in all and ignores their own timeout, unless the user sets
`CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`. `kitsu tokens record` took 0.12 s for a
built session record of 50 MB and 1.15 s for one of 500 MB, read from memory.

**A stored file** holds `version`, `host`, `session`, `agent`, `record` (the
path of the host's file), `task`, `stage` and `round` from the label (absent
without one), `role` (the role the notice names, kept only when the record
gives no label), `known`, `why` (why the counts are unknown), and `models`: for
each model name the record gives, the four counts `input`, `cache_write`,
`cache_read` and `output`, and `size`, the size of the record before it was
read. It holds no text of a brief or an answer.

**A record of Claude Code** has one JSON object in each line:

- A line with `"type": "user"` holds the message in `message.content`: a
  string, or a list of parts of which one has `text`. The first such line of a
  role's record is its brief, and the first line of the brief is the label.
- A line with `"type": "assistant"` holds `message.id`, `message.model` and
  `message.usage` with `input_tokens`, `cache_creation_input_tokens`,
  `cache_read_input_tokens` and `output_tokens`. Lines with the same
  `message.id` are one answer. It is counted once, with the largest count of
  each kind. An answer of the model `<synthetic>` is not counted.

Claude Code 2.1 writes one line for each block of an answer, each with the
usage of the whole answer, and keeps the record of a role at
`<session>/subagents/agent-<agent_id>.jsonl`. This was read from 170 records
on the owner's machine on 14 November 2026, where the reader and a separate
count gave the same totals.

**A record of Codex** has one JSON object in each line, with `type` and
`payload`: `turn_context` gives `payload.model`; the first `response_item`
with `payload.type` `message` and `payload.role` `user` whose text does not
begin with `# AGENTS.md instructions` or `<environment_context>` gives the
brief. After each request an `event_msg` with `payload.type` `token_count`
gives `payload.info.total_token_usage`, the running total of the thread, and
`payload.info.last_token_usage`, the request, each with `input_tokens`,
`cached_input_tokens`, `cache_write_input_tokens` and `output_tokens`.
Uncached input is `input_tokens` less the other two. The thread is counted
once, from its totals: each rise of the total goes to the model of the
`turn_context` before it, so the sum is the last total. A total that falls is
a reset: Codex sets the total to a value with no input and no output after a
context window overflows (`fill_to_context_window`). The total before the fall
is carried, and the count is the carried sum and the last total. A first total
larger than its own request makes the counts unknown, and so does a record
with no `token_count` that holds `info`. This shape was read
from 16 rollouts that codex-cli 0.141 and 0.142 wrote on the owner's machine
on 14 November 2026; none of them was the record of a subagent, so how a
child thread relates to its parent is not known. The first version of this
reader expected a line `token_usage_record`, which no real record on that
machine held. A review says codex-cli 0.168.0 writes one after each response;
the reader skips such a line, and a test holds that it changes no count.

**Unknown.** The tokens of a start are unknown when its record does not
exist or cannot be read, holds no count, holds a whole line that is not a
JSON object, or holds an answer without its input or output count. A last
line that does not end with a line end is skipped: the host may still be
writing it. A notice that names a role (`agent_id`) and no record of it
stores that start with unknown tokens. A stored file of `kitsu` that cannot be
read is named under `damaged` in the JSON of `kitsu tokens`, and every task of
its session is incomplete and names the file. In words the report tells the
unknown starts of roles from the unknown record of the session itself.

**The label** is the first line of the brief:
`kitsu-task: <task> <stage> <round>`. The stage gives the role: `lookup` is
`lookup`; `spec` is `spec-writer`; `design` is `architect`; `tests` is
`quality-engineer`; `code` is `developer`; `spec-review`, `design-review` and
`review` are `reviewer`; `commit` is `git`.

**A start whose record gives no label.** When its tokens are known, the brief
was read and held no label: the start is listed under `unlabelled` and belongs
to no task. When its tokens are unknown, the label could not be read either,
so the start may belong to any task of its session:

- When the notice named one of the seven roles and the session ran exactly one
  task, the start is a start of that role in that task: the role's entry
  counts it under `starts` and `unknown_starts`, and it is in no stage.
- Otherwise it is listed under `unlabelled`, with its `role` when the notice
  named one.
- In both cases every task of that session has `complete` `false` and names
  the record under `unknown_records`.

**`kitsu tokens [--task <task>] [--json]`** reads the stored files. Without a
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
| `unknown_records` | The records whose tokens are unknown and are, or may be, tokens of the task. |
| `shared_sessions` | The sessions that ran another task too; their own tokens are in no task's total. |
| `unlabelled` | One entry for each start without a label that was given to no task: `session`, `agent`, `record`, and the fields of a role entry; `role` only when the notice named one. |
| `sessions` | One entry for each session: `session`, `host`, `record`, `tasks` (the tasks it ran), `shared` (`true` with more than one task), and the counts and `total` of the session itself. |

In words, `kitsu tokens --task <task>` prints the total, the sentence "N of M
tokens, P%, went to the workflow and not to the first writing of the
specification, the design, the tests and the code", the split, one line for
each role and for each stage and round, and, when a start is unknown, how many
are and that every sum is at least the number shown; the sentence then says
"N of M known tokens", because the share is a share of what is known. Numbers have a comma
between their thousands. It prints no price.

## Rules

| Rule | Checked by |
| --- | --- |
| No package imports `internal/cli` except `cmd/kitsu` | test |
| `kitsu` links no network package (on Windows: none but through `golang.org/x/sys/windows`) | test |
| A role text names no language, build tool, test framework or package manager | review, test |
| A host's role file is generated from the role text and never edited by hand | test |
| The files of a change folder obey the rules of `kitsu change check` | record |

`test` and `record` are checks of this repository's `kitsu.toml`: `test` runs
every Go test, and `record` runs `kitsu change check`. The test of the third rule
searches for a list of names, so a reviewer still reads the texts.

## Validation

| What | How | Where it runs | What it shows, and what it does not |
| --- | --- | --- | --- |
| The scenarios without "(evaluation)" | Tests that drive `kitsu` in temporary repositories, and tests of the plugin's files | Linux, locally | The commands and the files. |
| The token report | Recorded sessions kept as test data, built to the shape today's reader expects | Linux | That the sums are right for that shape. Not that a current host still writes it. |
| The scenarios with "(evaluation)" | Model runs of the two paths in three repositories | Not run | Nothing yet. This is the owner's step. |
| Codex | Nothing | Not run | Nothing. |

## Failure behaviour

| Condition | What `kitsu` does |
| --- | --- |
| `kitsu-roles.toml` is invalid | `kitsu roles` and `kitsu roles check` exit 1 with the file, the line and the rule. `kitsu roles text` does not read the file. |
| A role's record cannot be read | Its tokens are `unknown`, and every sum says that it is incomplete. |
| `kitsu tokens record` fails | It exits 1 with the reason on standard error. The hook command ends with exit 0 all the same, so that it never stops a role or a turn. |
| A brief has no label | Its tokens are listed as `unlabelled`. |
| A change folder is half written | `kitsu change check` passes when the files it has obey their rules; `kitsu change status` names the stage. |
| The repository has no `kitsu.toml` | `kitsu tokens record` stores nothing; the other commands work. |
