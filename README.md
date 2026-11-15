<div align="center">

# Gatecub

<img src="docs/brand/gatecub-banner.png" alt="Gatecub: a fluffy Pomeranian in a red beanie holds a checked code-change card. Checked changes. Coordinated agents." width="100%">

**Checked changes. Coordinated agents.**

A gate for `git commit` and `git push`, seven roles for coding agents, and token usage reporting.
One small program for any Git repository, in any language.

[![CI](https://github.com/belevtsev/gatecub/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/belevtsev/gatecub/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Runs on Linux, macOS and Windows](https://img.shields.io/badge/runs%20on-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#install)

[Quick start](#quick-start) · [How the gate decides](#how-the-gate-decides) · [Install](#install) ·
[For a coding agent](#for-a-coding-agent) · [The roles](#the-roles) · [State](#state) · [Documents](#documents)

</div>

<img src="docs/diagrams/overview.svg" alt="The three parts of gatecub. The gate: gatecub.toml declares your checks, gatecub run runs what has not passed and records each run, and gatecub hook, asked by the git hooks, decides from that evidence. The roles: gatecub roles gives the role texts to your coding agent, which writes the record of a task under openspec/changes, and gatecub change check checks the rules of its files. The token count: gatecub tokens record reads the session record that the agent host keeps and stores the counts, and gatecub tokens sums them role by role." width="100%">

| Part | What it does |
| --- | --- |
| **The gate** | A commit or a push is allowed only when every check the repository declares in `gatecub.toml` has a recorded passing run on exactly that content. Git hooks enforce it, for people and for coding agents alike. |
| **The roles** | A coding agent takes a task from a specification to tested code through seven roles, each with one job, its own model and a limit on what it may change. `gatecub` checks afterwards what a program can check. |
| **The token count** | `gatecub tokens` says what a task cost, role by role, and how much of it the workflow itself used. |

> [!NOTE]
> The gate is a guardrail, not a sandbox: it stops a mistake, and it does not stop a person who decides to go around
> it.

> [!IMPORTANT]
> This is a pre-release, and **nothing here is measured to improve anything**. [State](#state) says what was tried
> and what was not.

## Quick start

In a repository of yours, once `gatecub` is [installed](#install):

```sh
gatecub init                          # creates gatecub.toml, installs three git hooks
gatecub add test -- <your test command>
gatecub run                           # runs the checks and records the result
git add -A
git commit -m "..."              # allowed: the content has a passing run
```

Change a file and commit again without `gatecub run`, and the commit is denied. The message names the check that has no
passing run and the command to run:

```text
gatecub: this commit is denied: its content has no recorded passing run of every declared check.
  test: not run on this content
gatecub: to produce the missing evidence, run:
  gatecub run
```

The [quick start](docs/quickstart.md) is the same five commands as a text that a test executes.

## How the gate decides

<img src="docs/diagrams/gate.svg" alt="How the gate decides. First the evidence: gatecub run runs what has not passed and records each run. Then the decision, in enforce mode: git commit or git push starts one of three git hooks, the hook asks gatecub hook, and gatecub hook judges the commit or the pushed tip against the records. When every check passed on exactly this content, git goes on. When not, or when gatecub hook cannot tell, the commit or push is denied and says why: fix, then run again." width="100%">

1. **`gatecub run` makes the evidence.** It runs each check of `gatecub.toml` that has no passing run on the working tree as
   it is on disk, and records every run with that content.
2. **git asks.** `gatecub init` installed three hooks: `pre-commit`, `pre-merge-commit` and `pre-push`. Each one asks
   `gatecub hook` about the content that git is about to record or send: the commit at a commit, and the new tip of each
   branch or tag at a push. A hook runs no check and waits for none.
3. **`gatecub hook` decides from the records.** When every declared check has a recorded passing run on exactly that
   content, git goes on. Otherwise the commit or the push is denied, also when `gatecub hook` cannot tell, and the message
   names each check that is not satisfied, or the cause.

The picture shows the gate in enforce mode, as `gatecub init` sets it. After `gatecub init --observe` the hooks record what
they would decide and allow every commit and push; see [observe mode](docs/hooks.md#observe-mode). At a push, a ref
is allowed without a judgement when the push deletes it, when it holds notes, or when it adds nothing to what the
remote already holds; see [pre-push](docs/hooks.md#pre-push).

[Hooks](docs/hooks.md) says what each hook judges, [evidence](docs/evidence.md) what a recorded passing run proves
and what it does not, and [operations](docs/operations.md) lists every command and every exit status.

## Install

`gatecub` is one file, and it needs git. It uses options that git has had since
version 2.24, and was tried by hand with git 2.53 only. On Windows the git
hooks are shell scripts, run by the `sh` of Git for Windows.

**Build this checkout.** Gatecub's first release, `v3.0.0-beta.2`, is being
prepared and has not been published. The older `v3.0.0-beta.1` release contains
the `cw` executable, not Gatecub. Build from this checkout with Go 1.27:

Linux and macOS:

```sh
mkdir -p "$HOME/.local/bin"
GOWORK=off go build -o "$HOME/.local/bin/gatecub" ./cmd/gatecub
```

Windows, in PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\gatecub" | Out-Null
$env:GOWORK = "off"
go build -o "$env:LOCALAPPDATA\gatecub\gatecub.exe" ./cmd/gatecub
```

Then put the directory that holds `gatecub` on your `PATH`, and open a new shell.
`$HOME/.local/bin` is not on `PATH` by default on macOS, nor on some Linux
systems. For bash, or a shell that reads `~/.profile`:

```sh
echo 'export PATH="$HOME/.local/bin:$PATH"' >> ~/.profile
```

For zsh, the shell of macOS, write the same line to `~/.zprofile`. On Windows,
in PowerShell, for your user:

```powershell
[Environment]::SetEnvironmentVariable("Path", [Environment]::GetEnvironmentVariable("Path", "User") + ";$env:LOCALAPPDATA\gatecub", "User")
```

The hooks look for `gatecub` only in the entries of `PATH` that are absolute paths; see
[hooks](docs/hooks.md#when-gatecub-is-not-on-path).

The [release installation guide](docs/release-install.md) holds the archive
recipes prepared for `v3.0.0-beta.2`; they become usable after publication.
Existing `cw` users should follow the [migration guide](docs/migration.md)
before initializing Gatecub in an existing repository.

Do not leave a built `gatecub` in the root of a repository: git does not ignore it,
so it becomes part of the content every check is judged on.
[The test guide](docs/try-it.md) builds it into a folder of its own.

## For a coding agent

The gate needs no plugin: the git hooks decide. The plugin in this repository adds three small things on Claude Code
and on Codex:

| It adds | What that does |
| --- | --- |
| Context | It tells a new session what the gate requires. |
| A guard | It stops a git call that skips the hooks, such as `git commit --no-verify` or `git commit -n`, a `gatecub` call that turns the gate off, and an edit of `gatecub.toml`, a hook file, the git configuration or a record of `gatecub`. |
| Token notices | It gives `gatecub tokens record` the notices that a role or the session has ended. |

Claude Code asks you before a call that the guard stops. Codex refuses it and gives the reason, because Codex cannot
ask from a hook; run the call yourself if you want it. The guard is a reminder, not a wall:
[operations](docs/operations.md#what-it-misses) lists what it misses.

The plugin ships two skills. [`gatecub`](skills/gatecub/SKILL.md) tells an agent how to work in a gated repository: the loop
for any change, what each denial means, and seven worked examples that a test replays against `gatecub`. `task` is the
role workflow below. On Claude Code the plugin also ships the seven roles as agents; on Codex a role is started from
the text `gatecub roles text <role>` prints. Nothing was run on Codex.

On Claude Code:

```text
/plugin install gatecub --marketplace belevtsev/gatecub
```

On Codex, add this repository as a marketplace with `codex plugin marketplace add belevtsev/gatecub`, then
add the plugin `gatecub` with `codex plugin add`. Codex does not run the hooks of a plugin when it installs
it. Its documentation says: "Installing or enabling a plugin doesn't automatically trust its hooks … Codex skips
them until the user reviews and trusts the current hook definition." Until you review and trust them, the session
gets no text from `gatecub context`, no guard and no token records. The git hooks still decide.

## The roles

<img src="docs/diagrams/task-workflow.svg" alt="The seven roles of a task. On the full path only: spec-writer writes the specification and architect the design and the tasks; the reviewer reviews each, then you approve. On both paths, and the small path starts here: quality-engineer writes the tests first, then gatecub run runs your checks; developer writes the code, the reviewer reviews it and findings go back; git makes the commit, and the gate allows or denies it. At any stage: lookup finds files and facts, and gatecub roles check runs after every role." width="100%">

A task that needs a requirement written or changed takes the full path: specification, design, tests, code, review
and commit. A task whose intended behaviour is already clear and does not change starts at the tests. You approve
the specification and the design, and the commit goes through the gate.

| Role | Its job | It may change |
| --- | --- | --- |
| `lookup` | Finds files, facts and existing specifications, and reports where they are. | nothing |
| `spec-writer` | Writes the requirements and their scenarios from the request and its sources. | `proposal.md` and `specs/` of the task's change folder |
| `architect` | Designs the building blocks, their dependencies and the tasks. | `design.md` and `tasks.md` of the task's change folder |
| `quality-engineer` | Writes the tests of the scenarios before the code exists. | the test paths, and `tests.md` of the task's change folder |
| `developer` | Writes the code that makes the tests pass, within the design. | any other path except `gatecub.toml`, `gatecub-roles.toml` and `openspec/`; it may add a test, and may not change or remove one |
| `reviewer` | Reviews a specification, a design or code that another role wrote, and reports findings. | nothing |
| `git` | Runs the checks and makes the commit through the gate. | nothing |

`gatecub roles` prints the seven roles, each with its model, its effort and what it may write. A repository changes the
models in `gatecub-roles.toml`. The texts name no language and no build tool; the commands of a repository come from its
`gatecub.toml`.

- [Roles](docs/roles.md): what each role does and may change.
- [The task workflow](docs/workflow.md): the two paths, the record of a task under `openspec/changes/<task>/`, and
  what `gatecub change` checks.
- [Tokens](docs/tokens.md): what is counted, and what "workflow tokens" mean.

Whether seven roles give better code than one agent working alone is **not measured**. The roles cost tokens that
one agent would not spend, and `gatecub tokens` shows how many. [The evaluation](docs/evaluation.md) says how the
comparison is to be made; it has not been run. A first run on two small tasks is in
[benchmarks](docs/benchmarks.md): it shows what the roles cost there, not whether the code is better.

## State

- Gatecub `v3.0.0-beta.2` is prepared but unpublished. The earlier
  [`cw v3.0.0-beta.1`](https://github.com/belevtsev/gatecub/releases/tag/v3.0.0-beta.1)
  was published on 15 November 2026. Its installation was checked on Linux, macOS and Windows.
  CI builds the six Gatecub archives and tests the [prepared release instructions](docs/release-install.md).
- **Nothing here is measured to improve anything.** The gate has not yet been used on real work. The plugin and the
  roles ran with a model in scratch repositories only, on two small tasks: [benchmarks](docs/benchmarks.md) says what
  that showed and what it cost. The [production plan](openspec/production-plan.md) says what was built and how each
  part was checked.
- The rename was developed on Linux. CI is configured to test Linux, macOS and Windows;
  the historical cross-platform results above describe `cw`, not an executed Gatecub release.

## Known limits

- `git commit --no-verify` and `git push --no-verify` skip the hooks. So does a clone in which nobody ran `gatecub init`.
  A check in continuous integration, `gatecub run --base <rev>`, is the backstop.
- A passing run proves that the check passed on that content on that machine. It proves nothing about what the check
  does not test.
- The hooks judge what git is about to record. They do not run the checks: `gatecub run` does, before the commit.
- `gatecub` reads the records of Claude Code and Codex to count tokens. Their format is not documented by either host, so
  a count can be `unknown`.

More in [evidence](docs/evidence.md) and [hooks](docs/hooks.md).

## Documents

| Document | What it holds |
| --- | --- |
| [Quick start](docs/quickstart.md) | The five commands in a new repository. A test executes them. |
| [Try it](docs/try-it.md) | Everything that can be tried by hand, with the output to expect, and what was never run. |
| [Operations](docs/operations.md) | Every command, `gatecub.toml`, the exit statuses and where `gatecub` keeps its records. |
| [Hooks](docs/hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](docs/evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Roles](docs/roles.md) | The seven roles, their models, what each may write, and `gatecub-roles.toml`. |
| [The task workflow](docs/workflow.md) | The two paths of a task, its record under `openspec/changes/<task>/`, and `gatecub change`. |
| [Tokens](docs/tokens.md) | What `gatecub tokens` counts, what "workflow tokens" are, and when a count is unknown. |
| [Benchmarks](docs/benchmarks.md) | What `gatecub` costs: the time of a commit and of each command, and the tokens of an agent on two small tasks. |
| [Evaluation](docs/evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `gatecub` is recorded. |

[docs/README.md](docs/README.md) lists every document, and [CONTRIBUTING.md](CONTRIBUTING.md) says how to change
this repository.

## The previous workflow

Versions up to 2.0.96 were a larger workflow of skills and task records. It is kept on the branch `workflow-v3`.

## License

MIT, see [LICENSE](LICENSE). Third-party code: [THIRD_PARTY.md](THIRD_PARTY.md).
