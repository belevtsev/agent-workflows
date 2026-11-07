# agent-workflows

An engineering workflow for Claude Code and Codex that takes a change from spec
to a reviewed, tested and verified commit.

[![CI](https://img.shields.io/github/actions/workflow/status/belevtsev/agent-workflows/ci.yml?branch=main&label=CI)](https://github.com/belevtsev/agent-workflows/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/github/license/belevtsev/agent-workflows)](LICENSE)
[![Release](https://img.shields.io/github/v/release/belevtsev/agent-workflows)](https://github.com/belevtsev/agent-workflows/releases/latest)

Coding agents often ship code that nobody reviewed, that has no tests, or that
is tested by the same agent that wrote it. This project splits a substantial
change across agents with separate contexts: one writes the spec, one designs,
one implements, another writes tests from the contract without seeing the code,
and another reviews. The `cw` command records each step, and on Claude Code a
hook blocks the commit until `cw task verify` passes.

## How it works

```mermaid
flowchart TD
    R([Your request]) --> SW[spec-writer]
    SW --> SR{spec-reviewer}
    SR -- findings --> SW
    SR -- reviewed spec --> AR[architect]
    AR --> C[/interface contract/]
    C --> IM[implementer]
    C --> TE[test-engineer]
    IM --> RUN[integrate and run the tests]
    TE --> RUN
    RUN --> RV{reviewer}
    RV -- findings --> IM
    RV -- accepted --> V{cw task verify}
    V -- problems --> RUN
    V -- passes --> G([git commit: hook lets it through])
```

The full flow is for substantial tasks, where at least two of these hold: the
change spans more than one package or component, its behaviour must match across
platforms or shells, it changes more than about 300 lines, or it has a design
choice with real alternatives. Smaller changes stay lightweight. The spec steps are skipped when a reviewed spec exists
or your request already states every behaviour and value.

| Agent | Gets | Produces | Model, effort (Claude Code) |
| --- | --- | --- | --- |
| spec-writer | your request and source documents | an [OpenSpec](https://github.com/Fission-AI/OpenSpec) change under `openspec/changes/` | Opus 5.5, high |
| spec-reviewer | the change, its sources and the code, not the writer's reasoning | findings graded by severity | Opus 5.5, high |
| architect | the reviewed spec or request, the repository | design record, interface contract with numbered requirements, implementation brief | Opus 5.5, xhigh |
| implementer | the brief and the contract | the smallest change that meets it, a self-review | Opus 5.5, high |
| test-engineer | the contract and its interface extract, never the implementation | test matrix, tests, requirement map | Opus 5.5, high |
| reviewer | the integrated source and a reviewer bundle | findings with evidence and severity | Opus 5.5, high |

Codex uses the models in
[model-policy.yaml](skills/task-orchestration/model-policy.yaml). Roles and
contracts are described in [docs/agents.md](docs/agents.md) and the
[contracts overview](skills/task-orchestration/references/contracts/README.md).

## Quick start

Install `cw`, the command the skills call to record tasks and run checks. It
needs Go 1.27 or newer:

```sh
go install github.com/belevtsev/agent-workflows/cmd/cw@main
```

Use `@main`, not `@latest`. Release archives with checksums are an alternative;
see [get cw](docs/operations.md#get-cw).

**Claude Code.** Add the marketplace:

```sh
claude plugin marketplace add belevtsev/agent-workflows
```

Install the plugin, then start a new Claude Code session:

```sh
claude plugin install agent-workflows@agent-workflows
```

**Codex.** Add the marketplace:

```sh
codex plugin marketplace add belevtsev/agent-workflows
```

Then run `codex`, open `/plugins`, install `agent-workflows`, and start a new
session. If you also want the shared instruction block in `AGENTS.md`, use
`cw install --host codex` instead of the plugin; see
[operations](docs/operations.md#codex-install-and-uninstall).

Check that `cw` can read your repository:

```sh
cw doctor --summary --repo "$PWD"
```

## Your first task

You ask for the change, answer questions and approve the commit. The agent runs
everything else, including the `cw` commands below. You can run them yourself to
inspect a task.

1. Open your repository in Claude Code and ask for the change. Naming the skill
   starts the workflow; on either host a plain request works too, because the
   skill loads when the task matches.

   ```text
   /agent-workflows:task-orchestration Add a --json flag to the export command
   ```

2. The agent checks the repository with `cw doctor`, decides whether the task is
   substantial, and records a plan of requirements and the checks that prove
   them ([plan format](skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task)).
   It prints the task directory:

   ```sh
   cw task start --repo /absolute/path/to/repo --plan plan.json --summary
   ```

3. The spec writer and spec reviewer produce a reviewed change under
   `openspec/changes/`. The agent brings you any open questions.
4. The architect fixes the interface contract. The implementer and the test
   engineer then work in parallel, isolated from each other.
5. Checks run through `cw`, so the record holds the real result:

   ```sh
   cw task run --task <task-dir> --check unit --summary
   ```

6. An independent reviewer reviews the integrated change. Then the agent runs
   the gate:

   ```sh
   cw task verify --task <task-dir> --summary
   ```

7. You approve the commit. It goes through once verify has passed on the current
   source.

## The verify gate

On Claude Code the plugin runs a hook before every `git commit` and `git push`.
It finds the newest substantial task record for the repository, started in the
last 14 days. If that record has no passing `cw task verify` on the current
source, the hook denies the command. Repositories without a task record are not
affected. Committing the verified files does not reopen the record; editing one
does.

**See why a commit is blocked.** The denial names the task and the command to
run. Its output lists every problem, such as a missing independent review or an
unmet requirement:

```sh
cw task verify --task <task-dir> --summary
```

**Abandon a stale task.** A record stops gating 14 days after it was started. To
drop it sooner, move its directory out of the task store
(`$XDG_STATE_HOME/agent-workflows/workflow-tasks`, else
`~/.local/state/agent-workflows/workflow-tasks`). The next newest record for the
repository, if any, then decides.

**Turn the gate off.** Set the plugin option `enforce_verify` to `false`, with
`/plugin configure agent-workflows@agent-workflows` in a session or from a
shell, then restart Claude Code:

```sh
echo '{"enforce_verify":"false"}' | claude plugin configure agent-workflows@agent-workflows --values-stdin
```

The gate fails open: without `cw` on `PATH`, or on any internal error, the
command goes ahead. Codex has no such hook; there the skill only claims
completion after `cw task verify` passes. Details are in
[docs/hooks.md](docs/hooks.md).

## Language packs

Core skills are language-neutral, and language practice comes from opt-in
packs. Go is the first: today it is the `cc-skills-golang` plugin in the same
marketplace, plus a few Go skills still in core. On Claude Code:

```sh
claude plugin install cc-skills-golang@agent-workflows
```

On Codex, install `cc-skills-golang` from `/plugins`. Coming: the Go pack as its
own plugin with a `cw lang` command, then a UI pack. See the
[language pack design](docs/design/language-packs.md).

## Skills

`task-orchestration` runs substantial changes and `code-review` reviews diffs;
the other skills are specialists those two point to. Most load automatically
when a task matches, and a few run only when you name them; see the
[skill catalog](skills/README.md).

## Configuration

Paths, state and config directories, and the model policy lookup are in
[operations](docs/operations.md).

## Advisers (optional)

At decision checkpoints `cw consult` can ask hosted advisers the same questions,
and the coordinator decides from source evidence; see
[advisers](docs/advisers.md).

## Upgrading from v2

Run `cw migrate-v2 --dry-run`, then `cw migrate-v2`, then install the plugins;
see [moving from v2](docs/operations.md#moving-from-v2).

## Contributing, security, license

See [CONTRIBUTING](CONTRIBUTING.md) for building and testing, and the
[docs index](docs/README.md) for everything else. Report vulnerabilities
privately as described in [SECURITY](SECURITY.md). Licensed under the
[MIT License](LICENSE).
