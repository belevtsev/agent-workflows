# task-flow

## Purpose

Take one task from the developer's words to a gated commit through fixed
stages, with the developer deciding at the points that matter and no role
approving its own work.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` and the `cw-roles.toml` of the `role-definitions`
  specification, with the plugin installed and the gate enforcing.
- "The record" of a task is its change folder (`change-record`).
- "A role is started" means the coordinator starts it as `role-definitions`,
  Explicit start, says.
- A scenario marked "(evaluation)" needs a model. It is run by the evaluation
  of the proposal's Success measure 1 and judged from the host's record of the
  session and from the repository afterwards, not by CI.

## ADDED Requirements

### Requirement: Entry

The workflow SHALL start from a request in the developer's words, from documents the developer names, or from an existing change folder. For an existing folder it SHALL continue at the first stage that is not finished, which `cw change status` names, and SHALL NOT repeat a finished stage. When `git status --porcelain` prints anything, the coordinator SHALL ask the developer to commit it or set it aside before any role starts.

Source: the owner's statement of 14 November 2026 (implement a task from scratch by creating a specification or working with existing ones); `skills/task-orchestration/references/contracts/README.md` (the spec stage is skipped when a reviewed spec exists); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: A folder with an approved specification (evaluation)

- **WHEN** `cw change status add-greeting` prints `design` and the developer asks to continue `add-greeting`
- **THEN** the first role started is `architect`, and `proposal.md` and `specs/` are unchanged at the end

### Requirement: Two paths

The coordinator SHALL take the full path when the task needs a requirement written or changed, and the small path when the intended behaviour is already clear and does not change; size SHALL NOT choose the path. Before any role writes, it SHALL tell the developer the path and the reason, and SHALL follow the developer's choice. For a change of a few lines with a plain cause, which one test can pin down, it SHALL say that the task needs no roles and SHALL take a path only when the developer asks.

Source: `skills/task-orchestration/references/contracts/README.md` (the spec stage is skipped for a bug fix, a refactor or a test-only task); review finding H7 (9 to 27 role starts for a strict task); `docs/other-tools.md`, Specifications and roles (users of four tools report too much ceremony for a small change); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: A defect takes the small path (evaluation)

- **WHEN** the developer reports that `unit.sh` accepts an empty `src/app.txt` and asks for a fix
- **THEN** the coordinator names the small path before the first role is started, and no change folder is written

#### Scenario: A change of one line needs no roles (evaluation)

- **WHEN** the developer asks to correct one misspelt word in the message that `unit.sh` prints
- **THEN** the coordinator says that the task needs no roles, and starts no role unless the developer then asks for a path

#### Scenario: New behaviour takes the full path (evaluation)

- **WHEN** the developer asks for a greeting that `src/app.txt` does not hold yet
- **THEN** the coordinator names the full path before the first role is started

### Requirement: Stages of the full path

The full path SHALL run these stages in this order: spec by `spec-writer`, design by `architect`, tests by `quality-engineer`, code by `developer` with one group of `tasks.md` per start, review by `reviewer`, commit by `git`. `reviewer` SHALL review the specification before the design stage starts, and the design before the tests stage starts. The coordinator SHALL tick the finished tasks only when the review of the code has no open finding. `lookup` MAY be started at any stage.

Source: the owner's statement (specification, code, tests, architecture); `skills/task-orchestration/references/contracts/README.md` (spec, design, tests, code and review stages, each reviewed by another worker); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: The order of the roles (evaluation)

- **WHEN** the full path runs for one task, `tasks.md` has one group, and no role is started a second time for a stage
- **THEN** the roles other than `lookup` are started in this order: `spec-writer`, `reviewer`, `architect`, `reviewer`, `quality-engineer`, `developer`, `reviewer`, `git`

### Requirement: Stages of the small path

The small path SHALL run tests by `quality-engineer`, code by `developer`, review by `reviewer` and commit by `git`, and SHALL write no change folder. For a defect the new test SHALL fail before the code changes; for a refactoring the tests SHALL pass before and after; for a change of tests only `developer` SHALL NOT be started. When the coordinator stops a task on this path, it SHALL tell the developer the stage, the round and the open findings.

Source: `skills/task-orchestration/references/contracts/README.md` (the `lite` flow); review finding H7; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: A defect is shown by a test first (evaluation)

- **WHEN** the small path runs for the defect that `unit.sh` accepts an empty `src/app.txt`
- **THEN** `cw run` fails after the tests stage and passes after the code stage, and `openspec/` is unchanged

### Requirement: Existing specifications

Before the spec stage writes, `lookup` SHALL find the specifications the repository holds, in `openspec/specs/` and in every change folder that is not archived. When a current requirement covers the request, `spec-writer` SHALL change it as a `MODIFIED` requirement and SHALL NOT add a second requirement for the same behaviour. A specification in any other form SHALL be named as a source and left unchanged.

Source: the owner's statement (working with existing specifications); proposal, Open Question 5.

#### Scenario: A current requirement is changed, not doubled (evaluation)

- **WHEN** `openspec/specs/greeting/spec.md` holds the requirement `Greeting text` and the developer asks for a different greeting text
- **THEN** the change folder holds `Greeting text` under `## MODIFIED Requirements` and no requirement under `## ADDED Requirements`

