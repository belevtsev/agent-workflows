# Roles

`kitsu` defines seven roles for a coding agent's task workflow, plus the coordinator that starts
them. Each role has one job, a default model and effort on Claude Code and on Codex, and a limit
on the paths it may change. `kitsu` holds these definitions, prints them, and checks afterwards
which paths a role changed. The roles run inside the developer's coding agent, and what a role
does there is an instruction to a model. Optional, experimental [advisers](advisers.md) use native decision APIs
or configured CLI commands through the coordinator; they change neither the role's job nor its write limit.
None are enabled by default. Roles continue without advisers when none are allowed.
Optional [existing skills](skills.md) add instructions from their original directories. They
also change no role job, write limit, tool grant, review, or approval rule.

The [early role trial](#what-is-not-known) used two scratch repositories. Later [Codex comparisons](benchmarks/archived-delivery-comparison.md) report delivery, resource use and verification on specific tasks.

The order of the work, the record of a task and `kitsu task` are in
[the task workflow](workflow.md). The token count is in [tokens](tokens.md).
The preferred commands use singular `role`; existing `roles` commands remain supported.
[CLI help and host preferences](cli.md) can supply a host once instead of repeating `--host`.

[Configure a host per role](configured-workflow.md#configure-the-roles) with version 2 of `kitsu-roles.toml`. `kitsu task run` saves those assignments for a daily task. A managed dispatch can use the same configuration.

With [compact context](compact-context.md), the coordinator prepares a fresh worker using
`kitsu task prepare`. The saved brief includes current model/reasoning, original selected skills,
write limits, factual task state and allowed advisers. Selection receipts precede its baseline;
`task finish <dispatch-id>` checks the saved write interval and existing evidence. Use explicit
`--expect-checks pass|fail` for required outcomes. The coordinator can launch a fresh worker for a manual handoff. [Supervised runs](configured-workflow.md) use the registered runner.
Codex receives role text; native Claude roles supply their own text once. Neither approach
changes the role's tools, independence, approvals or after-role write check.

## The roles at a glance

These are the defaults in a repository without `kitsu-roles.toml`. Reasoning is the effort requested
from the host. The coordinator uses the model and effort of your host session; Kitsu cannot read
or configure them.

| Role | Claude Code model | Reasoning | Codex model | Reasoning |
| --- | --- | --- | --- | --- |
| `coordinator` | host session | host session | host session | host session |
| `lookup` | `haiku` | `medium` | `gpt-6-luna` | `medium` |
| `spec-writer` | `opus` | `high` | `gpt-6.1-sol` | `high` |
| `architect` | `opus` | `max` | `gpt-6.1-sol` | `max` |
| `quality-engineer` | `sonnet` | `high` | `gpt-6.1-sol` | `high` |
| `developer` | `sonnet` | `high` | `gpt-6.1-terra` | `high` |
| `reviewer` | `opus` | `max` | `gpt-6.1-sol` | `max` |
| `git` | `haiku` | `low` | `gpt-6-luna` | `low` |

The review of a [daily task](task-harness.md#choose-independent-review) starts cheaper: `sonnet` at `high` in Claude Code and `gpt-6.1-sol` at `high` in Codex. A reviewer model or reasoning that you set applies to both.

Start the coordinator with your host session's defaults unless you explicitly
choose another model or effort. Kitsu does not configure that session;
`kitsu-roles.toml` configures specialist models and can select skills for
specialists or the coordinator.
These selections are recommendations based on the jobs, not results of a benchmark:

| Role | Why this selection | When to choose a stronger setting explicitly |
| --- | --- | --- |
| `lookup` | Bounded searches and citations need little synthesis. | Use Sol or Opus with higher effort for a difficult investigation. |
| `spec-writer` | Requirements need source reconciliation and precise scenarios. | Raise effort when sources conflict or behavior is ambiguous. |
| `architect` | Dependencies and competing designs need the strongest reasoning setting. | Keep a strong model for cross-service design and concurrency. |
| `quality-engineer` | Tests need careful separation of the requirement from the implementation. | Use Sol or Opus with higher effort for race, security, and failure-path tests. |
| `developer` | Routine implementation benefits from a coding-focused model. | Use Sol or Opus with higher effort for complex debugging or concurrency. |
| `reviewer` | Independent scrutiny needs enough effort to challenge the design. | Keep review effort at least as high as the work being reviewed. |
| `git` | Routine checks and a gated commit are bounded operations. | Use Sol or Opus with higher effort before resolving a difficult merge conflict. |

Kitsu never escalates or substitutes a model automatically. A developer's explicit model choice
takes precedence. The local Codex model catalog was checked for these model ids, and Claude Code's
[effort settings](https://code.claude.com/docs/en/model-config#adjust-effort-level) describe the
supported levels; availability still depends on the host and account. Historical scratch-session
results below used earlier defaults and do not validate this routing.

### Reasoning defaults and inheritance

The table above shows current explicit Kitsu presets. An omitted role effort,
or `role reset`, currently selects that preset; it does not request native host
inheritance. Literal `default` or `auto` values have no special Kitsu semantics.

The agreed next policy is **inherit host reasoning defaults unless a role has an
explicit override**. Keep model routing independent and preserve saved overrides,
including Codex architect `gpt-6.1-sol`/`max`. This inheritance mode is not added
by the coordinator-handoff changes; current specialist settings remain unchanged.

| Host | Required behavior for a future inheritance mode |
| --- | --- |
| Codex | Omit the native effort override and let host/model/agent configuration resolve it. Do not invent `reasoning_effort=auto`. |
| Claude Code | Omit per-role invocation/definition effort for inheritance. Do not use a global environment override that would also override explicit specialists. |

Host defaults are not necessarily provider-native defaults or low effort.
Effective reasoning must remain unverified until the host reports it. See
[Codex subagent settings](https://learn.chatgpt.com/docs/agent-configuration/subagents)
and [Claude effort resolution](https://code.claude.com/docs/en/model-config#adjust-effort-level).

`kitsu role list --host codex` or `kitsu role list --host claude` prints the selected host in a compact
**Role / Model / Reasoning / Advisers** table with a **Skills** line beneath each role. Adviser access is configured separately in
`kitsu-advisers.toml`; `--task <task>` reads a saved task binding, including access, without the
personal adviser configuration. Change a specialist's model or reasoning in
[`kitsu-roles.toml`](#kitsu-rolestoml); configure adviser models and arguments as described in
[advisers](advisers.md#configure-definitions-and-access).

| Role | Job | May change | Tools on Claude Code |
| --- | --- | --- | --- |
| `lookup` | Finds files, facts and existing specifications, and reports where they are. | nothing | Read, Grep, Glob, WebFetch, WebSearch, Skill |
| `spec-writer` | Writes the requirements and their scenarios from the request and its sources. | `proposal.md` and `specs/` of the task's change folder | Read, Grep, Glob, Edit, Write, Skill |
| `architect` | Designs the building blocks, their dependencies and the tasks. | `design.md` and `tasks.md` of the task's change folder | Read, Grep, Glob, Edit, Write, Skill |
| `quality-engineer` | Writes the tests of the scenarios before the code exists. | the test paths, and `tests.md` of the task's change folder | Read, Grep, Glob, Edit, Write, Bash, Skill |
| `developer` | Writes the code that makes the tests pass, within the design. | any other path except `kitsu.toml`, `kitsu-roles.toml`, `kitsu-advisers.toml` and `openspec/`; may add a test, may not change or remove one | Read, Grep, Glob, Edit, Write, Bash, Skill |
| `reviewer` | Reviews a specification, a design or code that another role wrote, and reports findings. | nothing | Read, Grep, Glob, Bash, Skill |
| `git` | Runs the checks and makes the commit through the gate. | nothing | Read, Grep, Glob, Bash, Skill |

"The test paths" are the patterns that `kitsu-roles.toml`
names under `[paths]`. No role file gives a role the tool that starts another role; only the
coordinator starts roles. `reviewer` runs at the effort of `architect`, because a review does not
run at a lower effort than the work it reviews.

## Each role

Each role starts with none of the coordinator's conversation. Its brief is all it knows. The first
line of every brief is a label for the token report (see [tokens](tokens.md#the-label)); each role
text tells the role to ignore it. Every role hands back the paths it wrote, the commands it ran
with their results, and its open questions. The full texts are what `kitsu role text <role>` prints.
Each role may return a question, choices, and explicit source paths for an adviser named in its
brief. The coordinator checks its writes, runs the consultation, and returns the record before
the role resumes. Roles never execute advisers or write adviser records themselves. Reviewers
first form their own assessment and receive none of the author's consultations as review guidance.
Before each start, the coordinator runs `kitsu skill resolve <role> --host <host>` with the task
and workflow, includes selected ids, original paths, and hashes in the brief, and tells the
worker to load those originals. The `Skill` tool on Claude Code or an explicit file read supplies
instructions; the host still owns execution. Selection receipts keep loading `unverified`.

### lookup

Answers questions with a path and a line for each fact: which specifications exist for a
behaviour, where it is implemented and tested, which commands the repository uses to build, test
and lint, which conventions it follows. When it does not find something, it says where it looked.
It changes nothing. Its Claude Code file gives it no tool that edits a file and no shell.

### spec-writer

Turns the request and its sources into requirements a test can check, in `proposal.md` and
`specs/<capability>/spec.md` of the change folder. Every requirement holds `SHALL` or `MUST`, a
`Source:` line and at least one scenario with one `WHEN` and one `THEN`. A value the sources do not
give becomes an open question in `proposal.md`, not a guess. When `openspec/specs/` already holds a
requirement for the behaviour, it changes that requirement under `## MODIFIED Requirements`. The
coordinator runs `kitsu task check` after it.

### architect

Writes `design.md` (a table of building blocks and a table of rules) and `tasks.md` (small,
ordered tasks with `Size:`, `Depends on:`, `Check:` and `Source:`). For every new block it names
the existing code it considered. A rule that a program can check names a check that `kitsu.toml`
declares, or `review`. It does not change the approved specification. `developer` builds one
group of tasks per start. When the tests must be seen failing on behaviour, the first group may
build only the interface, with empty bodies, before the tests. The coordinator runs
`kitsu task check` after it.

### quality-engineer

Writes the tests of the scenarios, only under the test paths, before the code exists, and on the
full path writes `tests.md`: one row per scenario with the test file and a text that occurs in it.
It follows the repository's own way of testing. After its work, `kitsu check run` is expected to fail,
because the behaviour is not there yet; in a language that compiles, because the names of the
design's interface are missing. It changes no production code. It is the only role that changes
or removes an existing test: the tests of behaviour that a `MODIFIED` or `REMOVED` requirement
changes, or of code the brief says will be renamed or moved. It lists each one with the
requirement or the reason, on the full path under `## Changed tests` in `tests.md`.

### developer

Writes the code that makes the tests pass, inside the design. It may add a test, and may not
change or remove one. It does not touch `kitsu.toml`, `kitsu-roles.toml`, `kitsu-advisers.toml` or anything under `openspec/`,
does not tick tasks, and does not commit. It builds one group of `tasks.md` per start. It runs
`kitsu check run` and reports the result as it is. A test that is wrong or must change is reported, and the
coordinator sends that report to `quality-engineer`.

### reviewer

Examines a specification, a design or code that another role wrote and reports findings with a
severity (`blocker`, `major`, `minor`), a place and the evidence. It runs `kitsu check run` and `kitsu gate status`
when it reviews code, checks each test against the WHEN and THEN of its scenario, and reports an
existing test changed or removed without a requirement or a reason from `quality-engineer`. It
changes nothing. Its Claude Code file gives it no tool that edits or writes a file; it has a shell,
which a host cannot limit to reading.

### git

Runs `git status`, `kitsu check run` and `kitsu gate status`, stages only authorized paths, and commits
through the hooks. It never passes `--no-verify`, does not push, tag or open a pull request unless
the brief says the developer asked for it, and does not rewrite history. It changes no file of the
working tree.

### The coordinator

The developer's host session coordinates the work. For managed work, the `managed` skill of the opt-in `kitsu-team` plugin holds the instructions printed by `kitsu role text coordinator`. A normal plugin request follows the [daily workflow](task-harness.md#how-a-daily-task-runs). Run
`kitsu agent check --host codex` or `--host claude` to inspect local setup, then
`kitsu task start "<coding request>" --host <host>` to explicitly launch that host with the task
skill. For an existing desktop session, add `--print` and paste its handoff. Print and JSON
modes launch nothing and create no task record. A successful local check proves neither model
availability nor live skill loading or hook trust; see [onboarding](cli.md#check-setup-and-start-a-task).

The coordinator chooses the path, writes each brief, starts each role on the model and effort `kitsu role list` prints,
runs `kitsu role check` after each role, keeps `reviews.md`, ticks the tasks in `tasks.md` after the
review of the code, and asks the developer for the approvals. It writes no specification, design, test or code itself. When a
host cannot run a role on its model, the instructions tell it to stop and tell the developer, not
to use another model. The developer's own instruction about models comes first: when it names a
model for agents, the coordinator uses it and says once which roles it changes. The steps are in
[the task workflow](workflow.md#the-flow).

## kitsu-roles.toml

`kitsu-roles.toml` is an optional file at the root of a repository. It sets the test paths and,
for a specialist on either host, another model or effort. It can also select existing skills
for any specialist or the coordinator. Without model overrides every specialist has its built-in
default. Skills inherit from personal bindings unless a repository array replaces them; with
neither file, no skills are pinned. Without test paths,
`kitsu role check developer` and `kitsu role check quality-engineer` exit 1, because their
limits depend on the test paths.

### Format

```toml
version = 1

[paths]
tests = ["tests/**", "**/*_test.*"]

[claude.developer]
model = "opus"
effort = "high"
skills = ["claude:repo:.claude/skills/project-style"]

[codex.git]
model = "<another model name>"

[codex.coordinator]
skills = []
```

- `version` must be `1`.
- `[paths]` holds one key, `tests`: an array of path patterns, relative to the repository root.
  In a pattern `**` as a whole segment matches any number of segments, also none; `*` matches any
  run of characters inside one segment; `?` matches one character. Case is respected. A pattern
  with an empty segment (a leading `/`, a trailing `/` or `//`) is refused.
- `[claude.<role>]` and `[codex.<role>]` hold optional `model`, `effort`, and `skills`. Model and
  effort are nonempty texts; an omitted field keeps the default. `skills` is an array of skill
  references. Each repository array replaces the personal list; `[]` suppresses inheritance.
- The `coordinator` tables accept only `skills`; the host selects its model and reasoning.
- Personal skill bindings use `os.UserConfigDir()/kitsu/roles.toml` and accept no models, effort,
  or test paths. See [skills](skills.md#personal-defaults-and-repository-overrides).

### Changing a model

Run these commands anywhere inside the repository; no TOML editing is needed:

```sh
kitsu role set developer --host codex --model gpt-6.1-sol --reasoning max
kitsu role set developer --host claude --model opus --reasoning high
kitsu role set reviewer --host codex --reasoning high
kitsu role list --host codex
kitsu role reset developer --host codex
```

`set` requires an effective host from `--host` or [CLI preferences](cli.md#select-your-host-once)
and at least one of `--model`, `--reasoning`, or `--skills`.
An omitted option keeps its current override or default. `--reasoning` is saved as `effort`.
`reset` removes model, effort, and skill overrides for that role and host, restoring the built-in
model defaults and any personal skill selection. Both commands print the resulting selection;
`--json` includes the role, host, model, effort, and skills.

The commands update the repository root's `kitsu-roles.toml`, creating a version 1 file when needed.
They preserve other roles, hosts, test paths, and unrelated comments; an edited inline table may be
reformatted. An invalid file or a symbolic link is refused without writing it. Resetting a missing
file creates nothing. Commit the file to share the selection with the team. `--global` updates
only personal skill bindings and rejects model or reasoning options; global reset removes only
that host/role's personal skill selection. See [the skill guide](skills.md) for discovery,
`--skills ''`, duplicates, and extra roots. Settings affect newly started workers.
The coordinator uses the model and reasoning selected in your host session.

To inspect one role or explicitly open its editor:

```sh
kitsu role show developer --host codex
kitsu role edit developer --host codex
kitsu role edit reviewer --global --accessible
```

Only `role edit` prompts. The editor offers host model suggestions, an exact custom id, multiple
skills, field help, and a preview before saving; global scope remains skills-only. Cancel saves
nothing, and save revalidates sources and concurrent changes. See [terminal editors](cli.md#open-an-editor-when-you-want-one).

`kitsu` does not judge a model name. `model = "no-such-model"` is accepted and printed; a wrong name
fails only when the host tries to start the role.

The role files of the Claude Code plugin are generated with the default models. A setting in
`kitsu-roles.toml` does not change them. It reaches a role only because the coordinator's
instructions say to give the model and effort that `kitsu role list` prints on the call that starts the
role. In the two sessions of Claude Code named under [What is not known](#what-is-not-known),
every role was answered by the model it was started with; both sessions used the default models.
The proposal also notes that on Claude Code the developer's own settings can override the model
of every role.

### What is refused

Broken configuration rules are printed with their source file and line on standard error, and
the command exits 1. Unknown hosts such as `[gemini.git]` and unknown keys such as `temperature`
are refused.

Refused are: a file that is not valid TOML; a `version` other than `1`; any top-level key other
than `version`, `paths`, `claude` and `codex`; a key under `[paths]` other than `tests`; `tests`
that is not an array of texts, or holds a pattern with an empty segment; an unknown role; a key
under a role other than `model`, `effort`, and `skills`; an empty or non-text `model` or `effort`;
invalid or duplicate skill references; or a coordinator model or effort override. The personal
file is skills-only. Selection and resolution also refuse missing, ambiguous, invalid,
host-disabled, and manual-only skill entries.

`kitsu role list`, `set`, `reset`, and `check` read the file and refuse it. `kitsu role text` does not read it, so
it works even when the file is invalid.

## Commands

### kitsu role list

```text
kitsu role list [--host claude|codex] [--task <task>] [--json]
```

Without an effective host filter, prints one row for each specialist role: its id, its model and effort on Claude
Code and on Codex, and the size of its text in characters. Below each row stand its job, what it
may write, selected skills, and allowed advisers. With `--host`, the compact table includes the
coordinator and separates model, reasoning, skills, and allowed advisers. The coordinator's model and reasoning are
shown as the host session because Kitsu has no configured values for them. A saved CLI host
preference also selects the compact host view. With `--task`, adviser
access comes from that task's binding and can be read offline. Examples:

```sh
kitsu role list --host codex
kitsu role list --host claude --task add-order-csv-export --json
```

The default two-host output starts like this:

```text
ROLE              CLAUDE CODE     CODEX              TEXT
lookup            haiku, medium   gpt-6-luna, medium  2283
    Finds files, facts and existing specifications, and reports where they are.
    May write: nothing.
    Advisers: none.
    Skills (claude): none pinned.
    Skills (codex): none pinned.
```

Below the table it prints the size of the coordinator's instructions. With `--json` it prints one object. Here it is shortened to one
of the seven entries of `roles`:

```json
{
  "roles": [
    {
      "id": "architect",
      "job": "Designs the building blocks, their dependencies and the tasks.",
      "writes": "design.md and tasks.md of the task's change folder",
      "text_chars": 4230,
      "advisers": null,
      "skills": {
        "claude": [],
        "codex": []
      },
      "models": {
        "claude": {
          "model": "opus",
          "effort": "max"
        },
        "codex": {
          "model": "gpt-6.1-sol",
          "effort": "max"
        }
      }
    }
  ],
  "coordinator_text_chars": 11887,
  "coordinator_skills": {
    "claude": [],
    "codex": []
  }
}
```

The sizes are illustrative; `kitsu role list` prints the current values. It reads `kitsu-roles.toml` of the repository it runs
in and personal skill bindings. Outside a git repository it prints model defaults with any
personal skill selections.

| Exit | When |
| --- | --- |
| 0 | The roles are printed. |
| 1 | Repository or personal role configuration is invalid. |
| 2 | The command line is wrong, for example an extra argument. |

### kitsu role text

```text
kitsu role text <role>
kitsu role text coordinator
```

Prints the one text of a role, or the coordinator's instructions, exactly as it is embedded in
`kitsu`. A host without role files of its own gives this text to a new worker. The size `kitsu role list`
prints is the number of characters of this text, each Unicode code point counted once.

| Exit | When |
| --- | --- |
| 0 | The text is printed. |
| 2 | The name is not one of the seven roles or `coordinator`, or the command line is wrong. |

### kitsu role check

```text
kitsu role check <role> --from <tree> [--task <task>] [--finalize]
```

Compares the working tree now with a tree from before the role started, and says which changed
paths are outside the role's limit. The coordinator notes the `tree` that `kitsu gate status --json`
prints before it starts a role, and passes it as `--from` when the role has ended. `--from` takes
any name git resolves to a tree, such as that id or `HEAD`. `--task` names the change folder
`openspec/changes/<task>/`; `spec-writer` and `architect` need it, and `quality-engineer` needs it
to be allowed `tests.md`.

For [a shipped change](workflow.md#the-archive), use `--finalize` with `spec-writer` and
`--task`. This replaces its ordinary limit with only `openspec/specs/<capability>/spec.md`
for each `openspec/changes/<task>/specs/<capability>/spec.md` in the `--from` tree. The role
may add, change or remove those exact current-specification files. It may change no other
capability, no source file and no change record. A capability added after that snapshot gives
no new permission. The baseline must contain at least one capability specification.

Within the limit:

```text
`developer` stayed within its limit: 2 paths differ from 2ce48b04048bdc1368c1b461e867ef921f82d0e6
```

Outside the limit, each path with its kind (`added`, `changed` or `removed`) and the rule:

```text
`developer` changed 1 of 2 paths outside its limit:
  tests/app_test.txt (changed): `developer` may add a test and may not change or remove one
start `developer` again with these paths; do not accept the work and do not repair it yourself
```

When git does not run the hooks of `kitsu` in this clone, it also prints on standard error the
warning `kitsu gate status` prints:

```text
kitsu: warning: the hooks of kitsu are not installed in this clone, so git gates nothing here; run `kitsu gate init`
```

| Exit | When |
| --- | --- |
| 0 | Every path that differs is within the limit. |
| 5 | At least one path is outside the limit. |
| 1 | `kitsu-roles.toml` is invalid; `developer` or `quality-engineer` is checked and `kitsu-roles.toml` names no test paths; `--from` names no tree of this repository; finalization finds no capability specification for the task in that tree; or the directory is not in a git repository. The test paths are looked up before any path is judged, so this exit 1 comes before an exit 5. |
| 2 | The role is unknown; `--from` is missing; `--task` is not lower-case words joined by hyphens; `spec-writer` or `architect` is checked without `--task`; or `--finalize` is used without `spec-writer` and `--task`. |

#### What it can tell and what it cannot

`kitsu role check` compares two snapshots of the working tree: the tree given by `--from` and a
snapshot of the files now, tracked and untracked, without the files git ignores. It lists every
path whose content differs and compares each with the role's limit. That is all it knows.

It cannot tell:

- **Who made a change.** Every difference between the two snapshots is charged to the role. An
  edit by the developer, by another session or by a tool during that time looks the same.
- **What happened between the snapshots.** A file that a role changed and changed back does not
  differ. A file that git ignores is not seen.
- **What a role read.** Every role can read every file. Only writing is checked.
- **Whether the work is right.** A path within the limit can hold wrong content.

It also changes nothing: it does not undo a change. The coordinator's instructions say to start
the role again with the paths it named.

## How the roles reach a host

### One text per role

Each role, and the coordinator, has one text under `internal/roles/text/`: `lookup.md`,
`spec-writer.md`, `architect.md`, `quality-engineer.md`, `developer.md`, `reviewer.md`, `git.md`
and `coordinator.md`. These texts are embedded in `kitsu`, so `kitsu role text` prints them.

The files a host reads are generated from them:

- `plugins/kitsu-team/agents/<role>.md`, one for each role: a header with `name`, `description` (the job), `model`
  and `effort` (the defaults for Claude Code) and `tools`, then a comment that the file is
  generated, then the role's text.
- `skills/task/SKILL.md` contains the daily task instructions. The managed coordinator text is shipped in `plugins/kitsu-team/skills/managed/SKILL.md`.

A test of this repository makes these files from the texts and fails when a file on disk differs,
or when `plugins/kitsu-team/agents/` holds a file that belongs to no role. The failure message names the file and the
command that writes the files again. Edit the text, never the generated file.

Other tests keep each role text within 8,000 characters, the coordinator's within 12,000 and all
of them within 60,000, and search the texts for the names of languages and build tools.

### Claude Code

This repository ships two plugins for Claude Code. The default plugin `kitsu` holds the skills under `skills/` and the hooks in `hooks/hooks.json`. The [opt-in plugin `kitsu-team`](release-install.md#add-role-coordination) holds the seven role files under `plugins/kitsu-team/agents/` and the `managed` skill. Neither `plugin.json`
names these paths; they are in the folders a Claude Code plugin uses for agents, skills
and hooks. Ask `/kitsu-team:managed` to use the managed role workflow and the coordinator starts specialists as `kitsu-team:<role>`. The `tools` list of a role file is for Claude Code to apply; `kitsu` does
not apply it.

`kitsu task start "<request>" --host claude` opens an interactive Claude Code session with the
daily task skill; for managed work the host still dispatches the `kitsu-team:<role>` workers.

### Codex

What is stated, and nothing more:

- `.codex-plugin/plugin.json` names `"skills": "./skills/"` and `"hooks":
  "./hooks/codex-hooks.json"`. It names no role files; the role files of `kitsu-team` are for Claude Code. The Codex manifest of `kitsu-team` names its `managed` skill.
- The coordinator's instructions say: on a host other than Claude Code, give a new worker the
  text that `kitsu role text <role>` prints, on the model and effort `kitsu role list` prints for Codex.
- The design states that a host without role files of its own gives this text to a new worker.
- `kitsu task start "<request>" --host codex` opens the interactive host with an explicit request
  to use Kitsu's installed task skill. It does not add a worker scheduler or verify role execution.
- The proposal records that the Codex documentation does not say how a role is started on a given
  model; that question is open and is the owner's to settle.

The early trial used Claude Code. Later Codex runs are recorded in the [coding task comparisons](benchmarks/archived-delivery-comparison.md).

## What is not known

These are historical results for `cw`, before the Kitsu rename; no new Kitsu host run is claimed.

- **The roles ran with a model in two sessions only.** On 14 November 2026 a script gave the
  skill `task` a small task in three sessions of Claude Code 2.1.311, on two tasks in scratch
  repositories, and answered every question of the coordinator with one fixed approval. Roles
  started in two of the three.
  [Benchmarks](benchmarks.md#what-an-agent-spends) holds what the roles did there and what they
  cost. Nobody answered the coordinator's questions on their merits, `lookup` was not started,
  and the cases of [the evaluation](evaluation.md) were not run. Two tasks do not show whether a
  model follows the texts, the limits and the order of the work in general.
- **Whether seven roles give better code than one agent is not measured.** The roles cost tokens
  that one agent would not spend, and `cw tokens` shows how many. The comparison with one agent
  is defined in [the evaluation](evaluation.md#the-comparison-with-one-agent) and has not been run.
  The two sessions show what the roles cost on two tasks, not what they are worth.
- **Whether a host uses the model a role is started with** was observed on Claude Code only, and
  only for the default models: in the two sessions every start was answered by the model the
  coordinator named on the call, `claude-opus-5-5` for `opus` and `claude-sonnet-5-5` for
  `sonnet`. A model set in `cw-roles.toml`, the effort, the model of `lookup`, and Codex were not
  observed.
