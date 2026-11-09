# agent-workflows

An engineering workflow for Claude Code and Codex that takes a change from spec
to a reviewed, tested and verified commit.

[![CI](https://img.shields.io/github/actions/workflow/status/belevtsev/agent-workflows/ci.yml?branch=main&label=CI)](https://github.com/belevtsev/agent-workflows/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/github/license/belevtsev/agent-workflows)](LICENSE)
[![Release](https://img.shields.io/github/v/release/belevtsev/agent-workflows)](https://github.com/belevtsev/agent-workflows/releases/latest)

Coding agents often ship code that nobody reviewed, that has no tests, or that
is tested by the agent that wrote it. This project splits a substantial change
across agents with separate contexts: one writes the spec, one designs, one
implements, another writes tests from the contract without seeing the code, and
another reviews. The `cw` command records each step, and on Claude Code a hook
blocks the commit until `cw task verify` passes.

## How it works

<img src="docs/diagrams/workflow-overview.svg" alt="Flow: spec-writer and reviewer (spec), architect and reviewer (design), implementer in parallel with test-engineer and reviewer (tests), integrate and run, reviewer (code, on the integrated source against the contract, the design and coverage) and implementer, cw task verify, then the coordinator publishes with cw publish, with github-worker as the fallback. Findings loop back to each author; optional advisers join at four decision points." width="820">

Each stage's review findings go back to its author, capped per stage. Advisers,
when available, join only at four decision points after the before checkpoint;
without them the loop runs unchanged, and the coordinator decides either way.
The full flow is for substantial tasks: at least two of more than one package
or component changed, behaviour that must match across platforms or shells,
more than about 300 changed lines, or a design choice with real alternatives.
Smaller changes stay lightweight. The spec stage is skipped when a reviewed
spec exists or the request states every behaviour.

| Agent | Gets | Produces | Claude Code model |
| --- | --- | --- | --- |
| spec-writer | source documents | an [OpenSpec](https://github.com/Fission-AI/OpenSpec) change folder, a change log | Opus 5.5, high |
| architect | the reviewed spec or request | design, interface contract, implementation brief | Opus 5.5, xhigh |
| implementer | brief and contract | the change, a self-review | Opus 5.5, high |
| test-engineer | contract and interface, never the code | test matrix, tests, requirement map | Opus 5.5, high |
| adversarial-tester (opt-in) | contract, design, the integrated source and its coverage | tests that fail on the code, each a finding | Opus 5.5, high |
| reviewer | per stage: the spec; the design; the tests, never the implementation; or the integrated source and its changed files, checked against the interface contract, the design and, when one exists, the final check's coverage profile | graded findings with evidence; a failing test for each behaviour finding | Opus 5.5, high |
| github-worker | verified facts, your approval | commit, push, PR when `cw publish` does not fit (hunk-level scopes, fork PRs, conversions) | Sonnet 5.5, medium |

The table is not a one-way pipeline: every review loops back to its stage's
author, except that a code review finding about a test goes to a fresh test
engineer, by requirement ID, contract-level inputs and expected outcome only;
the implementer and the test engineer run in parallel from the same
contract. Small tasks skip this staged flow: their author writes the tests and
a reviewer checks them.

Codex uses [model-policy.yaml](skills/task-orchestration/model-policy.yaml).
See [agents](docs/agents.md) and the
[role contracts](skills/task-orchestration/references/contracts/README.md).

## Quick start

Install `cw`, which records tasks and runs checks (Go 1.27 or newer), from
`@main` or a [release archive](docs/operations.md#get-cw):

```sh
go install github.com/belevtsev/agent-workflows/v3/cmd/cw@main
```

`@latest` works once the first v3 release tag exists.

**Claude Code.** Add the marketplace:

```sh
claude plugin marketplace add belevtsev/agent-workflows
```

Install the plugin, then start a new session:

```sh
claude plugin install agent-workflows@agent-workflows
```

**Codex.** Add the marketplace:

```sh
codex plugin marketplace add belevtsev/agent-workflows
```

Then run `codex`, open `/plugins`, install `agent-workflows` and start a new
session. For the `AGENTS.md` instruction block, use `cw install --host codex`
instead ([details](docs/operations.md#codex-install-and-uninstall)).

Check that `cw` can read your repository:

```sh
cw doctor --summary --repo "$PWD"
```

## Your first task

You ask, answer questions and approve the commit. The agent runs the rest,
including the `cw` commands below; you can run them yourself to inspect a task.

1. Ask for the change in your repository. Naming the skill starts the workflow;
   a plain request also works, because the skill loads when the task matches.

   ```text
   /agent-workflows:task-orchestration Add a --json flag to the export command
   ```

2. The agent runs `cw doctor`, decides whether the task is substantial and
   records the requirements and the checks that prove them
   ([plan format](skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task)):

   ```sh
   cw task start --repo /absolute/path/to/repo --plan plan.json --summary
   ```

3. The spec writer and a reviewer produce a change under
   `openspec/changes/`. The agent brings you the open questions.
4. The architect's design passes a design review, then the implementer and the
   test engineer work in parallel, isolated; a reviewer checks the tests
   against the contract before integration.
5. Checks run through `cw`, so the record holds the real result:

   ```sh
   cw task run --task <task-dir> --check unit --summary
   ```

6. An independent reviewer reviews the code, then the agent runs the gate:

   ```sh
   cw task verify --task <task-dir> --summary
   ```

7. You approve the commit. It goes through once verify passes on the current
   source.

## The verify gate

On Claude Code the plugin checks every `git commit` and `git push` it can find
in a Bash command, in any form. Each open substantial task for the repository
(not abandoned, touched in the last 14 days) blocks it until `cw task verify`
passes on the current source. Editing a verified file reopens the task. With no
task at all, a large change gets a prompt suggesting `cw task start`.

`cw publish` commits a verified task's files, pushes them and creates or edits
the pull request, and applies the same gate itself, so it holds under Codex as
well. The coordinator runs it with your approval; the command never approves
anything on its own. See the
[publication reference](skills/task-orchestration/references/github-worker.md).

When a task applies the `go` pack, verify also needs passed direct `go test`
runs, with allowlisted flags only (no `-run` or `-skip`): one with `-race`
covering each changed package (a plain run, with a recorded limitation,
where `-race` cannot run on the host), one covering each package whose tests
link a changed package, and `go test ./...` after a change to module or
vendored files, build constraints, generated code, embedded files or shared
testdata. A pass after a failed run of the same check at the same source
and `GOFLAGS` earns no credit until the source changes, and skipped tests
are listed. A task keeps the rule
it recorded at start; see [evidence rules](skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task).
The `go` pack is selected by default. A task records it when `go.mod` or
`go.work` is at the top of the selected path at start, and the rule then checks
packages whenever `go.mod` is at the top at verify. A task that started with
neither file gets no Go rule, even if it creates `go.mod` later. `cw pack add`
and `cw pack remove` affect only tasks started afterwards; an open task keeps
the pack set it recorded. `cw pack list` shows the selection.
`cw pack remove go` writes an explicit `packs.json`, which drops the Go rule for
all of that user's repositories and leaves only the fallback rule (any passed
check); `cw pack add go`, or deleting `packs.json`, restores it.

**See why a commit is blocked.** The denial names each blocking task. This lists
its problems, such as a missing review or an unmet requirement:

```sh
cw task verify --task <task-dir> --summary
```

**Give a task up** so it stops blocking:

```sh
cw task abandon --task <task-dir> --reason "replaced by a new task"
```

**Turn the gate off.** Set the plugin option `enforce_verify` to `false`
(and `small_task_prompt` to `false` for the prompt), then restart Claude Code:

```sh
echo '{"enforce_verify":"false"}' | claude plugin configure agent-workflows@agent-workflows --values-stdin
```

The gate fails open: without `cw` on `PATH`, or on an internal error, the
command goes ahead. Codex has no hook; there the skill claims completion only
after verify passes. Details: [hooks](docs/hooks.md).

## Language packs

Core skills are language-neutral; language practice comes from opt-in packs.
Go is first: today the `cc-skills-golang` plugin in the same marketplace, plus a
few Go skills in core. On Codex, install it from `/plugins`; on Claude Code:

```sh
claude plugin install cc-skills-golang@agent-workflows
```

Coming: the Go pack as its own plugin with `cw lang`, then a UI pack. See the
[design](docs/design/language-packs.md).

## Skills

`task-orchestration` runs substantial changes and `code-review` reviews diffs;
the rest are specialists those two point to. Most load when a task matches;
some run only by name. See the [skill catalog](skills/README.md).

## Configuration

Paths, state, config, the model policy lookup and
[cleaning up a v2 installation](docs/operations.md#cleaning-up-a-v2-installation):
[operations](docs/operations.md).

## Advisers (optional)

`cw consult` can ask hosted advisers at decision checkpoints; the coordinator
decides from source evidence. See [advisers](docs/advisers.md).

## Contributing, security, license

See [CONTRIBUTING](CONTRIBUTING.md) and the [docs index](docs/README.md).
Report vulnerabilities as described in [SECURITY](SECURITY.md). Licensed under
the [MIT License](LICENSE).