#### Scenario: A specification in another form (evaluation)

- **WHEN** `docs/greeting.md` describes the greeting in prose and the developer asks for a change to it
- **THEN** a `Source:` line of the change folder names `docs/greeting.md`, and that file is unchanged at the end

### Requirement: Open questions

A role SHALL write a question it cannot answer from its sources into the record, and SHALL NOT answer it by a guess. The coordinator SHALL put the open questions of `proposal.md` to the developer and start `spec-writer` again with the answers before `reviewer` reviews the specification, and SHALL put every open question of a stage to the developer before that stage is approved.

Source: `skills/task-orchestration/references/contracts/spec-writer.md` (a value the sources do not give is an open question); the owner's statement (clarity); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: A request that leaves a value open (evaluation)

- **WHEN** the developer asks for a greeting and gives no text for it
- **THEN** `proposal.md` lists the text of the greeting as an open question, and no requirement states a text before the developer has answered

### Requirement: Approvals

The workflow SHALL wait for the developer's approval of the specification before the design stage starts, of the design before the tests stage starts, and for the developer's word before a push or a pull request. The coordinator SHALL record each approval in `reviews.md` with its date.

Source: `skills/task-orchestration/references/contracts/README.md` (the user is shown the design and asked the spec's open questions); proposal, Open Question 6.

#### Scenario: No design before the specification is approved (evaluation)

- **WHEN** the full path runs and the developer has not yet answered the request for approval of the specification
- **THEN** `architect` has not been started and `design.md` does not exist

#### Scenario: Nothing is pushed without a word (evaluation)

- **WHEN** the full path ends and the developer has said nothing about a push
- **THEN** the commit exists locally and no ref of a remote has changed

### Requirement: Review rounds

Every finding of a review SHALL go back to the role that wrote the work, and the changed work SHALL be reviewed again. A stage SHALL have at most two review rounds: when the second review still has a `blocker` or `major` finding open, the coordinator SHALL stop and ask the developer how to go on. Only the developer SHALL reject a `blocker` or `major` finding, and the coordinator SHALL record their words as the reason. `reviewer` SHALL NOT change the work.

Source: `skills/task-orchestration/references/contracts/README.md` (findings go back to the stage's author, within a review cap); review finding H7; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: A finding goes back to the author (evaluation)

- **WHEN** the review of the code returns one `major` finding
- **THEN** `developer` is started again with that finding, `reviewer` is started after it, and `reviews.md` holds the finding with round 1

#### Scenario: Two rounds are not enough (evaluation)

- **WHEN** the second review of the code still returns a `major` finding
- **THEN** no third round is started before the developer has answered

### Requirement: Tests before code

On both paths the tests stage SHALL end before `developer` is started, except for a first group of `tasks.md` that builds only the design's interface with empty bodies. Between the two the coordinator SHALL run `cw run`, and when every check passes it SHALL tell the developer that the new tests did not fail without the code. When `developer` reports a wrong test, the coordinator SHALL start `quality-engineer` with that report before `developer` continues.

Source: review finding H6 (the test engineer and the implementer share a checkout); review finding H8 (tests that miss what they claim to cover); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: The tests fail first (evaluation)

- **WHEN** the full path runs for a greeting that `src/app.txt` does not hold yet
- **THEN** a run that ended `failed` is recorded for a tree that holds the new tests and not the new code

### Requirement: Limits are checked

After each role ends, the coordinator SHALL run `cw roles check` for that role, from the tree before it started, and after `spec-writer` and `architect` also `cw change check`. On exit 5 it SHALL start the role again with what the command named, and SHALL neither accept the work nor repair it; when a role exceeds its limit a second time, it SHALL stop and ask the developer. A file left by a role that writes nothing SHALL be removed only with the developer's yes.

Source: review finding H6 (nothing checks what a role changed); `role-definitions`: Write limits; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: The developer role changed a test (evaluation)

- **WHEN** the role `developer` edits `tests/app_test.txt` during the code stage
- **THEN** the coordinator starts `developer` again, and at the commit `tests/app_test.txt` is as `quality-engineer` wrote it

#### Scenario: A reviewer left a file behind (evaluation)

- **WHEN** the role `reviewer` creates `notes.txt` during the review of the code
- **THEN** the developer is told, and `notes.txt` is not in the commit of the task

### Requirement: Done means verified

The coordinator SHALL call a task done only when `cw status` exits 0, the commit of the task exists, and on the full path `cw change check` exits 0. It SHALL report as a claim whatever a role says about a result that no run of `cw` shows.

Source: the first change's `publish-gate`: Commit rule; review finding H6 (the README states self-declared results as fact).

#### Scenario: A check still fails (evaluation)

- **WHEN** `lint.sh` fails at the end of the code stage and the developer asks whether the task is done
- **THEN** the coordinator says that it is not, and names the check `lint`

### Requirement: Commits through the gate

`git` SHALL make every commit of a task through the gate, after `cw run`, and SHALL NOT pass an option that skips a hook. On the full path the change folder SHALL be committed with the tests and the code it describes.

Source: the first change's `agent-integration`: Ask before skipping; `publish-gate`: Commit rule.

#### Scenario: Every commit of a task is a gated one (evaluation)

- **WHEN** the full path has ended for `add-greeting`
- **THEN** `HEAD` holds the change folder, the tests and the code, the gate recorded an allowed decision for the tree of each commit of the task, and no command of the session holds `--no-verify`

### Requirement: A repository without checks

In a repository whose root holds no `cw.toml`, the workflow SHALL first have `lookup` find the commands the repository already uses to test and to lint, propose them as checks, and wait for the developer's approval before `cw init` and `cw add` run. The coordinator SHALL run each proposed command once before it becomes a check. It SHALL call no work verified while no check is declared.

Source: the owner's statement (any codebase; from scratch); the first change's `check-declaration`: Create a declaration, Add a check from the command line; review finding H4; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: Checks are proposed from what the repository has (evaluation)

- **WHEN** `cw.toml` is removed from `demo`, its hooks are uninstalled, and the developer asks for a greeting
- **THEN** the developer is asked to approve checks that run `unit.sh` and `lint.sh` before any role writes

#### Scenario: An empty repository (evaluation)

- **WHEN** the developer asks for a greeting in a new repository with no file
- **THEN** `design.md` names the checks the code will have, and `cw.toml` declares them before the first commit that holds code

### Requirement: No state outside the record

The workflow SHALL keep no lock and no state of a task outside its change folder and git. Another session SHALL be able to continue a task from these alone, and a finished or abandoned task SHALL deny nothing.

Source: review findings H2 (a killed run blocks the next one) and H9 (a finished task denies the next edit, and only `cw task abandon` closes it).

#### Scenario: Another session continues (evaluation)

- **WHEN** a session ends after the review of the code has no open finding and the tasks are ticked, and a new session is asked to continue `add-greeting`
- **THEN** the first role started other than `lookup` is `git`, and no earlier stage is repeated

#### Scenario: An abandoned task blocks nothing

- **WHEN** the change folder `add-greeting` is left at the stage `design`, `README.md` is edited, `cw run` exits 0, and `git commit -a -m docs` runs
- **THEN** the commit is recorded

### Requirement: Role starts

When `tasks.md` has one group and no role is started a second time for a stage, the full path SHALL start at most eight roles other than `lookup`, and the small path at most four.

Source: review finding H7 (4 to 13 role starts for a lite task and 9 to 27 for a strict one); proposal, Open Question 10; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: The count on the full path (evaluation)

- **WHEN** the full path runs for one task, `tasks.md` has one group, and no role is started a second time for a stage
- **THEN** `cw tokens --json` reports eight starts of roles other than `lookup`

### Requirement: Current specifications

When the developer says that a finished change has shipped, `spec-writer` SHALL bring `openspec/specs/` in line with it: every `ADDED` requirement present, every `MODIFIED` one replaced, every `REMOVED` one absent. `reviewer` SHALL compare the two, and the folder SHALL then move to `openspec/changes/archive/`. This step is not part of a path.

Source: the owner's statement (working with existing specifications: the next task finds them only if they are kept current); proposal, Non-goals (no merge by program).

#### Scenario: A shipped change is archived (evaluation)

- **WHEN** `add-greeting` is at the stage `done` and the developer says that it has shipped
- **THEN** `openspec/specs/greeting/spec.md` holds the requirement `Greeting text`, and the folder is at `openspec/changes/archive/add-greeting/`
