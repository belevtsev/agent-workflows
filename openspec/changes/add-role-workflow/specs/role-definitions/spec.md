# role-definitions

## Purpose

Give every job in a task to one named role with its own model and its own
limits, and let anyone see, change and check that in one place.

Shared fixture for the scenarios below, unless a scenario says otherwise:

- The repository `demo` of the `check-evidence` specification, with one more
  file, `tests/app_test.txt`, and this `gatecub-roles.toml` at its root:

  ```toml
  version = 1

  [paths]
  tests = ["tests/**"]
  ```

- A change folder `openspec/changes/add-greeting/`.
- `T0` is the value of `tree` that `gatecub status --json` prints before the edits
  a scenario names.
- "The plugin" means the plugin this project ships for Claude Code and for
  Codex. "The coordinator" is the developer's own session.
- A scenario marked "(evaluation)" needs a model. It is run by the evaluation
  of the proposal's Success measure 1, not by CI.

## ADDED Requirements

### Requirement: Role set

The workflow SHALL have exactly seven roles beside the coordinator: `lookup`, `spec-writer`, `architect`, `quality-engineer`, `developer`, `reviewer` and `git`. `gatecub roles` SHALL print, for each, its job in one line, what it may write, and its model and effort on each host. `gatecub roles --json` SHALL print the same as one JSON object.

Source: the owner's statement of 14 November 2026 (roles such as lookup, git, architect, quality engineer and principal developer); `agents/*.md` (seven role files today); proposal, Open Question 3.

#### Scenario: The table

- **WHEN** `gatecub roles` runs in `demo`
- **THEN** it prints seven rows, one for each role named above, and exits 0

#### Scenario: The table as JSON

- **WHEN** `gatecub roles --json` runs in `demo`
- **THEN** the entry of `roles` with `id` `architect` holds `job`, `writes`, and under `models` one entry for `claude` and one for `codex`, each with `model` and `effort`

### Requirement: Default models

Without a setting in `gatecub-roles.toml`, the roles SHALL have the models and efforts of the table below.

| Role | Claude Code | Codex |
| --- | --- | --- |
| `lookup` | `haiku`, `high` | `gpt-6-luna`, `high` |
| `git` | `sonnet`, `medium` | `gpt-6-luna`, `high` |
| `architect`, `reviewer` | `opus`, `xhigh` | `gpt-6.1-sol`, `max` |
| `spec-writer`, `quality-engineer`, `developer` | `opus`, `high` | `gpt-6.1-sol`, `high` |

Source: `skills/task-orchestration/model-policy.claude.yaml` (profiles `lookup`, `github`, `decision`, `substantive`); `skills/task-orchestration/model-policy.yaml` (profiles `luna`, `github`, `decision`, `sol`); proposal, Open Question 4; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge) (a review does not run at a lower effort than the work it reviews).

#### Scenario: The defaults

- **WHEN** `gatecub roles --json` runs in `demo`
- **THEN** on `claude` the model of `lookup` is `haiku`, of `git` `sonnet`, and of `architect` and `reviewer` `opus` with effort `xhigh`

### Requirement: Roles file

A repository MAY hold `gatecub-roles.toml` at its root. Under `[claude.<role>]` and `[codex.<role>]` it SHALL be able to set `model` and `effort`, and under `[paths]` the patterns `tests`, written as the path patterns of `gatecub.toml`. `gatecub` SHALL refuse a file with an unknown key, role or host, name the file, the line and the rule, and exit 1. It SHALL NOT judge a model name.

Source: the owner's statement (the appropriate model for each role); `check-declaration`: Path patterns, Invalid declaration; proposal, Known limits (hosts change model names).

#### Scenario: A repository changes one model

- **WHEN** the lines `[claude.developer]` and `model = "sonnet"` are added to `gatecub-roles.toml` and `gatecub roles --json` runs
- **THEN** on `claude` the model of `developer` is `sonnet`, its effort is `high`, and every other role is unchanged

#### Scenario: An unknown role is refused

- **WHEN** the lines `[claude.tester]` and `model = "opus"` are added to `gatecub-roles.toml` and `gatecub roles` runs
- **THEN** it exits 1 and names `gatecub-roles.toml`, the line of `[claude.tester]`, and the role `tester` as unknown

#### Scenario: A model name is not judged

- **WHEN** the lines `[claude.git]` and `model = "no-such-model"` are added to `gatecub-roles.toml` and `gatecub roles` runs
- **THEN** it exits 0 and prints `no-such-model` for `git`

### Requirement: Explicit start

