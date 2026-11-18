# token-report

## Purpose

Show what a task cost in tokens, role by role, and how much of it the workflow
itself used, counted from the host's own record of the session and never from
what a model says.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `role-definitions` specification.
- `S1`: the record Claude Code kept of one session that ran the full path for
  the task `add-greeting`, stored as test data. Its tokens:

  | Start | First line of the brief | Tokens |
  | --- | --- | --- |
  | the session itself | none | 40,000 |
  | `lookup`, twice | `kitsu-task: add-greeting lookup 1` | 10,000 together |
  | `spec-writer` | `kitsu-task: add-greeting spec 1` | 60,000 |
  | `reviewer` | `kitsu-task: add-greeting spec-review 1` | 30,000 |
  | `architect` | `kitsu-task: add-greeting design 1` | 80,000 |
  | `reviewer` | `kitsu-task: add-greeting design-review 1` | 30,000 |
  | `quality-engineer` | `kitsu-task: add-greeting tests 1` | 90,000 |
  | `developer` | `kitsu-task: add-greeting code 1` | 200,000 |
  | `reviewer` | `kitsu-task: add-greeting review 1` | 50,000 |
  | `developer` | `kitsu-task: add-greeting code 2` | 40,000 |
  | `reviewer` | `kitsu-task: add-greeting review 2` | 20,000 |
  | `git` | `kitsu-task: add-greeting commit 1` | 5,000 |

  The 200,000 tokens of the first `developer` start are 20,000 of uncached
  input, 30,000 of cache write, 140,000 of cache read and 10,000 of output.
- "S1 is recorded" means that every notice the host gave during `S1`, of a
  role that ended and of the session that ended, was given to
  `kitsu tokens record` in `demo`, in the order the host gave them.
- `S2`: the record Codex kept of one session that ran the small path for the
  task `fix-empty-app`, stored as test data.

## ADDED Requirements

### Requirement: Counts from the host's record

`kitsu` SHALL take token counts only from the record the host keeps of a session, and SHALL accept no count from a role or from the coordinator. The plugin SHALL give `kitsu tokens record` each notice of the host that a role or a session has ended. `kitsu` SHALL then read the counts and store them under `kitsu/` in the common git directory. Where the root holds no `kitsu.toml`, it SHALL store nothing.

Source: the owner's statement of 14 November 2026 (a count of the tokens used extra because of the flow); `internal/usagelog/claude.go` and `codex.go` (they read the host's transcript today); Claude Code documentation, `hooks.md` (every hook input has `transcript_path`, the end of a role also `agent_transcript_path`; no input holds a total for a run).

