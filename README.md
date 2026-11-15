<div align="center">

# Kitsu

<img src="docs/brand/kitsu-banner.png" alt="Kitsu: a fluffy Pomeranian in a red beanie holds a checked code-change card. Coding agents, working together." width="100%">

**Coding agents, working together.**

A workflow toolkit for coordinating coding agents through specification, implementation, testing, and review.
Seven specialist roles, shared task records, and token reporting, with a Git gate to check work before commits and pushes.

[![CI](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Runs on Linux, macOS and Windows](https://img.shields.io/badge/runs%20on-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#install)

[The roles](#the-roles) · [Quick start](#quick-start) · [Advisers](#ask-an-adviser) · [Install](#install) ·
[For a coding agent](#for-a-coding-agent) · [The Git gate](#how-the-gate-decides) · [State](#state) · [Documents](#documents)

</div>

| Part | What it does |
| --- | --- |
| **Agent coordination** | A coordinator in your coding-agent host delegates work to seven specialist roles, with a defined order, review handoffs, model choices, and limits on what each role may change. |
| **Shared task records** | Requirements, design decisions, tasks, tests, and review findings give the agents a common record to work from. `kitsu change` checks that record and reports the task's stage. |
| **Token reporting** | `kitsu tokens` reports recorded token usage by task and role, including the coordination overhead. |
| **Advisers** | Optional configured CLIs answer a role's questions or compare choices; the coordinator records the answer and decision. |
| **Git gate** | As a supporting check, Git hooks allow commits and pushes only when the checks declared in `kitsu.toml` have passing evidence for that content. |

Your agent host runs the coordinator and delegates to the roles. Kitsu supplies the workflow instructions,
role definitions, record checks, and usage reports. The Git gate supports that workflow and can also be used on its own.

> [!NOTE]
> Kitsu is a pre-release. [Install from source](#install); [State](#state) covers release status and validation.

## The roles

<img src="docs/diagrams/task-workflow.svg" alt="The task workflow. Full tasks start with specification and design, each independently reviewed and approved by you. Both paths continue through tests, code, review, and a gated commit. Configured advice checkpoints apply to spec, design, and review. Any permitted role can ask an adviser: the coordinator checks its writes, runs the adviser CLI, records the answer and rationale, and resumes the role." width="100%">

A task that needs a requirement written or changed takes the full path: specification, design, tests, code, review
and commit. A task whose intended behaviour is already clear and does not change starts at the tests. You approve
the specification and the design, and the commit goes through the gate.

| Role | Its job | It may change |
| --- | --- | --- |
| `lookup` | Finds files, facts and existing specifications, and reports where they are. | nothing |
| `spec-writer` | Writes the requirements and their scenarios from the request and its sources. | `proposal.md` and `specs/` of the task's change folder |
| `architect` | Designs the building blocks, their dependencies and the tasks. | `design.md` and `tasks.md` of the task's change folder |
| `quality-engineer` | Writes the tests of the scenarios before the code exists. | the test paths, and `tests.md` of the task's change folder |
| `developer` | Writes the code that makes the tests pass, within the design. | any other path except `kitsu.toml`, `kitsu-roles.toml`, `kitsu-advisers.toml` and `openspec/`; it may add a test, and may not change or remove one |
| `reviewer` | Reviews a specification, a design or code that another role wrote, and reports findings. | nothing |
| `git` | Runs the checks and makes the commit through the gate. | nothing |

`kitsu roles` prints the seven roles, each with its model, its effort and what it may write. Use
`kitsu roles set` to save model and reasoning overrides in `kitsu-roles.toml`. The texts name no language and no build tool; the commands of a repository come from its
`kitsu.toml`.

`kitsu roles --host codex` or `kitsu roles --host claude` shows a compact table of role, model,
reasoning effort, and allowed advisers. Add `--task <task>` to show a saved task's adviser access.
Optional advisers use authenticated CLIs configured in `kitsu-advisers.toml` or your personal
configuration. Roles return questions to the coordinator, which runs `kitsu consult` and records
how the answer was addressed. Adviser answers replace no review or approval.

- [Roles](docs/roles.md): what each role does and may change.
- [Advisers](docs/advisers.md): definitions, role access, advice and decisions, and required checkpoints.
- [The task workflow](docs/workflow.md): the two paths, the record of a task under `openspec/changes/<task>/`, and
  what `kitsu change` checks.
- [Tokens](docs/tokens.md): what is counted, and what "workflow tokens" mean.

## Quick start

1. [Install](#install) the CLI and [add the plugin](#for-a-coding-agent) to your coding-agent host.
2. Run `kitsu roles --host codex` or `kitsu roles --host claude` to inspect the role/model/reasoning/advisers table.
   [Set a specialist's model or reasoning](docs/roles.md#changing-a-model) with `kitsu roles set`; your host selects the coordinator.
3. Optionally [configure an adviser](#ask-an-adviser) to answer questions through a CLI you already use.
4. Give the `task` skill a coding task. On Claude Code, invoke `/kitsu:task`; on Codex, use the installed `task` skill.
   The coordinator chooses the workflow, sets up the repository's checks and role paths, and delegates each stage.
5. Review the proposed specification and design when the task changes behavior, then follow implementation,
   tests, and review through the shared task record.

You can inspect the role instructions and a recorded task from the terminal:

```sh
kitsu roles --host codex
kitsu roles text coordinator
kitsu change status <task>
kitsu tokens --task <task>
```

The [task workflow](docs/workflow.md) explains the full path and the shorter path for work whose behavior is already clear.
Task records and token totals appear as the workflow runs; the commands above do not launch agents themselves.

Change a role's selection without editing TOML:

```sh
kitsu roles set developer --host codex --model gpt-6.1-sol --reasoning max
kitsu roles set developer --host claude --model opus --reasoning high
kitsu roles set reviewer --host codex --reasoning high
kitsu roles reset developer --host codex
```

Omit `--model` or `--reasoning` to keep that value. Reset restores both defaults for that role and host.

### Ask an adviser

Install and authenticate your chosen provider CLI first. This example uses Codex; the
[adviser guide](docs/advisers.md) also shows Claude Code and personal definitions. In your repository,
create `kitsu-advisers.toml`:

```toml
version = 1

[advisers.second-opinion]
model = "gpt-6.1-sol"
command = [
  "codex", "exec", "--model", "{model}",
  "-c", 'model_reasoning_effort="high"',
  "--sandbox", "read-only", "--ephemeral", "--color", "never",
  "--output-last-message", "{answer_file}", "-"
]

[access]
default = []
coordinator = ["second-opinion"]
architect = ["second-opinion"]
```

This requests high reasoning from the adviser; [the guide](docs/advisers.md#configure-definitions-and-access)
shows effort selection for both CLIs. Role models and effort remain separately configurable.

Inspect access and bind a small task. Binding records the selected policy without contacting a provider:

```sh
kitsu roles --host codex
kitsu advisers bind --task advice-demo --workflow small
```

Ask a question from the terminal, or let the coordinator broker it for a role. This command invokes
the authenticated provider CLI and may consume your account's usage:

```sh
kitsu consult --task advice-demo --role coordinator --stage code \
  --adviser second-opinion --request - <<'JSON'
{"version":1,"kind":"advice","question":"What failure cases should an order CSV export cover?"}
JSON
```

Read the answer, then use the returned consultation id to record how you addressed it:

```sh
kitsu advisers resolve --task advice-demo <consultation-id> \
  --disposition accept --reason "Use these cases when planning the export tests."
kitsu advisers status --task advice-demo
```

The request, answer, execution outcome, and disposition are under
`openspec/changes/.advice/advice-demo/`. Full tasks keep them under
`openspec/changes/<task>/advisers/`; commit the record with the task. You can also
[compare named choices and require stage checkpoints](docs/advisers.md#ask-a-question-or-compare-choices).
Advice grants no approval and replaces no review. Adviser usage is reported as unmeasured.

### Add the supporting Git gate

In a repository of yours, once `kitsu` is installed:

```sh
kitsu init                          # creates kitsu.toml, installs three git hooks
kitsu add test -- <your test command>
kitsu run                           # runs the checks and records the result
git add -A
git commit -m "..."              # allowed: the content has a passing run
```

Change a file and commit again without `kitsu run`, and the commit is denied. The message names the check that has no
passing run and the command to run:

```text
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  test: not run on this content
kitsu: to produce the missing evidence, run:
  kitsu run
```

The [quick start](docs/quickstart.md) is the same five commands as a text that a test executes.

## Install

`kitsu` is one file, and it needs git. It uses options that git has had since
version 2.24, and was tried by hand with git 2.53 only. On Windows the git
hooks are shell scripts, run by the `sh` of Git for Windows.

**Build this checkout.** Kitsu's first release, `v3.0.0-beta.2`, is being
prepared and has not been published. The older `v3.0.0-beta.1` release contains
the `cw` executable, not Kitsu. Clone the source if needed, then build with Go 1.27:

```sh
git clone https://github.com/belevtsev/kitsu.git
cd kitsu
```

Linux and macOS:

```sh
mkdir -p "$HOME/.local/bin"
GOWORK=off go build -o "$HOME/.local/bin/kitsu" ./cmd/kitsu
```

Windows, in PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\kitsu" | Out-Null
$env:GOWORK = "off"
go build -o "$env:LOCALAPPDATA\kitsu\kitsu.exe" ./cmd/kitsu
```

Then put the directory that holds `kitsu` on your `PATH`, and open a new shell.
`$HOME/.local/bin` is not on `PATH` by default on macOS, nor on some Linux
systems. For bash, or a shell that reads `~/.profile`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile
```

For zsh, the shell of macOS, write the same line to `~/.zprofile`. On Windows,
in PowerShell, for your user:

```powershell
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:LOCALAPPDATA\kitsu", "User")
```

The hooks look for `kitsu` only in the entries of `PATH` that are absolute paths; see
[hooks](docs/hooks.md#when-kitsu-is-not-on-path).

The [release installation guide](docs/release-install.md) holds the archive
recipes prepared for `v3.0.0-beta.2`; they become usable after publication.
Existing `cw` users should follow the [migration guide](docs/migration.md)
before initializing Kitsu in an existing repository.

Do not leave a built `kitsu` in the root of a repository: git does not ignore it,
so it becomes part of the content every check is judged on.
[The test guide](docs/try-it.md) builds it into a folder of its own.

## For a coding agent

The plugin's `task` skill gives the coding-agent host a coordinator workflow for specification, design, tests,
implementation, review, and commit. Claude Code receives the seven roles as agents; on Codex, the coordinator uses
the texts printed by `kitsu roles text <role>`. Execution and delegation are handled by the host.

The plugin also connects the workflow to supporting checks and token reporting:

| It adds | What that does |
| --- | --- |
| Context | It tells a new session which repository checks support the workflow. |
| A guard | It stops a git call that skips the hooks, such as `git commit --no-verify`, a `kitsu` call that turns the gate off, and an edit of [workflow configuration](docs/operations.md#what-it-asks-about), a hook, the git configuration, or a Kitsu record. |
| Token notices | It gives `kitsu tokens record` the notices that a role or the session has ended. |

Claude Code asks you before a call that the guard stops. Codex refuses it and gives the reason, because Codex cannot
ask from a hook; run the call yourself if you want it. The guard is a reminder, not a wall:
[operations](docs/operations.md#what-it-misses) lists what it misses.

The additional [`kitsu`](skills/kitsu/SKILL.md) skill explains the check-and-change loop, each gate denial,
and seven worked examples that tests replay against the CLI. The role workflow has not been run on Codex.

On Claude Code:

```text
/plugin install kitsu --marketplace belevtsev/kitsu
```

On Codex, add this repository as a marketplace with `codex plugin marketplace add belevtsev/kitsu`, then
add the plugin `kitsu` with `codex plugin add`. Codex does not run the hooks of a plugin when it installs
it. Its documentation says: "Installing or enabling a plugin doesn't automatically trust its hooks … Codex skips
them until the user reviews and trusts the current hook definition." Until you review and trust them, the session
gets no text from `kitsu context`, no guard and no token records. The git hooks still decide.

## How the gate decides

<img src="docs/diagrams/gate.svg" alt="How the gate decides. First the evidence: kitsu run runs what has not passed and records each run. Then the decision, in enforce mode: git commit or git push starts one of three git hooks, the hook asks kitsu hook, and kitsu hook judges the commit or the pushed tip against the records. When every check passed on exactly this content, git goes on. When not, or when kitsu hook cannot tell, the commit or push is denied and says why: fix, then run again." width="100%">

1. **`kitsu run` makes the evidence.** It runs each check of `kitsu.toml` that has no passing run on the working tree as
   it is on disk, and records every run with that content.
2. **git asks.** `kitsu init` installed three hooks: `pre-commit`, `pre-merge-commit` and `pre-push`. Each one asks
   `kitsu hook` about the content that git is about to record or send: the commit at a commit, and the new tip of each
   branch or tag at a push. A hook runs no check and waits for none.
3. **`kitsu hook` decides from the records.** When every declared check has a recorded passing run on exactly that
   content, git goes on. Otherwise the commit or the push is denied, also when `kitsu hook` cannot tell, and the message
   names each check that is not satisfied, or the cause.

The picture shows the gate in enforce mode, as `kitsu init` sets it. After `kitsu init --observe` the hooks record what
they would decide and allow every commit and push; see [observe mode](docs/hooks.md#observe-mode). At a push, a ref
is allowed without a judgement when the push deletes it, when it holds notes, or when it adds nothing to what the
remote already holds; see [pre-push](docs/hooks.md#pre-push).

[Hooks](docs/hooks.md) says what each hook judges, [evidence](docs/evidence.md) what a recorded passing run proves
and what it does not, and [operations](docs/operations.md) lists every command and every exit status.

The workflow, adviser records, token reports, and gate share the same CLI:

<img src="docs/diagrams/overview.svg" alt="Kitsu connects four parts. Role selection supplies models and effort to your agent host, which writes a shared task record. Optional adviser policy is bound to the task; kitsu consult uses a configured provider CLI, records answers and decisions, and checks current stage checkpoints. Host token records supply measured usage while adviser usage remains unmeasured. The supporting Git gate runs declared checks and decides commits and pushes from content-bound evidence." width="100%">

## State

- Kitsu `v3.0.0-beta.2` is prepared but unpublished. The earlier
  [`cw v3.0.0-beta.1`](https://github.com/belevtsev/kitsu/releases/tag/v3.0.0-beta.1)
  was published on 15 November 2026. Its installation was checked on Linux, macOS and Windows.
  CI builds the six Kitsu archives and tests the [prepared release instructions](docs/release-install.md).
- The current implementation passed build, vet, and race-enabled tests on Linux. Lint and vulnerability checks
  passed for Linux, macOS, and Windows targets. Native runtime tests for this change ran on Linux; the historical
  installation results above describe `cw`. See the [adviser change checks](openspec/changes/add-configurable-advisers/tests.md).
- Whether the workflow improves code quality or saves tokens compared with one agent is **not measured**.
  Historical scratch runs on two small tasks recorded workflow usage, without a comparative quality result:
  [benchmarks](docs/benchmarks.md). The planned [evaluation](docs/evaluation.md) has not been run.
- Advisers were tested with local fake executables and checked against provider CLI help. No live adviser quality
  evaluation was run; adviser token usage remains unmeasured. The model-routing recommendations are not benchmarked.
  The [production plan](openspec/production-plan.md) records earlier implementation and validation work.

## Known limits

- `git commit --no-verify` and `git push --no-verify` skip the hooks. So does a clone in which nobody ran `kitsu init`.
  A check in continuous integration, `kitsu run --base <rev>`, is the backstop.
- A passing run proves that the check passed on that content on that machine. It proves nothing about what the check
  does not test.
- The hooks judge what git is about to record. They do not run the checks: `kitsu run` does, before the commit.
- `kitsu` reads the records of Claude Code and Codex to count tokens. Their format is not documented by either host, so
  a count can be `unknown`.

More in [evidence](docs/evidence.md) and [hooks](docs/hooks.md).

## Documents

| Document | What it holds |
| --- | --- |
| [Quick start](docs/quickstart.md) | The five commands in a new repository. A test executes them. |
| [Try it](docs/try-it.md) | Everything that can be tried by hand, with the output to expect, and what was never run. |
| [Operations](docs/operations.md) | Every command, `kitsu.toml`, the exit statuses and where `kitsu` keeps its records. |
| [Hooks](docs/hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](docs/evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Roles](docs/roles.md) | The seven roles, their models, what each may write, and `kitsu-roles.toml`. |
| [Advisers](docs/advisers.md) | Optional CLI advisers, access by role, shared answers and decisions, and required checkpoints. |
| [The task workflow](docs/workflow.md) | The two paths of a task, its record under `openspec/changes/<task>/`, and `kitsu change`. |
| [Tokens](docs/tokens.md) | What `kitsu tokens` counts, what "workflow tokens" are, and when a count is unknown. |
| [Benchmarks](docs/benchmarks.md) | What `kitsu` costs: the time of a commit and of each command, and the tokens of an agent on two small tasks. |
| [Evaluation](docs/evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `kitsu` is recorded. |

[docs/README.md](docs/README.md) lists every document, and [CONTRIBUTING.md](CONTRIBUTING.md) says how to change
this repository.

## The previous workflow

Versions up to 2.0.96 were a larger workflow of skills and task records. It is kept on the branch `workflow-v3`.

## License

MIT, see [LICENSE](LICENSE). Third-party code: [THIRD_PARTY.md](THIRD_PARTY.md).