The coordinator SHALL start every role with the model and effort `gatecub roles` prints for it on that host, given on the call that starts the role. When the developer's own instruction names a model for agents, that instruction SHALL come first, and the coordinator SHALL say once which roles it changes. Only the coordinator SHALL start a role: the role files SHALL give no role a tool that starts another.

Source: `skills/task-orchestration/references/hosts.md` (pass model and effort on every call); Claude Code documentation, `sub-agents.md` (the model on the call wins over the role file; a role can start roles of its own by default); `agents/*.md` (`disallowedTools: Skill, Agent, Workflow`); the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge).

#### Scenario: No role can start a role

- **WHEN** the role files the plugin ships for Claude Code are read
- **THEN** none of the seven can use the tool `Agent`: the file denies it, or leaves it out of the tools it allows

#### Scenario: The model of the roles file is the one that runs (evaluation)

- **WHEN** `gatecub-roles.toml` sets the `claude` model of `developer` to `sonnet` and the small path runs for one task on Claude Code
- **THEN** `gatecub tokens --json` names a `sonnet` model for the role `developer`

### Requirement: Model not available

When the host cannot run a role on its model, the coordinator SHALL stop before that role writes anything, tell the developer the role and the model, and wait. It SHALL NOT run the role on another model on its own.

Source: `skills/task-orchestration/references/hosts.md` ("Never silently move these roles to your own, an expensive or any other model"); `model-policy.yaml` (`github_unavailable: report_blocker`).

#### Scenario: A model that does not exist (evaluation)

- **WHEN** `gatecub-roles.toml` sets the `claude` model of `git` to `no-such-model` and a task reaches its commit stage
- **THEN** no commit is made, and the developer is told that `git` could not run on `no-such-model`

### Requirement: Write limits

Each role SHALL change only the paths the table below gives it. `gatecub roles check <role> --from <tree> [--task <task>]` SHALL compare the snapshot with that tree, exit 0 when every path that differs is within the role's limit, and otherwise exit 5 and list each path outside it. It SHALL warn, as `gatecub status` does, when git does not run the hooks of `gatecub` in this clone.

| Role | May change |
| --- | --- |
| `lookup`, `reviewer`, `git` | nothing |
| `spec-writer` | `proposal.md` and `specs/` of the task's change folder |
| `architect` | `design.md` and `tasks.md` of the task's change folder |
| `quality-engineer` | paths that match `tests`, existing tests included, and `tests.md` of the task's change folder; it is the only role that changes or removes an existing test |
| `developer` | any other path, except `gatecub.toml`, `gatecub-roles.toml` and `openspec/`; it MAY add a path that matches `tests` and SHALL NOT change or remove one |

For a shipped change, `gatecub roles check spec-writer --from <tree> --task <task> --finalize` SHALL instead allow only `openspec/specs/<capability>/spec.md` for the capability specifications present in that task's change folder in the baseline tree. It SHALL allow adding, changing or removing those files and reject every other changed path, including the change record. Adding a capability after the baseline SHALL NOT expand the limit. `--finalize` SHALL require `spec-writer`, `--task` and a nonempty set of capability specifications in the baseline; without it the normal limit SHALL remain unchanged.

Source: review finding H6 (independence is one sentence in a prompt, and nothing checks it); proposal, Terms of the first change (snapshot; a path differs); proposal, Open Question 7; the independent review of the role texts, 14 November 2026 (`openspec/production-plan.md`, Independent challenge) (existing tests have one owner).

#### Scenario: The developer role changes a test

- **WHEN** `src/app.txt` and `tests/app_test.txt` are edited and `gatecub roles check developer --from T0` runs
- **THEN** it exits 5 and lists `tests/app_test.txt` and no other path

#### Scenario: The developer role adds a test

- **WHEN** `src/app.txt` is edited, `tests/extra_test.txt` is created, and `gatecub roles check developer --from T0` runs
- **THEN** it exits 0

#### Scenario: The quality engineer changes production code

- **WHEN** `tests/app_test.txt` and `src/app.txt` are edited and `gatecub roles check quality-engineer --from T0 --task add-greeting` runs
- **THEN** it exits 5 and lists `src/app.txt` and no other path

#### Scenario: The quality engineer removes an existing test

- **WHEN** `tests/app_test.txt` is removed and `gatecub roles check quality-engineer --from T0 --task add-greeting` runs
- **THEN** it exits 0

#### Scenario: A role that writes nothing wrote a file

- **WHEN** `notes.txt` is created and `gatecub roles check reviewer --from T0` runs
- **THEN** it exits 5 and lists `notes.txt`

#### Scenario: The spec writer stays in its folder

- **WHEN** `openspec/changes/add-greeting/specs/greeting/spec.md` is created and `gatecub roles check spec-writer --from T0 --task add-greeting` runs
- **THEN** it exits 0