#### Scenario: A recorded session

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting --json` runs
- **THEN** `total` is 655,000 and `complete` is `true`

#### Scenario: A directory that is not gated

- **WHEN** the notices of `S1` are given to `kitsu tokens record` in a git repository whose root holds no `kitsu.toml`
- **THEN** it exits 0 and no file is written

#### Scenario: A notice is given twice

- **WHEN** `S1` is recorded, the notice of the first `developer` start is given to `kitsu tokens record` again, and `kitsu tokens --task add-greeting --json` runs
- **THEN** `total` is 655,000

### Requirement: Notices as the host gives them

At each notice `kitsu tokens record` SHALL read again the record of every start already stored for that session whose record still exists, and SHALL replace its stored counts when the new reading is known. It SHALL find the repository from the notice's `cwd`, or from its working directory when `cwd` is empty. It SHALL NOT store as a start a notice of Claude Code that names an agent (`agent_id`) with an empty `agent_type`.

Source: review of the token report, 14 November 2026: Claude Code documentation, `hooks.md` (the transcript is written asynchronously and may not yet hold the last messages at `SubagentStop` or `SessionEnd`; every hook input has `cwd`; the host's own agents end with an empty `agent_type`); Codex source (`SubagentStop` runs at the end of each turn of a sub-agent, before its record is flushed).

#### Scenario: A record that grows after its notice

- **WHEN** the record of the first `developer` start lacks its last answer when its notice is given and holds it at the next notice, `S1` is recorded, and `kitsu tokens --task add-greeting --json` runs
- **THEN** `total` is 655,000 and the entry of `stages` for `code`, round 1, has `total` 200,000

#### Scenario: A repeated notice omits or cannot reread a transcript

- **WHEN** a subsequent notice for a captured worker omits its transcript path or a later read of the same worker or coordinator transcript fails
- **THEN** Kitsu reuses the saved worker path and counts each start once. A failed reread preserves the last measurement, marks coverage incomplete and can recover on a later notice even when the transcript size stays the same.

#### Scenario: An internal agent of the host

- **WHEN** `S1` is recorded, a notice that names the agent `aside01` with an empty `agent_type` and a record of 3,000 tokens is given to `kitsu tokens record`, and `kitsu tokens --json` runs
- **THEN** `unlabelled` is empty and no file is stored for `aside01`

#### Scenario: The directory of the notice

- **WHEN** every notice of `S1`, with `cwd` the root of `demo`, is given to `kitsu tokens record` running in a git repository whose root holds no `kitsu.toml`, and `kitsu tokens --task add-greeting --json` runs in `demo`
- **THEN** `total` is 655,000

### Requirement: Task label

The first line of every brief SHALL be `kitsu-task: <task> <stage> <round>`, with the stage `lookup`, `spec`, `spec-review`, `design`, `design-review`, `tests`, `code`, `review` or `commit`. `kitsu` SHALL give the tokens of that start to that task, stage and round, and to the role of the stage. It SHALL list the tokens of a start without such a line as `unlabelled` and SHALL NOT drop them.

Source: `skills/task-orchestration/references/hosts.md` (the coordinator writes every brief on both hosts); review finding M11 (usage that is not attributed disappears from the budget).

#### Scenario: Tokens go to the role of the stage

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting --json` runs
- **THEN** the entry of `roles` for `reviewer` has `starts` 4 and `total` 130,000, and the entry for `developer` has `starts` 2 and `total` 240,000

#### Scenario: A start without a label

- **WHEN** the first line of the `git` brief is removed from the test data, `S1` is recorded, and `kitsu tokens --json` runs
- **THEN** `unlabelled` holds one start with 5,000 tokens, and the `total` of `add-greeting` is 650,000

### Requirement: Kinds of tokens

For each role, and for each stage and round, `kitsu tokens` SHALL report the uncached input, cache write, cache read and output tokens, their sum, the number of starts, and the model the host's record names. It SHALL report no price and no amount of money.

Source: `internal/usagelog/import.go` (total input is uncached input plus cache writes plus cache reads); review finding M11 (the budget froze on a model missing from the price table); proposal, Non-goals.

#### Scenario: The four kinds

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting --json` runs
- **THEN** the entry of `stages` for `code`, round 1, has `role` `developer`, `input` 20,000, `cache_write` 30,000, `cache_read` 140,000, `output` 10,000 and `total` 200,000

#### Scenario: No money

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting` runs
- **THEN** the output holds no currency sign and no price

### Requirement: Flow tokens

`kitsu tokens` SHALL split the tokens of a task in two. Writing tokens are those of `spec-writer`, `architect`, `quality-engineer` and `developer` in round 1 of their stage. Flow tokens are all the others, reported as `coordination` (the session itself), `review`, `lookup`, `git` and `rework` (a later round of a writing role). It SHALL print the flow tokens as a share of the total, in whole per cent.

Source: the owner's statement (the tokens used extra because of the flow); review finding H7 (4 to 27 role starts per task and no measure of what they cost); proposal, Open Question 9.

