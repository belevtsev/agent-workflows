# change-record

## Purpose

Keep everything a task decided in one folder that a person can read and a
program can check, so that the state of a task is what its files say and not
what a session remembers.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` and the `cw-roles.toml` of the `role-definitions`
  specification, and the folder `openspec/changes/add-greeting/` with these
  files:
  - `proposal.md`.
  - `specs/greeting/spec.md` with one requirement under
    `## ADDED Requirements`, `Greeting text`. It holds `SHALL`, a `Source:`
    line and two scenarios, `A name is given` and `No name is given`.
  - `design.md` with two building blocks: `greeter`, new, which depends on
    nothing and names `src/app.txt` as the code considered for reuse; and
    `app`, existing, which depends on `greeter`. It has one rule, checked by
    `review`.
  - `tasks.md` with tasks 1.1 and 1.2, both checked. Each has `Size:`,
    `Depends on:`, `Check:` and `Source:`, and 1.1 names `Greeting text`.
  - `tests.md` with one row per scenario. Both name `tests/app_test.txt`,
    with the texts `greets_by_name` and `greets_without_name`, and that file
    holds both texts.
  - `reviews.md` with the reviews `spec`, `design` and `code`, round 1 each;
    one finding, `minor` and `resolved`; and the approvals `spec` and
    `design`. Each review and each approval holds the content id that
    `cw change status add-greeting` prints for it.
- The folder and `tests/app_test.txt` are committed.
- In this state `cw change check` exits 0 and `cw change status` prints
  `done`.

## ADDED Requirements

### Requirement: Folder

A task on the full path SHALL be recorded in `openspec/changes/<task>/` as `proposal.md`, `specs/<capability>/spec.md`, `design.md`, `tasks.md`, `tests.md` and `reviews.md`. `<task>` SHALL be lower-case words joined by hyphens. `cw` SHALL read the folder itself and SHALL need no other program for it.

Source: `skills/task-orchestration/references/contracts/spec-writer.md` (an OpenSpec change folder); this repository's `openspec/changes/reduce-cw-to-evidence-gate/`; proposal, Open Question 5.

#### Scenario: The folder is found

- **WHEN** `cw change status` runs in `demo`
- **THEN** it prints one line for `add-greeting` and exits 0

#### Scenario: A task that does not exist

- **WHEN** `cw change status no-such-task` runs
- **THEN** it exits 1 and says that `openspec/changes/no-such-task/` does not exist

### Requirement: Stage from the files

`cw change status [<task>] [--json]` SHALL print the stage of a change folder, decided from its files alone: the first stage of the table below whose condition does not hold, with what is missing, or `done`. It SHALL change nothing.

| Stage | Its condition |
| --- | --- |
| `spec` | The specifications obey their rules, `reviews.md` holds a review of the specification with no `open` finding, and the approval of the specification is recorded. |
| `design` | `design.md` and `tasks.md` obey their rules, `reviews.md` holds a review of the design with no `open` finding, and the approval of the design is recorded. |
| `tests` | `tests.md` has a row for every scenario. |
| `code` | Every task is checked. |
| `review` | `reviews.md` holds a review of the code with no `open` finding. |
| `commit` | The folder in the working tree is the folder in `HEAD`. |

