# Roles

`cw` defines seven roles for a coding agent's task workflow, plus the coordinator that starts
them. Each role has one job, a default model and effort on Claude Code and on Codex, and a limit
on the paths it may change. `cw` holds these definitions, prints them, and checks afterwards
which paths a role changed. `cw` starts no model: the roles run inside the developer's coding
agent, and what a role does there is an instruction to a model, not something `cw` enforces.

The roles have run with a model in two scratch repositories only, on one small task each. See
[What is not known](#what-is-not-known).

The order of the work, the record of a task and `cw change` are in
[the task workflow](workflow.md). The token count is in [tokens](tokens.md).

## The roles at a glance

These are the defaults that `cw roles` prints in a repository without `cw-roles.toml`. "Tools"
are the tools the generated Claude Code role file allows.

| Role | Job | May change | Claude Code | Codex | Tools on Claude Code |
| --- | --- | --- | --- | --- | --- |
| `lookup` | Finds files, facts and existing specifications, and reports where they are. | nothing | `haiku`, `high` | `gpt-6-luna`, `high` | Read, Grep, Glob, WebFetch, WebSearch |
| `spec-writer` | Writes the requirements and their scenarios from the request and its sources. | `proposal.md` and `specs/` of the task's change folder | `opus`, `high` | `gpt-6.1-sol`, `high` | Read, Grep, Glob, Edit, Write |
| `architect` | Designs the building blocks, their dependencies and the tasks. | `design.md` and `tasks.md` of the task's change folder | `opus`, `xhigh` | `gpt-6.1-sol`, `max` | Read, Grep, Glob, Edit, Write |
| `quality-engineer` | Writes the tests of the scenarios before the code exists. | the test paths, and `tests.md` of the task's change folder | `opus`, `high` | `gpt-6.1-sol`, `high` | Read, Grep, Glob, Edit, Write, Bash |
| `developer` | Writes the code that makes the tests pass, within the design. | any other path except `cw.toml`, `cw-roles.toml` and `openspec/`; may add a test, may not change or remove one | `opus`, `high` | `gpt-6.1-sol`, `high` | Read, Grep, Glob, Edit, Write, Bash |
| `reviewer` | Reviews a specification, a design or code that another role wrote, and reports findings. | nothing | `opus`, `xhigh` | `gpt-6.1-sol`, `max` | Read, Grep, Glob, Bash |
| `git` | Runs the checks and makes the commit through the gate. | nothing | `sonnet`, `medium` | `gpt-6-luna`, `high` | Read, Grep, Glob, Bash |

The model is given as `<model>, <effort>`. "The test paths" are the patterns that `cw-roles.toml`
names under `[paths]`. No role file gives a role the tool that starts another role; only the
coordinator starts roles. `reviewer` runs at the effort of `architect`, because a review does not
run at a lower effort than the work it reviews.

## Each role

Each role starts with none of the coordinator's conversation. Its brief is all it knows. The first
line of every brief is a label for the token report (see [tokens](tokens.md#the-label)); each role
text tells the role to ignore it. Every role hands back the paths it wrote, the commands it ran
with their results, and its open questions. The full texts are what `cw roles text <role>` prints.

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
coordinator runs `cw change check` after it.

### architect

Writes `design.md` (a table of building blocks and a table of rules) and `tasks.md` (small,
ordered tasks with `Size:`, `Depends on:`, `Check:` and `Source:`). For every new block it names
the existing code it considered. A rule that a program can check names a check that `cw.toml`
declares, or `review`. It does not change the approved specification. `developer` builds one
group of tasks per start. When the tests must be seen failing on behaviour, the first group may
build only the interface, with empty bodies, before the tests. The coordinator runs
`cw change check` after it.

### quality-engineer

Writes the tests of the scenarios, only under the test paths, before the code exists, and on the
full path writes `tests.md`: one row per scenario with the test file and a text that occurs in it.
It follows the repository's own way of testing. After its work, `cw run` is expected to fail,
because the behaviour is not there yet; in a language that compiles, because the names of the
design's interface are missing. It changes no production code. It is the only role that changes
or removes an existing test: the tests of behaviour that a `MODIFIED` or `REMOVED` requirement
changes, or of code the brief says will be renamed or moved. It lists each one with the
requirement or the reason, on the full path under `## Changed tests` in `tests.md`.

### developer

Writes the code that makes the tests pass, inside the design. It may add a test, and may not
change or remove one. It does not touch `cw.toml`, `cw-roles.toml` or anything under `openspec/`,
does not tick tasks, and does not commit. It builds one group of `tasks.md` per start. It runs
`cw run` and reports the result as it is. A test that is wrong or must change is reported, and the
coordinator sends that report to `quality-engineer`.

### reviewer

Examines a specification, a design or code that another role wrote and reports findings with a
severity (`blocker`, `major`, `minor`), a place and the evidence. It runs `cw run` and `cw status`
when it reviews code, checks each test against the WHEN and THEN of its scenario, and reports an
existing test changed or removed without a requirement or a reason from `quality-engineer`. It
changes nothing. Its Claude Code file gives it no tool that edits or writes a file; it has a shell,
which a host cannot limit to reading.

### git

Runs `git status`, `cw run` and `cw status`, stages everything with `git add -A`, and commits
through the hooks. It never passes `--no-verify`, does not push, tag or open a pull request unless
the brief says the developer asked for it, and does not rewrite history. It changes no file of the
working tree.

### The coordinator

The coordinator is the developer's own session, not a role. Its instructions are
`cw roles text coordinator`, shipped on Claude Code as the skill `/agent-workflows:task`. It
chooses the path, writes each brief, starts each role on the model and effort `cw roles` prints,
runs `cw roles check` after each role, keeps `reviews.md`, ticks the tasks in `tasks.md` after the
review of the code, and asks the developer for the approvals. It writes no specification, design, test or code itself. When a
host cannot run a role on its model, the instructions tell it to stop and tell the developer, not
to use another model. The developer's own instruction about models comes first: when it names a
model for agents, the coordinator uses it and says once which roles it changes. The steps are in
[the task workflow](workflow.md#the-flow).

## cw-roles.toml

`cw-roles.toml` is an optional file at the root of a repository. It sets the test paths and,
for any role on either host, another model or effort. Without the file every role has its
default, and `cw roles check developer` and `cw roles check quality-engineer` exit 1, because their
limits depend on the test paths.

### Format

```toml
version = 1

[paths]
tests = ["tests/**", "**/*_test.*"]

[claude.developer]
model = "sonnet"
effort = "high"

[codex.git]
model = "<another model name>"
```

- `version` must be `1`.
- `[paths]` holds one key, `tests`: an array of path patterns, relative to the repository root.
  In a pattern `**` as a whole segment matches any number of segments, also none; `*` matches any
  run of characters inside one segment; `?` matches one character. Case is respected. A pattern
  with an empty segment (a leading `/`, a trailing `/` or `//`) is refused.
- `[claude.<role>]` and `[codex.<role>]` hold `model`, `effort`, or both. Each is a text that is
  not empty. A field that is left out keeps the default.

### Changing a model

Add a table for the host and the role. With the lines `[claude.developer]` and
`model = "sonnet"`, `cw roles` prints `sonnet, high` for `developer` on Claude Code, and every
other role keeps its default:

```text
developer         sonnet, high    gpt-6.1-sol, high  2841
```

`cw` does not judge a model name. `model = "no-such-model"` is accepted and printed; a wrong name
fails only when the host tries to start the role.

The role files of the Claude Code plugin are generated with the default models. A setting in
`cw-roles.toml` does not change them. It reaches a role only because the coordinator's
instructions say to give the model and effort that `cw roles` prints on the call that starts the
role. In the two sessions of Claude Code named under [What is not known](#what-is-not-known),
every role was answered by the model it was started with; both sessions used the default models.
The proposal also notes that on Claude Code the developer's own settings can override the model
of every role.

### What is refused

Every broken rule is printed as `cw-roles.toml:<line>: <rule>` on standard error, and the command
exits 1. All the problems of the file are listed at once. For example, a file with a table
`[gemini.git]` at line 6 and a key `temperature` under `[claude.git]` at line 9:

```text
cw: cw-roles.toml:9: unknown key `temperature` under [claude.git]; it holds `model` and `effort`
cw-roles.toml:6: unknown key `gemini`; the file holds `version`, `[paths]`, and `[claude.<role>]` and `[codex.<role>]`
```

Refused are: a file that is not valid TOML; a `version` other than `1`; any top-level key other
than `version`, `paths`, `claude` and `codex`; a key under `[paths]` other than `tests`; `tests`
that is not an array of texts, or holds a pattern with an empty segment; a role that is not one
of the seven (`unknown role `tester`; the roles are lookup, spec-writer, ...`); a key under a role
other than `model` and `effort`; an empty or non-text `model` or `effort`.

`cw roles` and `cw roles check` read the file and refuse it. `cw roles text` does not read it, so
it works even when the file is invalid.

## Commands

### cw roles

```text
cw roles [--json]
```

Prints one row for each role: its id, its model and effort on Claude Code and on Codex, and the
size of its text in characters. Below the row of a role stand its job and what it may write:

```text
ROLE              CLAUDE CODE     CODEX              TEXT
lookup            haiku, high     gpt-6-luna, high   1827
    Finds files, facts and existing specifications, and reports where they are.
    May write: nothing.
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
      "text_chars": 3766,
      "models": {
        "claude": {
          "model": "opus",
          "effort": "xhigh"
        },
        "codex": {
          "model": "gpt-6.1-sol",
          "effort": "max"
        }
      }
    }
  ],
  "coordinator_text_chars": 11126
}
```

The sizes are those of this version of the texts. It reads `cw-roles.toml` of the repository it runs
in. Outside a git repository it prints the defaults.

| Exit | When |
| --- | --- |
| 0 | The roles are printed. |
| 1 | `cw-roles.toml` is invalid. |
| 2 | The command line is wrong, for example an extra argument. |

### cw roles text

```text
cw roles text <role>
cw roles text coordinator
```

Prints the one text of a role, or the coordinator's instructions, exactly as it is embedded in
`cw`. A host without role files of its own gives this text to a new worker. The size `cw roles`
prints is the number of characters of this text, each Unicode code point counted once.

| Exit | When |
| --- | --- |
| 0 | The text is printed. |
| 2 | The name is not one of the seven roles or `coordinator`, or the command line is wrong. |

### cw roles check

```text
cw roles check <role> --from <tree> [--task <task>]
```

Compares the working tree now with a tree from before the role started, and says which changed
paths are outside the role's limit. The coordinator notes the `tree` that `cw status --json`
prints before it starts a role, and passes it as `--from` when the role has ended. `--from` takes
any name git resolves to a tree, such as that id or `HEAD`. `--task` names the change folder
`openspec/changes/<task>/`; `spec-writer` and `architect` need it, and `quality-engineer` needs it
to be allowed `tests.md`.

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

When git does not run the hooks of `cw` in this clone, it also prints on standard error the
warning `cw status` prints:

```text
cw: warning: the hooks of cw are not installed in this clone, so git gates nothing here; run `cw init`
```

| Exit | When |
| --- | --- |
| 0 | Every path that differs is within the limit. |
| 5 | At least one path is outside the limit. |
| 1 | `cw-roles.toml` is invalid; `developer` or `quality-engineer` is checked and `cw-roles.toml` names no test paths; `--from` names no tree of this repository; or the directory is not in a git repository. The test paths are looked up before any path is judged, so this exit 1 comes before an exit 5. |
| 2 | The role is unknown; `--from` is missing; `--task` is not lower-case words joined by hyphens; or `spec-writer` or `architect` is checked without `--task`. |

#### What it can tell and what it cannot

`cw roles check` compares two snapshots of the working tree: the tree given by `--from` and a
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
and `coordinator.md`. These texts are embedded in `cw`, so `cw roles text` prints them.

The files a host reads are generated from them:

- `agents/<role>.md`, one for each role: a header with `name`, `description` (the job), `model`
  and `effort` (the defaults for Claude Code) and `tools`, then a comment that the file is
  generated, then the role's text.
- `skills/task/SKILL.md`: a header with `name: task` and a description, the same comment, then the
  coordinator's text.

A test of this repository makes these files from the texts and fails when a file on disk differs,
or when `agents/` holds a file that belongs to no role. The failure message names the file and the
command that writes the files again. Edit the text, never the generated file.

Other tests keep each role text within 8,000 characters, the coordinator's within 12,000 and all
of them within 60,000, and search the texts for the names of languages and build tools.

### Claude Code

The plugin of this repository ships, for Claude Code, the seven role files under `agents/`, the
skill under `skills/task/`, and the hooks in `hooks/hooks.json`. `.claude-plugin/plugin.json`
names none of these paths; they are in the folders a Claude Code plugin uses for agents, skills
and hooks. The coordinator's instructions start the roles as the agents
`agent-workflows:<role>`, and the developer starts the workflow with the skill
`/agent-workflows:task`. The `tools` list of a role file is for Claude Code to apply; `cw` does
not apply it.

### Codex

What is stated, and nothing more:

- `.codex-plugin/plugin.json` names `"skills": "./skills/"` and `"hooks":
  "./hooks/codex-hooks.json"`. It names no role files; `agents/*.md` are files for Claude Code.
- The coordinator's instructions say: on a host other than Claude Code, give a new worker the
  text that `cw roles text <role>` prints, on the model and effort `cw roles` prints for Codex.
- The design states that a host without role files of its own gives this text to a new worker.
- The proposal records that the Codex documentation does not say how a role is started on a given
  model; that question is open and is the owner's to settle.

Nothing was run on Codex: not the skill, not a role, not the hooks.

## What is not known

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