#### Scenario: The spec writer synchronizes a shipped capability

- **WHEN** T0 holds the task's `specs/greeting/spec.md`, the role changes only `openspec/specs/greeting/spec.md`, and `gatecub roles check spec-writer --from T0 --task add-greeting --finalize` runs
- **THEN** it exits 0, whereas the same check without `--finalize` exits 5

#### Scenario: Finalization cannot expand its own capabilities

- **WHEN** T0 holds only the task's greeting capability, the role adds an authorization capability and its current specification, and the finalization check runs from T0
- **THEN** it exits 5 and names both newly added authorization paths as outside the limit

#### Scenario: Test paths are not declared

- **WHEN** `gatecub-roles.toml` is removed, `src/app.txt` is edited, and `gatecub roles check developer --from T0` runs
- **THEN** it exits 1 and says that `gatecub-roles.toml` must name the test paths under `[paths]`

#### Scenario: A clone in which nothing is gated

- **WHEN** `gatecub init` has not run in `demo` and `gatecub roles check reviewer --from T0` runs with nothing changed
- **THEN** it exits 0 and warns that the hooks are not installed, naming `gatecub init`

### Requirement: Roles without a writing tool

On a host that can limit the tools of a role, the role files of `lookup` and `reviewer` SHALL give them no tool that edits or writes a file, and the role file of `lookup` SHALL give it no shell.

Source: Claude Code documentation, `plugins/components.md` (a plugin's role file supports `tools` and `disallowedTools`) and `sub-agents.md` (a specifier removes the whole tool, so a shell cannot be limited to reading).

#### Scenario: The tools of the two reading roles on Claude Code

- **WHEN** the role files the plugin ships for Claude Code are read
- **THEN** `lookup` and `reviewer` list neither `Edit`, `Write` nor `NotebookEdit` among their tools, and `lookup` does not list `Bash`

### Requirement: Fresh context and brief

Each role SHALL start with none of the coordinator's or another role's conversation, and SHALL NOT be started from a copy or fork of it. Its brief SHALL name the files it is to read and what it is to hand back, and a brief for `reviewer` SHALL hold no opinion of the author about the work. A role SHALL hand back the paths it wrote, the commands it ran with their results, and its open questions.

Source: `skills/task-orchestration/references/hosts.md` (a new Agent call starts with a fresh context; a Codex worker is created without the history); `skills/task-orchestration/references/contracts/README.md` (a worker reads only its contract and the inputs it lists).

#### Scenario: A reviewer sees the work, not the author's account (evaluation)

- **WHEN** the full path runs for one task
- **THEN** the brief of each `reviewer` start names files of the task and holds no sentence from the handoff of the role that wrote them

### Requirement: Role text names no language

The role texts and the coordinator's instructions SHALL name no programming language, build tool, test framework or package manager. They SHALL take the commands of a repository from `gatecub.toml` and its conventions from the repository's own files.

Source: the owner's statement (any language and any codebase); review finding H4 (the starter plan and the language pack are Go only); `packs/go/pack.yaml`.

#### Scenario: A search of the texts

- **WHEN** the role texts, the coordinator's instructions and the files generated from them for a host are searched for `go test`, `npm`, `pytest`, `cargo`, `gradle` and `maven`
- **THEN** none of them is found

### Requirement: One text per role

Each role SHALL have one text. The files a host needs for a role SHALL be generated from it, and a test SHALL fail when a host's file differs from what the text gives.

Source: review finding C1 (`hooks/hooks.json` and `hooks/codex-hooks.json` each hold their own copy of the same text and version); `agents/*.md` beside `skills/*/agents/openai.yaml`.

#### Scenario: A host file is edited by hand

- **WHEN** one sentence of the Claude Code file of `architect` is changed, the role text is left as it is, and the tests run
- **THEN** a test fails and names that file

### Requirement: Text budget

No role text SHALL exceed 8,000 characters, the coordinator's instructions SHALL NOT exceed 12,000, and all of them together SHALL NOT exceed 60,000. `gatecub roles` SHALL print the size of each role text.

Source: review finding M24; `wc -c` at `ce9b122` (the orchestration skill and its references are 141,780 characters, the largest role contract 8,183); proposal, Open Question 10.

#### Scenario: The sizes are printed

- **WHEN** `gatecub roles --json` runs in `demo`
- **THEN** `text_chars` of each role is the number of characters that `gatecub roles text <role>` prints for it

#### Scenario: A text grows past its limit

- **WHEN** the role text of `developer` is made 8,001 characters long and the tests run
- **THEN** a test fails and names `developer` and the limit
