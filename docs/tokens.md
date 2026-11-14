# Tokens

`cw tokens` reports how many tokens a task used, role by role and stage by stage, and how many of
them went to the workflow rather than to the first writing of the specification, the design, the
tests and the code. The counts come from the record the host keeps of a session, read when the
plugin's hooks say that a role or the session has ended. `cw` never takes a count from a model.
It reports no price and no amount of money.

The report is a split of one task's tokens. It is not a comparison with one agent working alone,
and it shows no saving and no benefit; see [What is not measured](#what-is-not-measured).

## What is counted

For every start of a role, and for the developer's own session, `cw` reads four kinds of tokens:

| Kind | JSON field | Meaning |
| --- | --- | --- |
| Uncached input | `input` | Input that was not read from the host's cache. |
| Cache write | `cache_write` | Input written to the cache. |
| Cache read | `cache_read` | Input read from the cache. |
| Output | `output` | Output of the model. |

The total of a start is the sum of the four. `cw` also keeps the model names the record gives.

### Where the counts come from

The plugin of this repository runs `cw tokens record` from two hooks of the host:

| Host | Hook | Command | Timeout |
| --- | --- | --- | --- |
| Claude Code (`hooks/hooks.json`) | `SubagentStop`, when a role ends | `cw tokens record --host claude \|\| true` | 30 s |
| Claude Code (`hooks/hooks.json`) | `SessionEnd`, when the session ends | `cw tokens record --host claude \|\| true` | 30 s |
| Codex (`hooks/codex-hooks.json`) | `SubagentStop` | `cw tokens record --host codex \|\| true` | 30 s |
| Codex (`hooks/codex-hooks.json`) | `SessionEnd` | `cw tokens record --host codex \|\| true` | 3 s |

Each command runs only when `cw` is on the `PATH`, and ends with `|| true`, so a failure of `cw`
never stops a role or a turn. On Windows the Codex command is `cw tokens record --host codex`
alone.

The host gives the hook a notice: one JSON object on standard input. `cw` reads five of its
fields and ignores the rest:

| Field | Meaning |
| --- | --- |
| `session_id` | The session. In a notice about a role, the session that started the role. When it is absent, the name of the file of `transcript_path` without its extension is used. |
| `transcript_path` | The record the host keeps of the session. |
| `agent_id` | The role start that ended. When it is absent, the name of the file of `agent_transcript_path` is used. Absent at the end of the session. |
| `agent_transcript_path` | The record the host keeps of that role start. Absent at the end of the session. |
| `agent_type` | The name of the role as the host knows it, such as `architect` or `agent-workflows:architect`. `cw` takes the text after the last colon and keeps it only when it is one of the seven roles. |

For each notice, `cw` reads the record of the role when the notice names one, and the record of
the session, and stores the counts of each. So the session's own count is current after every
role, not only at the end. The same notice given twice stores the same files, and the totals do
not change.

### How a record is read

**Claude Code.** One JSON object per line.

- The first line with `"type": "user"` holds the brief in `message.content`: a text, or a list of
  parts of which the first with a `text` is taken.
- A line with `"type": "assistant"` holds `message.id`, `message.model` and `message.usage` with
  `input_tokens`, `cache_creation_input_tokens`, `cache_read_input_tokens` and `output_tokens`.
  Lines with the same `message.id` are one answer, counted once with the largest count of each
  kind. An answer of the model `<synthetic>` is not counted.
- Claude Code writes one line for each block of an answer, and each of them holds the usage of
  the whole answer. Lines of other types, such as `api-request`, `attachment`, `system` and
  `cost-state`, are not answers and are not counted.
- The record of a role is `<session>/subagents/agent-<agent_id>.jsonl` beside the record of the
  session, and the session's own record holds none of its lines.

**Codex.** One JSON object per line, with `type` and `payload`.

- `turn_context` gives the model in `payload.model`, for the lines that follow.
- The first `response_item` with `payload.type` `message` and `payload.role` `user` holds the brief
  in `payload.content`, unless its text begins with `# AGENTS.md instructions` or
  `<environment_context>`: Codex adds those before the first prompt of a thread, and the brief is
  then the next such message.
- After each request Codex writes an `event_msg` with `payload.type` `token_count`. Its
  `payload.info.total_token_usage` is the running total of the thread, and
  `payload.info.last_token_usage` the request itself. Each holds `input_tokens`,
  `cached_input_tokens`, `cache_write_input_tokens` and `output_tokens`; uncached input is
  `input_tokens` less the other two input counts, and `output_tokens` includes the reasoning.
- The thread is counted once, from its last running total. Each rise of the total goes to the
  model of the `turn_context` before it. A total written twice adds nothing. A `token_count` with
  `info` `null` holds only rate limits and is skipped.
- A running total that falls, or a first running total larger than its own request, is not
  understood: the tokens of the record are unknown. A record with no `token_count` that holds
  `info` holds no count, so its tokens are unknown.

## The label

The first line of every brief the coordinator writes is a label:

```text
cw-task: <task> <stage> <round>
```

`<task>` is lower-case letters and digits in words joined by hyphens, `<stage>` is one of the nine
below, and `<round>` is a number from 1 that grows each time the same stage starts again. Blank
lines before the label are skipped. A first line in any other form is no label. `cw` gives the
tokens of the start to that task, stage and round, and to the role of the stage:

| Stage | Role | Round 1 counts as | A later round counts as |
| --- | --- | --- | --- |
| `lookup` | `lookup` | lookup | lookup |
| `spec` | `spec-writer` | writing | rework |
| `spec-review` | `reviewer` | review | review |
| `design` | `architect` | writing | rework |
| `design-review` | `reviewer` | review | review |
| `tests` | `quality-engineer` | writing | rework |
| `code` | `developer` | writing | rework |
| `review` | `reviewer` | review | review |
| `commit` | `git` | git | git |

The role comes from the stage of the label, not from the role the host started. A label is an
instruction to the coordinator; `cw` cannot tell whether the coordinator wrote the right one.

## Writing tokens and workflow tokens

`cw tokens` splits the tokens of a task in two:

- **Writing**: the starts of `spec`, `design`, `tests` and `code` in round 1. That is the first
  writing of the specification, the design, the tests and the code.
- **Workflow**: everything else, in five parts:
  - `coordination`: the developer's own session, which started the roles;
  - `review`: every start of `spec-review`, `design-review` and `review`;
  - `lookup`: every start of `lookup`;
  - `git`: every start of `commit`;
  - `rework`: a later round of a writing stage, such as `code 2`.

`flow_share` is the workflow tokens as a share of the task's total, in whole per cent, rounded
half up.

"Workflow tokens" are tokens that a run of this workflow spent outside the first writing. They are
not the extra cost of the workflow over one agent: one agent working alone would also search,
review its own work and commit, and would spend tokens on that too. Only the comparison in
[the evaluation](evaluation.md#the-comparison-with-one-agent) can say what the extra cost is.

### A worked example

The token-report specification describes one session, S1, that ran the full path for the task
`add-greeting` on Claude Code. A test of this repository builds a record of that shape. These are
its starts:

| Start | First line of the brief | Tokens | Part |
| --- | --- | ---: | --- |
| the session itself | none | 40,000 | coordination |
| `lookup`, twice | `cw-task: add-greeting lookup 1` | 10,000 | lookup |
| `spec-writer` | `cw-task: add-greeting spec 1` | 60,000 | writing |
| `reviewer` | `cw-task: add-greeting spec-review 1` | 30,000 | review |
| `architect` | `cw-task: add-greeting design 1` | 80,000 | writing |
| `reviewer` | `cw-task: add-greeting design-review 1` | 30,000 | review |
| `quality-engineer` | `cw-task: add-greeting tests 1` | 90,000 | writing |
| `developer` | `cw-task: add-greeting code 1` | 200,000 | writing |
| `reviewer` | `cw-task: add-greeting review 1` | 50,000 | review |
| `developer` | `cw-task: add-greeting code 2` | 40,000 | rework |
| `reviewer` | `cw-task: add-greeting review 2` | 20,000 | review |
| `git` | `cw-task: add-greeting commit 1` | 5,000 | git |

The sums:

- Writing: 60,000 + 80,000 + 90,000 + 200,000 = 430,000.
- Workflow: coordination 40,000 + review (30,000 + 30,000 + 50,000 + 20,000 = 130,000) + lookup
  10,000 + git 5,000 + rework 40,000 = 225,000.
- Total: 430,000 + 225,000 = 655,000.
- `flow_share`: 225,000 / 655,000 = 34.4%, rounded to 34.

With every notice of that record given to `cw tokens record` in a trial repository,
`cw tokens --task add-greeting` printed:

```text
add-greeting: 655,000 tokens in 12 starts of roles
225,000 of 655,000 tokens, 34%, went to the workflow and not to the first writing of the specification, the design, the tests and the code

         writing  430,000
        workflow  225,000
    coordination   40,000
          review  130,000
          lookup   10,000
             git    5,000
          rework   40,000

ROLE              STARTS  INPUT   CACHE WRITE  CACHE READ  OUTPUT  TOTAL    MODEL
lookup            2       1,500   1,500        6,000       1,000   10,000   claude-haiku-5-5
spec-writer       1       10,000  10,000       35,000      5,000   60,000   claude-opus-5-5
reviewer          4       17,000  23,000       82,000      8,000   130,000  claude-opus-5-5
architect         1       10,000  20,000       45,000      5,000   80,000   claude-opus-5-5
quality-engineer  1       10,000  20,000       55,000      5,000   90,000   claude-opus-5-5
developer         2       25,000  35,000       168,000     12,000  240,000  claude-opus-5-5
git               1       500     500          3,500       500     5,000    claude-sonnet-5-5

STAGE             ROUND   INPUT   CACHE WRITE  CACHE READ  OUTPUT  TOTAL    ROLE
lookup            1       1,500   1,500        6,000       1,000   10,000   lookup
spec              1       10,000  10,000       35,000      5,000   60,000   spec-writer
spec-review       1       5,000   5,000        18,000      2,000   30,000   reviewer
design            1       10,000  20,000       45,000      5,000   80,000   architect
design-review     1       5,000   5,000        18,000      2,000   30,000   reviewer
tests             1       10,000  20,000       55,000      5,000   90,000   quality-engineer
code              1       20,000  30,000       140,000     10,000  200,000  developer
code              2       5,000   5,000        28,000      2,000   40,000   developer
review            1       5,000   10,000       32,000      3,000   50,000   reviewer
review            2       2,000   3,000        14,000      1,000   20,000   reviewer
commit            1       500     500          3,500       500     5,000    git

session s1: 40,000 tokens of its own, counted as the coordination of add-greeting
workflow tokens are coordination (the session itself), review, lookup, git and rework (a later round of a writing role)
```

The model names are those of the test data. The numbers are of a built record, not of a session a
host wrote; they show how the sums are made, not what a real task costs.

## Commands

### cw tokens

```text
cw tokens [--task <task>] [--json]
```

Reads the stored counts of this clone and prints them. Without `--task` it prints one line per
task, then the sessions and the starts that belong to no task. The same record gave:

```text
TASK          TOTAL    WRITING  WORKFLOW  SHARE  STARTS  COMPLETE
add-greeting  655,000  430,000  225,000   34%    12      yes
session s1: 40,000 tokens of its own, counted as the coordination of add-greeting
`cw tokens --task <task>` prints the roles and the stages of one task
```

With `--task <task>` it prints the report of one task, as in
[the worked example](#a-worked-example). In a clone with nothing stored it prints
`no tokens are recorded in this clone; the plugin records them when a role or a session ends`.

| Exit | When |
| --- | --- |
| 0 | The report is printed. |
| 1 | `--task` names a task no stored start names (`cw: no start is recorded for the task `<task>``), or the directory is not in a git repository. |
| 2 | The command line is wrong. |

A stored file that cannot be read is named in a warning on standard error, and its tokens are in
no sum.

### cw tokens --json

Without `--task` the output is `{"tasks": [...], "unlabelled": [...], "sessions": [...]}`. With
`--task` it is the object of that task, with `unlabelled` and `sessions` narrowed to the sessions
that ran it. For the worked example it begins:

```json
{
  "task": "add-greeting",
  "total": 655000,
  "complete": true,
  "starts": 12,
  "writing": 430000,
  "flow": {
    "total": 225000,
    "coordination": 40000,
    "review": 130000,
    "lookup": 10000,
    "git": 5000,
    "rework": 40000
  },
  "flow_share": 34,
```

The fields of a task:

| Field | Content |
| --- | --- |
| `task` | The name of the task. |
| `total` | The tokens of every start of the task that is known, and of the session when it ran this task alone. Always a number. |
| `complete` | `false` when a start of the task, or its session, is unknown. `total` and every sum are then at least the number shown. |
| `starts` | The number of role starts of the task. The session is not a start. |
| `writing` | The tokens of `spec`, `design`, `tests` and `code` in round 1. |
| `flow` | `total`, and its parts `coordination`, `review`, `lookup`, `git` and `rework`. |
| `flow_share` | `flow.total` as a share of `total`, in whole per cent; 0 when `total` is 0. |
| `roles` | One entry for each role that started, in the order of the stages. |
| `stages` | One entry for each stage and round, with `stage`, `round` and `role`. |
| `unknown_records` | The records whose tokens are unknown and are, or may be, tokens of the task. Absent when there is none. |
| `shared_sessions` | The sessions that also ran another task; their own tokens are in no task's total. Absent when there is none. |
| `unlabelled` | With `--task`: the starts without a label of the sessions that ran the task. |
| `sessions` | With `--task`: the sessions that ran the task. |

An entry of `roles` or `stages`, of `unlabelled` and of `sessions` holds:

| Field | Content |
| --- | --- |
| `role` | The role. In `unlabelled`, only when the host's notice named one of the seven. |
| `starts` | The number of starts. |
| `unknown_starts` | How many of them are unknown. Absent when none is. |
| `input`, `cache_write`, `cache_read`, `output` | The four kinds, summed over the known starts. |
| `total` | Their sum, or the text `"unknown"` when no start of the entry is known. |
| `models` | The model names the records give; an empty list when no start of the entry is known. |

`unlabelled` entries also hold `session`, `agent` and `record`. `sessions` entries also hold
`session`, `host`, `record`, `tasks` (the tasks the session ran) and `shared` (`true` with more
than one task).

### cw tokens record

```text
cw tokens record [--host claude|codex]
```

Reads one notice from standard input, reads the records it names, and stores their counts. The
host is `claude` when `--host` is not given. It prints nothing on standard output. The plugin's
hooks run it; a person runs it only to test.

| Exit | When |
| --- | --- |
| 0 | The counts are stored; or the directory is not in a git repository, or the repository root holds no `cw.toml`: then nothing is stored and the notice is not read. |
| 1 | The input is not a JSON object; the notice names no session or no record; or a file cannot be written. |
| 2 | `--host` is neither `claude` nor `codex`, or the command line is wrong. |

An unreadable record is not a failure: its tokens are stored as unknown.

## Unknown is not zero

When `cw` cannot read a count it expects, the tokens of that start are unknown. They are never
taken as zero.

The tokens of a start are unknown when its record:

- does not exist or cannot be read;
- has a line longer than 64 MiB;
- holds no count at all, such as a Codex record whose `token_count` events hold only rate limits;
- holds an answer without its input or output count, or with a negative count; for Codex, counts
  whose cached and written input exceed the input, a running total that falls, or a first running
  total larger than its own request;
- holds a complete line that is not a JSON object;
- is not named: the notice says that a role ended and gives no record of it. The start is stored
  all the same, with the role the notice names.

A last line without a line end is skipped, not counted as broken: the host may still be writing
it. A task is also incomplete when no stored file holds the record of its session; its
`unknown_records` then holds `the record of session <id>`.

What the report then says:

- `complete` is `false`, and the record is listed under `unknown_records`.
- `total` and every sum count only the known starts. In words: `the tokens of 1 start are unknown,
  so every sum is at least the number shown:` followed by the records, and in the table without
  `--task`, `no: every sum is at least the number shown` under COMPLETE. When the unknown record
  is the session's own, the sentence names `the session itself`, not a start.
- The first sentence of the report then counts `known tokens`: the share is a share of what is
  known.
- A stored file of `cw` that cannot be read is named under `damaged` and in a warning. Every task
  of its session is incomplete and names the file under `unknown_records`.
- An entry with some starts unknown prints its total as `at least <n>`; an entry with all its starts
  unknown prints `unknown`.

For example, with the record of the `architect` start replaced by an empty file and a notice that
named `agent-workflows:architect`, the report of the worked example began:

```text
add-greeting: 575,000 tokens in 12 starts of roles
225,000 of 575,000 tokens, 39%, went to the workflow and not to the first writing of the specification, the design, the tests and the code
the tokens of 1 start are unknown, so every sum is at least the number shown:
  <record>/subagents/agent-a5.jsonl
```

Its row of `architect` read:

```text
architect         1       0       0            0           0       unknown
```

The stage `design` was missing from the stages, because the label of that start could not be
read. `writing` was 350,000, and `flow_share` rose to 39 only because a writing start was left
out. A share made from incomplete sums can be wrong in either direction.

### A start without a label

When the record of a start is read and its brief has no label, its tokens are known but belong to
no task. They are listed under `unlabelled` and are not dropped. With the label of the `git` brief
removed from the worked example, `cw tokens` printed:

```text
TASK          TOTAL    WRITING  WORKFLOW  SHARE  STARTS  COMPLETE
add-greeting  650,000  430,000  220,000   34%    11      yes
session s1: 40,000 tokens of its own, counted as the coordination of add-greeting
a start without a label in session s1: 5,000 tokens, in no task (<record>/subagents/agent-a12.jsonl)
`cw tokens --task <task>` prints the roles and the stages of one task
```

### A start whose record gives no label and no count

When the tokens of a start are unknown, its label could not be read either, so it may belong to
any task of its session:

- When the notice named one of the seven roles and the session ran exactly one task, the start is
  counted as a start of that role in that task, under `starts` and `unknown_starts`, and in no
  stage.
- Otherwise it is listed under `unlabelled`, with its `role` when the notice named one.
- In both cases every task of that session has `complete` `false` and lists the record under
  `unknown_records`.

With the `architect` record empty and a notice whose `agent_type` named no role, `cw tokens`
printed:

```text
TASK          TOTAL    WRITING  WORKFLOW  SHARE  STARTS  COMPLETE
add-greeting  575,000  350,000  225,000   39%    11      no: every sum is at least the number shown
session s1: 40,000 tokens of its own, counted as the coordination of add-greeting
a start in session s1 whose record gives no label and no count: it may belong to any task of that session (<record>/subagents/agent-a5.jsonl)
`cw tokens --task <task>` prints the roles and the stages of one task
```

### Two tasks in one session

The session's own tokens are counted once, as `coordination`. When the session ran one task, they
are part of that task's total. When it ran more than one, they go to no task's total: the session
is listed under `sessions` with the names of the tasks and `"shared": true`, and each task lists it
under `shared_sessions`. With a second task, `fix-typo`, of one `developer` start of 15,000 tokens
in the same session, `cw tokens` printed:

```text
TASK          TOTAL    WRITING  WORKFLOW  SHARE  STARTS  COMPLETE
add-greeting  615,000  430,000  185,000   30%    12      yes
fix-typo      15,000   15,000   0         0%     1       yes
session s1: 40,000 tokens of its own, shared by the tasks add-greeting, fix-typo and counted in the total of none
`cw tokens --task <task>` prints the roles and the stages of one task
```

## What is stored

`cw tokens record` writes one JSON file for each start and one for the session, in the common git
directory of the clone (`git rev-parse --git-common-dir`, usually `.git`):

```text
cw/tokens/<session>/agent-<agent id>.json
cw/tokens/<session>/session.json
```

A name that is not safe as a file name is replaced by a digest of it. A file is written whole or
not at all. One file, for the first `developer` start of the worked example:

```json
{
  "version": 1,
  "host": "claude",
  "session": "s1",
  "agent": "agent-a8",
  "record": "<record>/subagents/agent-a8.jsonl",
  "task": "add-greeting",
  "stage": "code",
  "round": 1,
  "known": true,
  "models": {
    "claude-opus-5-5": {
      "input": 20000,
      "cache_write": 30000,
      "cache_read": 140000,
      "output": 10000
    }
  }
}
```

A file holds the counts, the model names, the task, stage and round of the label, the path of the
host's record, and, when the counts are unknown, `"known": false` and the reason in `why`. When the
record gave no label it holds the `role` the notice named. It holds no text of a brief, a prompt
or an answer: a search of the stored files for a sentence of a brief finds nothing.

Nothing leaves the machine. `cw` links no network package, and a test of this repository fails
when it does. The stored files are not committed: they are inside the git directory, not the
working tree.

## Limits

- **The record formats are not documented by the hosts.** Claude Code documents where it keeps
  its records, not what a line holds. The Codex documentation calls its format unstable and
  describes no line. A host that changes its format makes counts unknown, or wrong if a field
  changes its meaning.
- **The readers were checked against real records of one machine only**; see
  [Checked against real records](#checked-against-real-records).
- **The Codex `SessionEnd` hook has a timeout of 3 seconds**, because Codex gives that hook between
  1 and 3 seconds. A long record may not be read in time; the session's count then stays as the
  last role's notice left it.
- **A notice without a role's record gives a start with unknown tokens.** The Codex documentation
  says `agent_transcript_path` may be null. The label is in that record, so the task of such a
  start is known only when its session ran one task and the notice named the role.
- **`cw` trusts the notice.** It reads the files the notice names; it cannot tell whether the
  notice came from the host.
- **The label is the coordinator's.** A wrong label gives the tokens to the wrong task, stage or
  round.

## Checked against real records

On 2026-11-14 the readers were run on records that the hosts wrote on one developer machine. Only
the structure and the numbers of the records were read. A separate script counted each record
independently: for Claude Code, per `message.id` the largest value of each kind; for Codex, the
last running total. The tests of this repository reproduce each structure with invented text.

**Claude Code**, installed version 2.1.311. 2 session records and 168 role records (103,573 lines)
were examined:

- 16,486 answers; 12,268 of them were written in 2 to 13 lines. The lines of one answer never
  differed in a count, and every line had a `stop_reason`. No answer lacked its final line.
- 9 lines of the model `<synthetic>` were errors the host made up, with all counts zero.
- Each role record was named `agent-<agent_id>.jsonl`, and every line in it gave that
  `agentId`. Its first line was the brief, a string.
- `cw tokens record` matched the independent count in all four kinds for the largest session record
  (18,198 lines) and for all 168 role records. The reader needed no change.

**Codex**, installed version codex-cli 0.168.0. The 16 rollouts on the machine were written by
codex-cli 0.141.0 and 0.142.3 (64,138 lines):

- No line was a `token_usage_record`, the shape the first reader expected; it found no count in any
  rollout. The reader was rewritten to read `token_count` events.
- In the 13 rollouts with counts, the running total never fell, also after a `compacted` line;
  each rise equalled `last_token_usage`; the first total equalled its own request; and 114 totals
  were written twice. `cache_write_input_tokens` was 0 in every event.
- The first user message of every rollout was the host's context, never a prompt.
- After the change `cw tokens record` matched the independent count for every rollout with counts,
  for example the largest (21,867 lines). The 3 rollouts without a `token_count` that holds `info`
  gave unknown tokens.

**Time.** `cw tokens record` took at most 0.30 s for one notice, for the largest rollout, with the
file in the page cache of the system. A read from a cold disk was not timed.

**Not checked:**

- Every rollout was a thread a person started (`thread_source` `user`). No rollout of a Codex
  subagent was on the machine, so how a subagent's thread relates to its parent, and whether its
  first total holds the parent's, is not known. A first total larger than its own request makes
  the counts unknown, not counted twice.
- No rollout of the installed codex-cli 0.168.0 was on the machine.
- No brief of a role carried a label, so a label was read only from built records.
- Whether Codex counts `cache_write_input_tokens` inside `input_tokens` could not be seen: the count
  was always 0.
- The notices themselves: the fields were taken from the hook documentation, and no notice a host
  gave was recorded.

## What is not measured

The comparison with one agent working alone, on the same tasks, has not been run. It is defined in
[the evaluation](evaluation.md#the-comparison-with-one-agent). Until it is, nothing here shows that
the workflow saves tokens, costs a known amount more than one agent, or gives better code.