Source: review findings H2 and H9 (a task's state lives in a store that can block and go stale); `task-flow`: No state outside the record.

#### Scenario: The design is missing

- **WHEN** `design.md` and `tasks.md` are removed and `cw change status add-greeting` runs
- **THEN** it prints `design` and names `design.md` as missing

#### Scenario: A finding is still open

- **WHEN** a finding of the code review is added to `reviews.md` with the disposition `open`, and `cw change status add-greeting` runs
- **THEN** it prints `review` and names that finding

#### Scenario: A task is not checked

- **WHEN** the mark of task 1.2 in `tasks.md` is changed from `[x]` to `[ ]` and `cw change status add-greeting --json` runs
- **THEN** `stage` is `code` and `missing` names task 1.2

#### Scenario: The record is not committed

- **WHEN** an empty line is added at the end of `reviews.md` and `cw change status add-greeting` runs
- **THEN** it prints `commit` and says that the folder differs from `HEAD`

#### Scenario: Reading changes nothing

- **WHEN** `cw change status` runs
- **THEN** `cw status --json` prints the same `tree` as before

### Requirement: Specification rules

`cw change check` SHALL fail when a requirement holds neither `SHALL` nor `MUST`, has no `Source:` line or has no scenario; when a scenario does not hold exactly one `WHEN` and one `THEN`; when two requirements of one capability have the same name; or when a requirement under `ADDED` has the name of a requirement that `openspec/specs/` holds for that capability.

Source: `skills/task-orchestration/references/contracts/spec-writer.md` and `spec-review.md` (every requirement traces to a source and has a scenario); `task-flow`: Existing specifications.

#### Scenario: A requirement without a scenario

- **WHEN** both scenarios of `Greeting text` are removed and `cw change check` runs
- **THEN** it exits 5, names `specs/greeting/spec.md` and the line of the requirement, and says that it has no scenario

#### Scenario: A requirement that exists already

- **WHEN** `openspec/specs/greeting/spec.md` is created with a requirement `Greeting text` and `cw change check` runs
- **THEN** it exits 5 and says that `Greeting text` exists and must be under `MODIFIED`

### Requirement: Task rules

`cw change check` SHALL fail when a task lacks `Size:`, `Depends on:`, `Check:` or `Source:`; when it depends on a task that is not earlier in the list; or when a requirement of the change is named by no task.

Source: `skills/task-orchestration/references/contracts/spec-writer.md` (each task names its size, the tasks it depends on and its check, and traces to a source); this repository's `openspec/changes/reduce-cw-to-evidence-gate/tasks.md`.

#### Scenario: A requirement that no task builds

- **WHEN** the name `Greeting text` is removed from task 1.1 and `cw change check` runs
- **THEN** it exits 5 and says that no task names `Greeting text`

#### Scenario: A task without a check

- **WHEN** `Check:` and its text are removed from task 1.2 and `cw change check` runs
- **THEN** it exits 5 and names `tasks.md`, the line of task 1.2, and `Check:`

### Requirement: Test table

`tests.md` SHALL hold one row per scenario, with the file of its test and a text that occurs in that file, or with `not automated` and a reason. `cw change check` SHALL fail when a row names a file that does not exist or does not hold the text, and, once every task is checked, when a scenario has no row. `cw change status` SHALL print the number of scenarios that are not automated.

Source: the owner's statement of 14 November 2026 (well tested code, in any language); review finding H8 (tests that miss what they claim to cover); proposal, Known limits.

#### Scenario: A test is renamed

- **WHEN** the text `greets_by_name` in `tests/app_test.txt` is changed to `greets_name` and `cw change check` runs
- **THEN** it exits 5 and names the scenario `A name is given` and the file `tests/app_test.txt`

#### Scenario: A scenario without a test, when the work is called finished

- **WHEN** the row of `No name is given` is removed from `tests.md` and `cw change check` runs
- **THEN** it exits 5 and names the scenario `No name is given`

#### Scenario: A scenario without a test, while the work goes on

- **WHEN** the row of `No name is given` is removed from `tests.md`, the mark of task 1.2 is changed to `[ ]`, and `cw change check` runs
- **THEN** it exits 0

#### Scenario: A scenario that is not automated

- **WHEN** the row of `No name is given` is changed to `not automated` with a reason, the change is committed, and `cw change status add-greeting` runs
- **THEN** it prints `done` and says that one scenario is not automated

### Requirement: Design rules

`design.md` SHALL hold a table of building blocks, each with its one responsibility, the blocks it depends on, and whether it is new, and a table of rules, each with how it is checked. `cw change check` SHALL fail when the dependencies form a cycle, when a new block names no existing code that was considered for reuse, or when a rule names a check that `cw.toml` does not declare.

Source: the owner's statement (clean architecture; reusable building blocks); `skills/task-orchestration/references/contracts/architect.md` (a decision record, an interface contract and an implementation brief); the first change's `check-declaration`: Check definition.

#### Scenario: Two blocks depend on each other

- **WHEN** `greeter` is changed to depend on `app` and `cw change check` runs
- **THEN** it exits 5 and names the cycle `greeter`, `app`

#### Scenario: A new block without a look at what exists

- **WHEN** the entry of `greeter` for the code considered for reuse is emptied and `cw change check` runs
- **THEN** it exits 5 and names `greeter`

#### Scenario: A rule that a check enforces

- **WHEN** the rule of `design.md` is changed to be checked by `lint` and `cw change check` runs
- **THEN** it exits 0

#### Scenario: A rule that names a check nobody declared

- **WHEN** the rule of `design.md` is changed to be checked by `layers` and `cw change check` runs
- **THEN** it exits 5 and says that `cw.toml` declares no check `layers`

### Requirement: Review records

`reviews.md` SHALL hold every finding with its stage (`spec`, `design` or `code`), round, severity (`blocker`, `major` or `minor`), place and disposition: `open`, `resolved`, `rejected` or `deferred`. `cw change check` SHALL fail when a row holds another value, when a `rejected` or `deferred` finding has no reason, when a `blocker` or `major` finding is `deferred`, or when every task is checked and a finding is `open`.

Source: `skills/task-orchestration/references/contracts/README.md` and `code-review.md` (findings with the severities `blocker` and `major`); `reports/Review findings 2026-11-14.md` (each finding has a status, and rejected ones a reason).

#### Scenario: A major finding is put off

- **WHEN** a finding with severity `major` and disposition `deferred` is added to `reviews.md` and `cw change check` runs
- **THEN** it exits 5 and names that finding

#### Scenario: A finding is rejected without a reason

- **WHEN** the finding of the fixture is changed to `rejected` with an empty reason and `cw change check` runs
- **THEN** it exits 5 and names that finding

#### Scenario: Finished work with an open finding

- **WHEN** a finding with severity `minor` and disposition `open` is added to `reviews.md` and `cw change check` runs
- **THEN** it exits 5 and names that finding

#### Scenario: A stage that does not exist

- **WHEN** the stage of the fixture's finding is changed to `final` and `cw change check` runs
- **THEN** it exits 5 and names `reviews.md`, the line of that finding, and `final`

### Requirement: Reviewed content

Each review and each approval in `reviews.md` SHALL hold the content id that `cw change status` printed for it: for `spec` the id of `proposal.md` and `specs/`, for `design` the id of `design.md` and `tasks.md` without their check marks, for `code` the id of every path outside `openspec/changes/`. A review or an approval SHALL count only while its id is that of the working tree or, for `code` when `HEAD` holds the folder as it is, that of the last commit that changed the folder.

Source: production plan, Independent challenge, finding 9 (an approval stayed valid after the approved file changed).

#### Scenario: The specification changes after its approval

- **WHEN** the word `SHALL` of `Greeting text` is changed to `MUST` in `specs/greeting/spec.md` and `cw change status add-greeting` runs
- **THEN** it prints `spec` and says that the review and the approval of the specification were given for other content

#### Scenario: The code changes after its review

- **WHEN** an empty line is added at the end of `reviews.md`, `src/app.txt` is edited, and `cw change status add-greeting` runs
- **THEN** it prints `review` and says that the review of the code was given for other content

#### Scenario: A checked task does not undo the approval of the design

- **WHEN** the mark of task 1.2 in `tasks.md` is changed from `[x]` to `[ ]` and `cw change status add-greeting` runs
- **THEN** it prints `code`, and names neither the review nor the approval of the design

#### Scenario: Later work leaves a finished task done

- **WHEN** `README.md` is edited and committed after the fixture's commit, `src/app.txt` is edited and not committed, and `cw change status add-greeting` runs
- **THEN** it prints `done`

### Requirement: Usable as a check

`cw change check [<task>]` SHALL read the named folder, or every folder under `openspec/changes/` except `archive/`. It SHALL exit 0 when each obeys the rules, and otherwise exit 5 and name the file, the line where the broken rule has one, and the rule. A folder at an earlier stage SHALL pass when the files it has obey their rules. A repository MAY declare the command as a check in `cw.toml`; the gate then treats it as any other check.

Source: the first change's proposal, What Changes (a check is any program); `gate-reporting`: Exit statuses.

#### Scenario: A folder at the first stage

- **WHEN** `design.md`, `tasks.md`, `tests.md` and `reviews.md` are removed and `cw change check` runs
- **THEN** it exits 0

#### Scenario: The gate enforces the record

- **WHEN** `cw.toml` declares a check `record` with `run = ["cw", "change", "check"]`, a finding with severity `minor` and disposition `open` is added to `reviews.md`, `cw run` has ended, and `git commit -a -m change` runs
- **THEN** the commit is denied and the output names the check `record`
