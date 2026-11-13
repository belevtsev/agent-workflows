# agent-workflows

An engineering workflow for Claude Code and Codex that takes a change from spec
to a reviewed, tested and verified commit.

[![CI](https://img.shields.io/github/actions/workflow/status/belevtsev/agent-workflows/ci.yml?branch=main&label=CI)](https://github.com/belevtsev/agent-workflows/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/github/license/belevtsev/agent-workflows)](LICENSE)
[![Release](https://img.shields.io/github/v/release/belevtsev/agent-workflows?include_prereleases)](https://github.com/belevtsev/agent-workflows/releases)

Coding agents often ship code that nobody reviewed, that has no tests, or that
is tested by the agent that wrote it. This project splits a substantial change
across agents with separate contexts: one writes the spec, one designs, one
implements, another writes tests from the contract without seeing the code, and
another reviews. The `cw` command records each step, and on Claude Code a hook
asks the agent to stop and pass `cw task verify` before it commits; see
[what the gate does not stop](#the-verify-gate). `cw` runs on macOS and Linux
only ([Quick start](#quick-start)).

## How it works

<img src="docs/diagrams/workflow-overview.svg" alt="Flow of a substantial task: spec-writer and reviewer (spec, skipped with a reviewed spec or a precise request), architect and reviewer (design and contract), implementer in parallel with test-engineer and reviewer (tests), integrate and run, then reviewer (code, on the integrated source against the contract, the design and coverage) and implementer, with the adversarial-tester beside the code review if the plan opts in, whose failing tests go to the implementer, then cw task verify, then the coordinator publishes with cw publish, with github-worker as the fallback when cw publish does not fit. Findings loop back to each author; optional advisers join at four decision points." width="820">

A substantial task runs the `lite` profile by default, with four agents: the
implementer and the test engineer work in parallel from one contract, a
reviewer checks the tests, the coordinator integrates and runs them, and a
reviewer checks the code before `cw task verify`. `standard` adds the architect
and a design review, for an open design choice or a public interface change;
`strict` also adds the spec writer and a spec review, when you ask or the change
touches security, data integrity or concurrency
([profiles](docs/operations.md#profiles)). The diagram shows `strict`.
Each stage's review findings go back to its author, capped per stage. Advisers,
when available, join only at four decision points after the before checkpoint;
without them the loop runs unchanged, and the coordinator decides either way.
The staged flow is for [substantial tasks](global/AGENTS.md#substantial-tasks).
For any other task the agent writes the tests itself, checks its diff against
the Code Review Rules before reporting, and commits or pushes only with your
authorization; it dispatches no workers.

| Agent | Gets | Produces | Claude Code model alias |
| --- | --- | --- | --- |
| spec-writer (`strict`) | source documents | an [OpenSpec](https://github.com/Fission-AI/OpenSpec) change folder, a change log | `opus`, high |
| architect (`standard`, `strict`) | the reviewed spec or request | design, interface contract, implementation brief | `opus`, xhigh |
| implementer | brief and contract | the change, a self-review | `opus`, high |
| test-engineer | contract and interface, never the code | test matrix, tests, requirement map | `opus`, high |
| adversarial-tester (opt-in) | contract, design, the integrated source and its coverage | tests that fail on the code, each a finding | `opus`, high |
| reviewer | per stage: the spec; the design; the tests, never the implementation; or the integrated source and its changed files, checked against the interface contract, the design and, when one exists, the final check's coverage profile | graded findings with evidence; a failing test for each behaviour finding | `opus`, high |
| github-worker | verified facts, your approval | commit, push, PR when `cw publish` does not fit (hunk-level scopes, fork PRs, conversions) | `sonnet`, medium |

The table is not a one-way pipeline: every review loops back to its stage's
author, except that a code review finding about a test goes to a fresh test
engineer, by requirement ID, contract-level inputs and expected outcome only;
the implementer and the test engineer run in parallel from the same
contract.

An alias resolves to Claude Code's current release of that model; the
[Claude Code policy](skills/task-orchestration/model-policy.claude.yaml) sets
each role. Codex uses [model-policy.yaml](skills/task-orchestration/model-policy.yaml).
See [agents](docs/agents.md) and the
[role contracts](skills/task-orchestration/references/contracts/README.md).

## Quick start

**Requirements.** macOS or Linux (Windows is not supported), Claude Code or
Codex, git and Go; the [host requirements](docs/operations.md#host-requirements)
table gives the minimum versions and why. The session-start hook says when it finds an
older client, or a plugin and `cw` whose versions differ
([session start](docs/hooks.md#session-start)).

**Languages.** Go modules (`go.mod` at the top of the task's path) get the
package-level `go test` rule in [`cw task verify`](#the-verify-gate); other
repositories need any one passed check.

Install `cw`, which records tasks and runs checks, from the release archive for
your system on the
[v3.0.0-rc.2 release page](https://github.com/belevtsev/agent-workflows/releases/tag/v3.0.0-rc.2)
([steps](docs/operations.md#get-cw)), or build that tag with Go 1.27.1 or newer:

```sh
go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.2
```

The project owner pushes each release tag; the release page and `go install`
work once `v3.0.0-rc.2` is tagged.

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

Then run `codex`, open `/plugins`, install `agent-workflows`, trust its
hooks in `/hooks` and start a new session. Until you trust them, Codex runs
none of the hooks and the plugin gives the skills only: no conventions and no commit
hook. Trust them again after an update that changes them, such as the one that
added the Codex hooks file. The alternative, `cw install --host codex`, adds archify, typesafe-ai and
the Docker skills and writes the `AGENTS.md` instruction block, but installs no
hooks. [What each shape gives](docs/operations.md#codex).

Check that `cw` can read your repository:

```sh
cw doctor --summary --repo "$PWD"
```

It prints `"status":"ok"` when nothing blocks the workflow; `problems` names
what does. Optional tools are only counted, in `optional_missing`; `--all`
lists them ([diagnostics](docs/operations.md#global-project-diagnostics-and-evidence)).

## Your first task

The [quick start](docs/quickstart.md) takes one task by hand, in ten minutes, from `cw task init` to a verified
commit. In daily use you ask, answer questions and approve the commit, and the agent runs those commands:

1. Ask in your repository, for example `/agent-workflows:task-orchestration Add retries to the HTTP client`.
2. The agent writes the plan ([starter plan](docs/operations.md#starter-plan)) and starts the task under a [profile](docs/operations.md#profiles).
3. Workers implement and test the change, and checks run through `cw task run`, so the record holds the real result.
4. An independent reviewer reviews the change, then `cw task verify` runs the gate; past 80% of the budget the task stops and asks you.
5. You approve the commit, which goes through once verify passes on the current source.

## The verify gate

The gate is a guardrail for a cooperative agent that skipped a step, not a
defence against an agent that sets out to bypass it. On Claude Code the plugin
reads each Bash command for `git commit` and `git push`. While an open
substantial task for the repository (not abandoned, touched in the last 14
days) has no passing `cw task verify` on the current source, the hook denies
the command and asks the agent to stop and verify first. Editing a verified
file reopens the task. With no task at all, a large change gets a prompt
suggesting `cw task start`.

It does not stop an edited or damaged task store, a fake toolchain on `PATH`,
or `cw task abandon` run by the agent from a script (the hook asks you before
one it can see), nor git run by a script, an alias or a command form the hook
does not parse, unless `cw gate install` added git's own pre-commit,
pre-push and pre-merge-commit hooks to the repository
([git hooks](docs/hooks.md#installing-the-git-hooks)). See the
[threat model](SECURITY.md#threat-model) and the
[known bypasses](docs/hooks.md#known-bypasses).

`cw publish` commits a verified task's files, pushes them and creates or edits
the pull request, and applies the same gate itself, so it holds under Codex as
well. The coordinator runs it with your approval; the command never approves
anything on its own. See the
[publication reference](skills/task-orchestration/references/github-worker.md).

When a task applies the `go` pack, verify also needs passed direct `go test`
runs for the host's `GOOS`/`GOARCH`, with allowlisted flags only (no `-run`
or `-skip`, in argv or in the `GOFLAGS` that `go env` reports) and, with cgo
on, a C toolchain `cw` verified
([the C toolchain](docs/operations.md#the-c-toolchain-of-a-go-check)): one with `-race`
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

A task that merges its base branch covers the merged packages by adding a
check with `cw task add-checks` after `git merge --no-ff <base>`; the plan
itself never changes. See
[Merging the base branch during a task](docs/operations.md#merging-the-base-branch-during-a-task).

**See why a commit is blocked.** The denial names up to three blocking tasks
and counts the rest. This lists a task's problems, such as a missing review or
an unmet requirement:

```sh
cw task verify --task <task-id>
```

**Give a task up** so it stops blocking:

```sh
cw task abandon --task <task-id> --reason "replaced by a new task"
```

**Let git run the gate too.** `cw gate install` writes git's pre-commit,
pre-push and pre-merge-commit hooks for one repository, so a commit, merge
commit or push from a script, an alias or a terminal meets the same gate;
`cw gate uninstall` removes them:

```sh
cw gate install --repo /path/to/repo
```

`cw` never writes them unasked. While a task is open in a repository without
them, the session-start context suggests the command once per task and `cw doctor` reports
`git_hooks` as `missing`
([installing the git hooks](docs/hooks.md#installing-the-git-hooks)).

**Turn the gate off.** Set the plugin option `enforce_verify` to `false`
(and `small_task_prompt` to `false` for the prompt), then restart Claude Code:

```sh
echo '{"enforce_verify":"false"}' | claude plugin configure agent-workflows@agent-workflows --values-stdin
```

On Codex, start Codex with `CLAUDE_PLUGIN_OPTION_ENFORCE_VERIFY=false` (and
`CLAUDE_PLUGIN_OPTION_SMALL_TASK_PROMPT=false`) in its environment instead.

The gate fails open: without `cw` on `PATH`, or on an internal error, the
command goes ahead. Codex runs the same hooks, from its own hooks file, once you trust them in `/hooks`;
it cannot ask, so there the hook denies where Claude Code asks, except that
the asks about a task record (damaged or from a newer `cw`) or a task store
it cannot read or locate let the command go ahead. Details:
[hooks](docs/hooks.md#codex).

## Language packs

A pack tells `cw task verify` what a changed task in one language must have
run. Packs are built into `cw`; today there is one, `go`, selected by default.
`cw pack list` shows the built-in packs and your selection, and
`cw pack add <id>` and `cw pack remove <id>` change it for tasks started
afterwards (`cw lang` is an alias). See [packs](docs/packs.md), which also
covers adding a pack.

The Go skills are separate: the `cc-skills-golang` plugin in the same
marketplace, plus a few Go skills in core. On Codex, install it from
`/plugins` (`cw install --host codex` already links it); on Claude Code:

```sh
claude plugin install cc-skills-golang@agent-workflows
```

## Skills

`task-orchestration` runs substantial changes and `code-review` reviews diffs;
the rest are specialists those two point to. Most load when a task matches;
some run only by name. See the [skill catalog](skills/README.md).

## Configuration

Paths, state, config and the model policy lookup:
[operations](docs/operations.md).

## Advisers (optional)

`cw consult` can ask hosted advisers at decision checkpoints; the coordinator
decides from source evidence. See [advisers](docs/advisers.md).

## Contributing, security, license

See [CONTRIBUTING](CONTRIBUTING.md) and the [docs index](docs/README.md).
Everyone who takes part follows the [code of conduct](CODE_OF_CONDUCT.md).
Report vulnerabilities as described in [SECURITY](SECURITY.md). Licensed under
the [MIT License](LICENSE).