#### Scenario: The split of one task

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting --json` runs
- **THEN** `writing` is 430,000, `flow` is 225,000 with `coordination` 40,000, `review` 130,000, `lookup` 10,000, `git` 5,000 and `rework` 40,000, and `flow_share` is 34

#### Scenario: The report in words

- **WHEN** `S1` is recorded and `kitsu tokens --task add-greeting` runs
- **THEN** the output says that 225,000 of 655,000 tokens, 34%, went to the workflow and not to the first writing of the specification, the design, the tests and the code

### Requirement: Session tokens

The tokens of the developer's own session SHALL be counted as `coordination`, once for the session. When the session ran one task, they SHALL be part of that task's total. When it ran more than one, `kitsu tokens` SHALL give them under the session, name the tasks that share them, and add them to the total of none.

Source: Claude Code documentation, `hooks.md` (`transcript_path` is the main session's transcript, for the whole session); proposal, Known limits.

#### Scenario: Two tasks in one session

- **WHEN** the test data of `S1` holds a second task, `fix-typo`, with one `developer` start of 15,000 tokens, `S1` is recorded, and `kitsu tokens --json` runs
- **THEN** the 40,000 tokens of the session are under `sessions` with both task names, and the `total` of `add-greeting` is 615,000

### Requirement: Unknown is not zero

When Kitsu cannot read a count it expects in the host's record, it SHALL name the record and mark the affected sums incomplete. It SHALL report an earlier measurement of the same source as a lower bound, or `unknown` if it has no measurement. It SHALL print no sum that leaves a start out without saying so.

Source: review finding M11 (unknown usage was treated as none); Claude Code documentation, `claude-directory.md` (it says where the transcripts are and does not describe a line).

#### Scenario: A record that cannot be read

- **WHEN** the record of the `architect` start in the test data is replaced by an empty file, `S1` is recorded, and `kitsu tokens --task add-greeting --json` runs
- **THEN** the entry for `architect` has `total` `unknown`, `complete` is `false`, and `total` is 575,000

#### Scenario: The report says that it is incomplete

- **WHEN** the record of the `architect` start in the test data is replaced by an empty file, `S1` is recorded, and `kitsu tokens --task add-greeting` runs
- **THEN** the output says that the tokens of one start are unknown and that every sum is at least the number shown

### Requirement: Local and without text

A token record SHALL hold counts, model names, stages, rounds and task names, and no text of a brief, a prompt or an answer. `kitsu tokens` and `kitsu tokens record` SHALL open no network connection.

Source: review finding H5 (repository details were sent to third parties); the first change's proposal, What Changes (the core uses no network).

#### Scenario: No text is kept

- **WHEN** `S1` is recorded and the files under `kitsu/` in the common git directory are searched for a sentence of the `developer` brief
- **THEN** it is not found

### Requirement: Both hosts

On Codex `kitsu tokens` SHALL report the same fields from the record Codex keeps of a session. Where that record holds no count for a start, the report SHALL say `unknown` for it. When a running total of Codex falls, `kitsu` SHALL add the total before the fall to the count and go on from the smaller total.

Source: `internal/usagelog/codex.go` (reads a Codex rollout today, and notes that a rollout can hold token counts without the entries that say which request used them); review finding M11; proposal, Open Question 12; Codex source, `fill_to_context_window` (after a context window overflows, the running total is set to a value with no input and no output).

#### Scenario: A Codex session

- **WHEN** `S2` is recorded and `kitsu tokens --task fix-empty-app --json` runs
- **THEN** `roles` holds an entry for `quality-engineer`, `developer`, `reviewer` and `git`, each with a number or `unknown` as its `total`, and `complete` is `false` when one of them is `unknown`

#### Scenario: A Codex total that is reset

- **WHEN** a Codex record that holds a running total of 95 tokens, then a total of 0, then a total of 14 is read
- **THEN** its count is 109 tokens

### Requirement: Published comparison

Each release SHALL publish, for one fixed set of tasks, the tokens the workflow used and the tokens one agent on the model of the `developer` role used for the same tasks without roles, and for both how many tasks passed the same acceptance tests, which neither arm saw. The documents SHALL state a benefit of the workflow only with these numbers beside it.

Source: the owner's statement (the tokens used extra because of the flow); review finding H7 (evaluations ran once per case, with no baseline); proposal, Success measure 7 and Open Question 11.

#### Scenario: The numbers of a release

- **WHEN** the documents of a release are read
- **THEN** one table gives, for each task of the set and for both arms, the tokens and whether the acceptance tests passed, and the sum of the difference in tokens

#### Scenario: A claim without numbers

- **WHEN** `README.md` says that the workflow improves quality and the table of the release is missing
- **THEN** a test of the documents fails
