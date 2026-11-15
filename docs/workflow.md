# The task workflow

The task workflow takes one task from the developer's words to a gated commit through the seven
[roles](roles.md). It has two parts:

- **Instructions for a model.** The coordinator's text (`gatecub roles text coordinator`, the skill
  `/gatecub:task` on Claude Code) and the seven role texts say who does what and in which
  order. A model follows them or does not; `gatecub` does not run the steps.
- **Commands that check files.** `gatecub change status` and `gatecub change check` read the record of a
  task under `openspec/changes/<task>/`, `gatecub roles check` compares what a role changed with its
  limit, and the gate decides about commits. [What gatecub checks](#what-gatecub-checks) lists exactly what
  is enforced.

A model has run the skill `task` in three sessions only, on two small tasks in scratch
repositories, and it started roles in two of them; see
[what is not known](roles.md#what-is-not-known). Whether the workflow gives better results than
one agent working alone is not measured; see [the evaluation](evaluation.md).

## The flow

This section restates the coordinator's instructions and the task-flow specification. Every step
here is an instruction to the coordinator or to a role. `gatecub` enforces only what the next section,
[What gatecub checks](#what-gatecub-checks), lists.

<img src="diagrams/task-workflow.svg" alt="The seven roles of a task. On the full path only: spec-writer writes the specification and architect the design and the tasks; the reviewer reviews each, then you approve. On both paths, and the small path starts here: quality-engineer writes the tests first, then gatecub run runs your checks; developer writes the code, the reviewer reviews it and findings go back; git makes the commit, and the gate allows or denies it. At any stage: lookup finds files and facts, and gatecub roles check runs after every role." width="100%">

The picture holds the order of the roles and, under each role, the main step that follows its work.
It leaves out `gatecub change check` after `spec-writer` and `architect`, and `gatecub run` after each start
of `developer`. The text below holds every step.

### Before the first role

1. The coordinator runs `gatecub status`. When the repository root holds no `gatecub.toml`, nothing can be
   verified yet. `lookup` finds the commands the repository already uses to test and to lint, the
   coordinator proposes them as checks, and after the developer's yes it runs `gatecub init` and one
   `gatecub add <id> -- <your test command>` for each. The coordinator runs each proposed command once
   before it becomes a check. In a repository with no code, the design names
   the checks, and they are declared before the first commit that holds code. No work is called
   verified while no check is declared.
2. `gatecub-roles.toml` must name the test paths under `[paths]`. When it does not, `lookup` finds
   where the tests live, and the coordinator writes the file after the developer's yes.
3. When `git status --porcelain` prints anything, the coordinator shows it and asks the developer
   to commit it or set it aside before any role starts.
4. For a task that already has a change folder, the coordinator runs `gatecub change status <task>`
   and continues at the stage it prints: the first one that is not finished. A finished stage is
   not repeated, and `commit` means everything before it is finished. The tasks are ticked after
   the review of the code, so `code` is printed until then.

### Two paths

- **Full path**: the task needs a requirement written or changed: new behaviour, or a change to
  behaviour that is specified or relied on. It writes a change folder.
- **Small path**: the intended behaviour is already clear and does not change: a defect against
  stated behaviour, a refactoring, a change of tests only. It writes no change folder.

Size does not choose the path.

A change of a few lines with a plain cause, which one test can pin down, needs neither path. The
coordinator says so and offers to make the change directly, as the skill
[`gatecub`](../skills/gatecub/SKILL.md) describes: the test, the change, `gatecub run`, the commit. It takes a
path for such a change only when the developer asks.

Before any role writes, the coordinator tells the developer which path it takes and why, and
follows the developer when the developer chooses the other one.

### The full path

| Stage | Role that writes | Then |
| --- | --- | --- |
| spec | `lookup` finds the specifications the repository holds; `spec-writer` writes `proposal.md` and `specs/` | the developer answers the open questions and `spec-writer` writes the answers in; `reviewer` reviews; the developer approves the specification |
| design | `architect` writes `design.md` and `tasks.md` | `reviewer` reviews; the developer approves the design |
| tests | `quality-engineer` writes the tests and `tests.md`, and changes or removes the existing tests that no longer hold | the coordinator runs `gatecub run` |
| code | `developer` writes the code, one group of `tasks.md` per start, each labelled `code <round>` | the coordinator runs `gatecub run` after each start |
| review | `reviewer` reviews the code | findings go back to `developer`; when no finding is open, the coordinator ticks the finished tasks in `tasks.md` |
| commit | `git` commits | `gatecub change status <task>` prints `done` |

When `tasks.md` has one group and no role is started a second time for a stage, the roles other
than `lookup` start in this order: `spec-writer`, `reviewer`, `architect`, `reviewer`,
`quality-engineer`, `developer`, `reviewer`, `git`. That is eight starts. `lookup` may be started
at any stage.

When `developer` reports that a test is wrong or must change, the coordinator starts
`quality-engineer` with that report, as stage `tests` with the next round, before `developer`
continues. `quality-engineer` is the only role that changes or removes an existing test.

### The small path

Tests by `quality-engineer`, code by `developer`, review by `reviewer`, commit by `git`: four
starts when the review returns no finding.

- For a defect, the new test must fail before the code changes. The coordinator runs `gatecub run` to
  see it fail before `developer` starts.
- For a refactoring, the tests must pass before and after.
- For a change of tests only, `developer` is not started.

The coordinator still gives the task a name of the same form as a change folder's, because the
[label of each brief](tokens.md#the-label) needs one. No change folder is written, so a small-path
task cannot be continued from files: when the coordinator stops one, it tells the developer the
stage, the round and the open findings.

### Starting a role

- Each role starts as a new worker with none of the coordinator's conversation, on the model and
  effort `gatecub roles` prints, given on the call that starts it. When a host offers to copy or fork
  the conversation into a new agent, the coordinator does not use it: the copy would be counted
  twice, and the label would not be the first message.
- On some providers a model alias resolves to an older model that takes no effort or a lower one,
  and the host then lowers the effort without saying so; `gatecub roles` prints what is asked for, not
  what the provider ran.
- When the host cannot run the role on that model, the coordinator stops before the role writes
  anything, tells the developer the role and the model, and waits.
- The developer's own instruction about models comes first. When it names a model for agents, the
  coordinator uses it and says once which roles it changes.
- The first line of every brief is `gatecub-task: <task> <stage> <round>`.
- The brief names the files to read and what to hand back. A brief for `reviewer` holds no opinion,
  summary or excuse from the role that wrote the work.

### After every role

1. Before the role starts, the coordinator notes the `tree` that `gatecub status --json` prints.
2. When the role ends, it runs `gatecub roles check <role> --from <that tree>`, with `--task <task>` on
   the full path.
3. On exit 5 it starts the same role again with the paths the command listed. It neither accepts
   the work nor repairs it. When the same role exceeds its limit a second time, it stops and asks
   the developer. When a role that writes nothing left a file, it asks the developer to remove
   the file, or removes it after their yes; the file is never in the commit.
4. After `spec-writer` and after `architect`, it runs `gatecub change check <task>`. On exit 5 it
   starts the same role again with the output, before any review.

### Open questions and approvals

- A role writes a question it cannot answer from its sources into the record, and does not guess.
- When `proposal.md` holds open questions, the coordinator puts them to the developer first and
  starts `spec-writer` again with the answers. `reviewer` reviews only a specification without
  open questions; then the developer approves it.
- The coordinator puts every open question of a stage to the developer before it asks for the
  approval of that stage.
- `architect` does not start before the developer has approved the specification, nor
  `quality-engineer` before the developer has approved the design.
- Nothing is pushed and no pull request is opened until the developer says so.
- The coordinator records each approval in `reviews.md`, with its date and the content id that
  `gatecub change status` prints for that stage.

### Review rounds

- Every finding goes back to the role that wrote the work, in a new start with the next round
  number, and the changed work is reviewed again.
- A stage has at most two review rounds. When the second review still has a `blocker` or `major`
  finding open, the coordinator stops and asks the developer how to go on.
- `reviewer` never changes the work, and neither does the coordinator.
- Each finding ends as `resolved`, `rejected` or `deferred`. `rejected` and `deferred` need a
  reason. A `blocker` or `major` finding is never deferred.
- Only the developer rejects a `blocker` or `major` finding, and the coordinator writes their words
  in Reason. The coordinator may reject a `minor` finding with a reason.

### Tests before code

On both paths the tests stage ends before `developer` starts, apart from the interface described
below. Between the two the coordinator runs `gatecub run`. For new behaviour and for a defect it is expected to fail, because the behaviour is not
there yet; for a refactoring the tests must pass before and after. When the new tests of new
behaviour or of a defect pass, the coordinator tells the developer that they did not fail without
the code.

In a language that compiles, the expected failure is that the names of the design's interface are
missing; `quality-engineer` says which names are missing and that nothing else fails. When the
tests must be seen failing on behaviour, the first group of `tasks.md` is that interface with
empty bodies, and `developer` builds it before the tests stage.

### Commits through the gate

`git` makes every commit of the task after `gatecub run`, through the hooks, and no command of the
session passes `--no-verify`. On the full path one commit holds the change folder, the tests and
the code.

### Done means verified

The coordinator calls a task done only when all of these hold:

1. `gatecub status` exits 0.
2. The commit of the task exists.
3. On the full path, `gatecub change check <task>` exits 0 and `gatecub change status <task>` prints `done`.

Whatever a role says about a result that no run of `gatecub` shows is reported as that role's claim.
When a check still fails, the task is not done, and the coordinator names the check. At the end it
runs `gatecub tokens --task <task>` and gives the developer the totals.

### No state outside the record

The workflow keeps no notes, locks or state of a task anywhere but in its change folder and in
git. Another session can continue from these alone. An unfinished or abandoned change folder
blocks nothing: the gate does not read change folders unless a repository declares
`gatecub change check` as a check.

## What gatecub checks

| What | Checked by | When |
| --- | --- | --- |
| A commit or a push holds content with a passing run of every declared check | the git hooks that `gatecub init` installs | at every commit and push in a clone where `gatecub init` ran, unless `--no-verify` skips the hooks |
| A role changed only the paths of its limit | `gatecub roles check` | only when someone runs it; it reports and undoes nothing |
| The files of a change folder obey their rules | `gatecub change check` | when someone runs it, or at every commit when `gatecub.toml` declares it as a check |
| The stage of a task, and whether a review or an approval is of the current content | `gatecub change status` | when someone runs it; it reports and changes nothing |
| A git call that skips the hooks, or an edit of `gatecub.toml`, a hook file or a record of `gatecub` | the plugin's `PreToolUse` hook | on Claude Code it asks first; on Codex it refuses (see [hooks](hooks.md)) |

What no program checks:

- Which path was chosen, and whether the developer agreed.
- Whether the developer gave an approval. `gatecub change status` checks that an approval row exists
  and that its content id is current; the coordinator writes that row.
- The order of the roles, the number of review rounds, whether the developer or the coordinator
  rejected a finding, and whether a reviewer's brief held the author's opinion.
- Whether the tests ran and failed before the code was written. `gatecub run` records each run, and
  nothing compares the order.
- Which model a role ran on. [`gatecub tokens`](tokens.md) shows the model names the host's record
  gives.
- On Claude Code, the host applies the `tools` list of each role file. `gatecub` does not.

## The record of a task

On the full path the record of a task is the folder `openspec/changes/<task>/`. `<task>` is
lower-case words joined by hyphens. `gatecub` reads the folder itself and needs no other program.

| File | Written by | Format |
| --- | --- | --- |
| `proposal.md` | `spec-writer` | Free text: why, what changes, what is not part of it, and `## Open questions`. |
| `specs/<capability>/spec.md` | `spec-writer` | `## ADDED Requirements`, `## MODIFIED Requirements` or `## REMOVED Requirements`; under them `### Requirement: <name>`, a text with `SHALL` or `MUST`, a `Source:` line, and `#### Scenario: <name>` with one `- **WHEN**` line and one `- **THEN**` line. |
| `design.md` | `architect` | Free text, plus a table under `## Building blocks` with the columns Block, Responsibility, Depends on, New, Reuse considered, and a table under `## Rules` with the columns Rule, Checked by. |
| `tasks.md` | `architect`; the coordinator ticks the boxes after the review of the code has no open finding | `- [ ] N.M <text>. Size: S\|M\|L. Depends on: <ids or none>. Check: <...>. Source: <...>.` under `## N. <group>`. |
| `tests.md` | `quality-engineer` | A table with the columns Scenario, Test file, Text. A row that is not automated holds `not automated` as its file and a reason as its text. Under `## Changed tests`, one line for each existing test it changed or removed, with the requirement or the reason; `gatecub` does not read it. |
| `reviews.md` | the coordinator | Under `## Reviews` the columns Stage, Round, Date, Content; under `## Findings` the columns Id, Stage, Round, Severity, Where, Finding, Disposition, Reason; under `## Approvals` the columns What, Date, Content. |

How `gatecub` reads these files:

- A line inside a fenced code block is not read, so an example in a file is never taken for
  content. Line ends of any system are read alike.
- A cell may stand in backticks. `\|` is a bar inside a cell. A table is found by the `##` heading
  above it; the table of `tests.md` is the first table with a column Scenario. Column names are
  matched without regard to case.
- `SHALL` or `MUST` counts in the text of a requirement above its first scenario. A `Source:` line
  counts anywhere in the requirement.
- A task may go on in indented lines. A task names a requirement when its text holds the name.
- In `design.md`, New is `yes` or `no`; Depends on and Checked by hold names separated by commas,
  and `none`, `nothing` or `-` mean none. A dependency on a name the table does not hold is not
  followed. Checked by is `review` or the id of a check of `gatecub.toml`.
- The test file of `tests.md` is a path from the root of the repository.
- In `reviews.md`, Stage is `spec`, `design` or `code`; What is `spec` or `design`; Round is a
  number from 1; Severity is `blocker`, `major` or `minor`; Disposition is `open`, `resolved`,
  `rejected` or `deferred`. Content is the content id that `gatecub change status` printed for that
  stage when the review or the approval was given.
- A file that is missing breaks no rule. `gatecub change status` names it as missing at its stage.

### An example folder

A complete folder for the task `add-greeting`, at the stage `done`. It assumes that `gatecub.toml`
declares a check with the id `test`, and that `tests/greeting_test.txt` exists and holds the
texts `greets_by_name` and `greets_without_name`. With these six files committed together with
the code, `gatecub change check` exited 0 and `gatecub change status add-greeting` printed `done` in a trial
repository. The content ids in `reviews.md` are the ones that trial printed: the `code` id depends
on every file of the repository outside `openspec/changes/`, so in your repository write the ids
your `gatecub change status add-greeting` prints.

`openspec/changes/add-greeting/proposal.md`:

```markdown
# Add a greeting

## Why

The program prints nothing when it starts. The request asks it to greet the user by name.

## What changes

- The program prints `Hello, <name>.` when it is given a name, and `Hello.` when it is not.

## Not part of this change

- Greetings in other languages.

## Open questions

None.
```

`openspec/changes/add-greeting/specs/greeting/spec.md`:

```markdown
# greeting

## ADDED Requirements

### Requirement: Greeting text

The program SHALL print `Hello, <name>.` when it is given a name, and `Hello.` when it is not.

Source: the request of 14 November 2026, "greet the user by name".

#### Scenario: A name is given

- **WHEN** the program runs with the name `Ann`
- **THEN** it prints `Hello, Ann.`

#### Scenario: No name is given

- **WHEN** the program runs with no name
- **THEN** it prints `Hello.`
```

`openspec/changes/add-greeting/design.md`:

```markdown
# Design: add-greeting

One new block makes the text; the existing entry point prints it.

## Building blocks

| Block | Responsibility | Depends on | New | Reuse considered |
| --- | --- | --- | --- | --- |
| `greeter` | Makes the greeting text from an optional name | none | yes | `src/format`: it formats numbers only |
| `app` | Prints what `greeter` returns | `greeter` | no | |

## Rules

| Rule | Checked by |
| --- | --- |
| `greeter` reads no file and prints nothing | review |
| Both scenarios have a passing test | test |
```

`openspec/changes/add-greeting/tasks.md`:

```markdown
# Tasks: add-greeting

## 1. Greeting

- [x] 1.1 Build `greeter`. Size: S. Depends on: none. Check: the tests of both scenarios pass. Source: Greeting text.
- [x] 1.2 Print the greeting from `app`. Size: S. Depends on: 1.1. Check: `gatecub run` passes. Source: Greeting text.
```

`openspec/changes/add-greeting/tests.md`:

```markdown
# Tests: add-greeting

| Scenario | Test file | Text |
| --- | --- | --- |
| A name is given | `tests/greeting_test.txt` | `greets_by_name` |
| No name is given | `tests/greeting_test.txt` | `greets_without_name` |
```

`openspec/changes/add-greeting/reviews.md`:

```markdown
# Reviews: add-greeting

## Reviews

| Stage | Round | Date | Content |
| --- | --- | --- | --- |
| spec | 1 | 2026-11-14 | aec69cbdb37f |
| design | 1 | 2026-11-14 | f709ae597d40 |
| code | 1 | 2026-11-14 | 011f904865b6 |

## Findings

| Id | Stage | Round | Severity | Where | Finding | Disposition | Reason |
| --- | --- | --- | --- | --- | --- | --- | --- |
| F1 | spec | 1 | minor | specs/greeting/spec.md:7 | The text does not say what a name may hold | rejected | Any text is a name; the request sets no limit |

## Approvals

| What | Date | Content |
| --- | --- | --- |
| spec | 2026-11-14 | aec69cbdb37f |
| design | 2026-11-14 | f709ae597d40 |
```

## gatecub change status

```text
gatecub change status [<task>] [--json]
```

Prints the stage of a change folder, decided from its files and from git alone. It changes no
file. Without a task it prints one line for each folder under `openspec/changes/`, except
`archive/` and folders whose name starts with a dot.

### Stages

The stage is the first one in this table whose condition does not hold, or `done` when all hold.
The output lists what is missing for that stage.

| Stage | Its condition |
| --- | --- |
| `spec` | `proposal.md` exists; at least one `specs/<capability>/spec.md` exists; the specifications break none of their rules (see [the rules](#the-rules)); `reviews.md` holds a review with Stage `spec` that counts, and an approval `spec` that counts; no finding of the stage `spec` is `open`. |
| `design` | `design.md` and `tasks.md` exist and break none of the rules of the design and the tasks; `reviews.md` holds a review `design` that counts and an approval `design` that counts; no finding of the stage `design` is `open`. |
| `tests` | `tests.md` has a row for every scenario of the requirements under `ADDED` and `MODIFIED`, matched by the exact name. A change without scenarios needs no `tests.md`. |
| `code` | Every task of `tasks.md` is checked. |
| `review` | `reviews.md` holds a review `code` that counts, and no finding of the stage `code` is `open`. |
| `commit` | The folder in the working tree is the folder in `HEAD`. |
| `done` | All of the above hold. |

`gatecub change status` does not apply the rules of `tests.md` and `reviews.md`: a row that names a
test file without its text, or a `major` finding that is `deferred`, does not change the stage.
`gatecub change check` finds those, and `gatecub change status` counts them: it prints
`broken rules: <n>` when there are any, and `broken_rules` in its JSON. A stage says how far the
record is, not that its files are valid. This is why "done" in
[Done means verified](#done-means-verified) needs both commands.

### Content ids

A content id is the first 12 hexadecimal characters of a SHA-256. `gatecub change status` prints three:

| Id | Made from |
| --- | --- |
| `spec` | `proposal.md` and every file under `specs/`, in the order of their paths, each with its path; line ends read as LF. |
| `design` | `design.md` and `tasks.md`, line ends read as LF, every task mark `- [x] ` read as `- [ ] `. Ticking a task does not change this id. |
| `code` | The entries of a snapshot of the working tree, as git lists a tree, for every path outside `openspec/changes/`. Untracked files git does not ignore are part of the snapshot. |

An id is empty, printed as `none`, when none of its files exists. An empty id matches nothing.

### When a review or an approval counts

- A review or an approval counts while its Content is the id of that stage in the working tree.
- A review of the code also counts when `HEAD` holds the change folder exactly as the working tree
  does, and its Content is the `code` id of the last commit that changed the folder. The task was
  committed with the code that was reviewed, and later work is later work: after the task's commit,
  editing and committing other files leaves the task `done`.
- When a review or an approval no longer counts, `gatecub change status` says so and prints the
  current id. For example, after `SHALL` was changed to `MUST` in the approved specification:

  ```text
  add-greeting: spec
    the review and the approval of the specification were given for other content; the content id is now `7a9e0d180351`
  content ids: spec 7a9e0d180351, design 86a687cc7c6d, code 60d50c98aef8
  ```

  And after the code changed while the change folder was not committed:

  ```text
  add-greeting: review
    the review of the code was given for other content; the content id is now `8e3af834419c`
  content ids: spec 93eafcdc213a, design 86a687cc7c6d, code 8e3af834419c
  ```

### Output and exit statuses

With a task, the text output is the stage, each missing item on its own line, the number of
scenarios that are not automated when there is one, and the three ids:

```text
add-greeting: code
  task 1.2 is not checked
content ids: spec 93eafcdc213a, design 86a687cc7c6d, code 60d50c98aef8
```

With `--json` and a task:

```json
{
  "task": "add-greeting",
  "stage": "code",
  "missing": [
    "task 1.2 is not checked"
  ],
  "not_automated": 0,
  "content": {
    "spec": "93eafcdc213a",
    "design": "86a687cc7c6d",
    "code": "60d50c98aef8"
  },
  "broken_rules": 0
}
```

Without a task, the text output is one line per folder, such as
``add-x: spec (3 missing; `gatecub change status add-x` names them)``, and the JSON output is
`{"changes": [...]}` with one object as above for each folder. In JSON an empty id is `""`.

| Exit | When |
| --- | --- |
| 0 | The stage is printed, at any stage. |
| 1 | The named folder does not exist (`gatecub: openspec/changes/no-such-task/ does not exist`), or the directory is not in a git repository. |
| 2 | The command line is wrong: more than one task, or a task name that is not lower-case words joined by hyphens. |

## gatecub change check

```text
gatecub change check [<task>]
```

Checks the files of the named folder, or of every folder under `openspec/changes/` except
`archive/`, against the rules below. A folder at an earlier stage passes when the files it has obey
their rules: a missing file breaks no rule.

### The rules

Specifications, for each `specs/<capability>/spec.md`:

- Two requirements of one file have the same name.
- A requirement under `ADDED` has the name of a requirement that
  `openspec/specs/<capability>/spec.md` holds. It must be under `MODIFIED`.
- A requirement that is not under `REMOVED` holds neither `SHALL` nor `MUST`, has no `Source:`
  line, or has no scenario.
- A scenario of such a requirement does not hold exactly one `- **WHEN**` line and one
  `- **THEN**` line.
- Two scenarios of requirements under `ADDED` or `MODIFIED` have the same name anywhere in
  the change, even across capabilities. Rename the duplicate and update `tests.md`; its rows
  identify scenarios by name. Scenarios under `REMOVED` need no mapping and do not participate.

Tasks, in `tasks.md`:

- The file holds no task of the form `- [ ] 1.1 <text>`.
- A task lacks `Size:`, `Depends on:`, `Check:` or `Source:`.
- A task depends on a task that is not earlier in the list.
- A requirement of the change, under any of the three headings, is named by no task.

Design, in `design.md`:

- There is no table under `## Building blocks` or `## Rules`, or a table lacks one of its columns.
- New holds something other than `yes` or `no`; a block has no responsibility.
- The dependencies of the blocks form a cycle.
- A new block names no existing code under Reuse considered.
- A rule has nothing under Checked by, or names a check that `gatecub.toml` does not declare.

Tests, in `tests.md`:

- There is no table with the column Scenario, or the table lacks Test file or Text.
- A row is `not automated` and gives no reason, or names no test file or no text.
- The test file of a row does not exist, or does not hold the text.
- The test file of a row is absolute or lies outside the repository, also through a symbolic
  link. `gatecub change check` then reads nothing outside the repository.
- Once every task is checked: a scenario of a requirement under `ADDED` or `MODIFIED` has no row,
  or `tests.md` is missing while the change has scenarios.

Reviews, in `reviews.md`:

- A table under `## Reviews`, `## Findings` or `## Approvals` lacks one of its columns.
- A review has a Stage other than `spec`, `design` or `code`, or a Round that is not a number from
  1; an approval has a What other than `spec` or `design`.
- A finding has a Stage, Severity or Disposition outside its values, or a Round that is not a
  number from 1.
- A finding is `rejected` or `deferred` without a reason.
- A finding is `blocker` or `major` and `deferred`.
- Every task is checked and a finding is `open`.

`gatecub change check` does not judge whether a review or an approval is of the current content;
`gatecub change status` does.

### Output and exit statuses

Each broken rule is one line, `file:line: text`, with the file from the root of the repository.
When the rule has no line, the form is `file: text`. A last line gives the count:

```text
openspec/changes/add-greeting/design.md:7: the building blocks depend on each other in a cycle: `greeter`, `app`
broken rules: 1; each line above names the file, the line and the rule
```

A folder that obeys every rule gives `add-greeting: the files obey their rules`.

| Exit | When |
| --- | --- |
| 0 | Every folder obeys its rules, or `openspec/changes/` holds no change folder. |
| 5 | At least one rule is broken. |
| 1 | The named folder does not exist; a file cannot be read; a rule of `design.md` names a check and `gatecub.toml` cannot be read; or the directory is not in a git repository. |
| 2 | The command line is wrong, including `--json`, which this command does not take. |

### As a check in gatecub.toml

`gatecub change check` reads only the files. It takes no snapshot and keeps no state, so a repository
can declare it as a check in `gatecub.toml`, as this repository does:

```toml
[[check]]
id = "record"
run = ["gatecub", "change", "check"]
```

The gate then treats it as any other check: a commit is denied while `record` has no passing run on
the content being committed.

## Existing specifications

The current requirements of a repository live in `openspec/specs/<capability>/spec.md`. Before the
spec stage writes, `lookup` finds the specifications the repository holds there and in every change
folder that is not archived.

- **A new behaviour** goes under `## ADDED Requirements`. `gatecub change check` refuses an `ADDED`
  requirement whose name `openspec/specs/<capability>/spec.md` already holds for that capability.
- **A changed behaviour** goes under `## MODIFIED Requirements`, with the same name and its full
  new text. It obeys the same rules as an added one. No second requirement is written for the same
  behaviour.
- **A removed behaviour** goes under `## REMOVED Requirements`. It needs only its name, and a task
  that names it.
- **A specification in another form**, such as prose in a documents folder, is named in a
  `Source:` line and left unchanged.

## The archive

When the developer says that a finished change has shipped, the coordinator confirms that it
is `done` and `gatecub change check <task>` exits 0. It saves the working-tree id from `gatecub status --json`
with `git update-ref refs/gatecub/finalize/<task> <tree>` before starting `spec-writer` with an explicit
finalization brief. In this mode the role brings only the task's capabilities in `openspec/specs/`
in line with the change: every added requirement present, every modified one replaced, every
removed one gone. The coordinator checks it with:

```sh
gatecub roles check spec-writer --from refs/gatecub/finalize/<task> --task <task> --finalize
```

The allowed paths are exactly `openspec/specs/<capability>/spec.md` for the capability spec files
of this task in the tree given by `--from`. Other current specifications, source files and the
change folder remain outside the limit. Adding a capability during the role cannot expand it.
The ordinary spec-writer limit is unchanged when `--finalize` is absent.

If finalization is interrupted while the change folder is active, reuse the saved Git ref and
rerun that scope check and review. Do not require `done` again: the current specifications may
already contain synchronized requirements. If the folder is already archived, finish the
final validation and commit, or verify that the archive commit already exists. If the original
baseline is missing before archival, stop and ask the developer instead of taking a new
snapshot that would overlook earlier writes.

`reviewer` compares the synchronized specifications with the change, and its ordinary role
check must pass. Then the coordinator moves the folder to `openspec/changes/archive/<task>/`
and runs `gatecub change check`. Do not run the ordinary change check between synchronization and
archive: an `ADDED` requirement is now present in the living specifications, which that check
correctly rejects while the change is active. No program merges the requirements; finalization
is an instruction to the roles and is not part of either path. After the archive is committed
through the gate, delete the saved ref with `git update-ref -d refs/gatecub/finalize/<task>`.

`gatecub change status` and `gatecub change check` skip `openspec/changes/archive/`, so an archived folder
is no longer read.
