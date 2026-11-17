<div align="center">

# Kitsu

<img src="docs/brand/kitsu-banner.png" alt="Kitsu: a fluffy Pomeranian in a red beanie holds a checked code-change card. Coding agents, working together." width="100%">

**Coding agents, working together.**

A workflow toolkit for coordinating coding agents through specification, implementation, testing, and review.
Seven specialist roles, shared task records, and token reporting, with a Git gate to check work before commits and pushes.

[![CI](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/belevtsev/kitsu/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Runs on Linux, macOS and Windows](https://img.shields.io/badge/runs%20on-Linux%20%7C%20macOS%20%7C%20Windows-lightgrey)](#install)

[The roles](#the-roles) · [Quick start](#quick-start) · [CLI](#find-a-command) · [Skills](#use-your-existing-skills) · [Advisers](#ask-an-adviser) · [Install](#install) ·
[For a coding agent](#for-a-coding-agent) · [The Git gate](#how-the-gate-decides) · [State](#state) · [Documents](#documents)

</div>

| Part | What it does |
| --- | --- |
| **Agent coordination** | A coordinator in your coding-agent host delegates work to seven specialist roles, with a defined order, review handoffs, model choices, and limits on what each role may change. |
| **Shared task records** | Requirements, design decisions, tasks, tests, and review findings give the agents a common record to work from. `kitsu task` checks that record and reports the task's stage. |
| **Existing skills** | Select skills from local repository, personal, and installed plugin sources for each role, while keeping their original files and references. |
| **Token reporting** | `kitsu usage show` reports recorded token usage by task and role, including the coordination overhead. |
| **Compact context (opt-in)** | Typed reads/searches, saved worker briefs, managed dispatch/returns, stage write checks and bounded state snapshots/deltas, with original evidence references. |
| **Advisers (experimental)** | Optional Jev, SubQ Snap, and CLI consultations, with recorded answers and decisions. None are enabled by default. |
| **Git gate** | As a supporting check, Git hooks allow commits and pushes only when the checks declared in `kitsu.toml` have passing evidence for that content. |

Your agent host runs the coordinator and delegates to the roles. Kitsu supplies the workflow instructions,
role definitions, record checks, and usage reports. The Git gate supports that workflow and can also be used on its own.

> [!NOTE]
> Kitsu is in early development. [Install](#install) with one command;
> [State](#state) covers validation and current limits.

## The roles

<img src="docs/diagrams/task-workflow.svg" alt="The task workflow. Full tasks start with specification and design, each independently reviewed and approved by you. Both paths continue through tests, code, review and a gated commit. Repository opt-in behavioral verification follows finished worker intervals: plan targets and gaps, verify targets, publish a summary, then satisfy full checks using local evidence. Experimental advisers remain off by default; explicit access and binding allow consultation and recorded decisions without replacing approval." width="100%">

A task that needs a requirement written or changed takes the full path: specification, design, tests, code, review
and commit. A task whose intended behaviour is already clear and does not change starts at the tests. You approve
the specification and the design, and the commit goes through the gate.

Advisers are experimental and off by default. [Opting in](docs/advisers.md) adds consultations;
required adviser checkpoints are a separate choice. Neither replaces review or your approval.

| Role | Its job | It may change |
| --- | --- | --- |
| `lookup` | Finds files, facts and existing specifications, and reports where they are. | nothing |
| `spec-writer` | Writes the requirements and their scenarios from the request and its sources. | `proposal.md` and `specs/` of the task's change folder |
| `architect` | Designs the building blocks, their dependencies and the tasks. | `design.md` and `tasks.md` of the task's change folder |
| `quality-engineer` | Writes the tests of the scenarios before the code exists. | the test paths, and `tests.md` of the task's change folder |
| `developer` | Writes the code that makes the tests pass, within the design. | any other path except `kitsu.toml`, `kitsu-roles.toml`, `kitsu-advisers.toml` and `openspec/`; it may add a test, and may not change or remove one |
| `reviewer` | Reviews a specification, a design or code that another role wrote, and reports findings. | nothing |
| `git` | Runs the checks and makes the commit through the gate. | nothing |

`kitsu role list` prints the seven roles, each with its model, its effort and what it may write. Use
`kitsu role set` to save model, reasoning, and existing-skill selections in `kitsu-roles.toml`. The texts name no language and no build tool; the commands of a repository come from its
`kitsu.toml`.

`kitsu role list --host codex` or `kitsu role list --host claude` shows a compact table of role, model,
reasoning effort, selected skills, and allowed advisers. Add `--task <task>` to show a saved task's adviser access.
Optional, experimental advisers use native APIs or authenticated CLIs configured in `kitsu-advisers.toml` or your personal
configuration. Roles return questions to the coordinator, which runs `kitsu adviser ask` and records
how the answer was addressed. None are enabled by default; repository role access is an explicit opt-in.
Adviser answers replace no review or approval.

- [Roles](docs/roles.md): what each role does and may change.
- [Skills](docs/skills.md): discover existing skills and select them per role on either host.
- [Advisers](docs/advisers.md): definitions, role access, advice and decisions, and required checkpoints.
- [The task workflow](docs/workflow.md): the two paths, the record of a task under `openspec/changes/<task>/`, and
  what `kitsu task` checks.
- [Tokens](docs/tokens.md): what is counted, and what "workflow tokens" mean.

## Quick start

1. [Install](#install) the CLI and [add the plugin](#for-a-coding-agent) to your coding-agent host.
2. Run `kitsu agent check --host codex` or `kitsu agent check --host claude` and follow any setup steps.
   On Codex, review and trust the current Kitsu hooks in `/hooks`; local diagnostics cannot prove that trust or hook execution.
3. Run `kitsu role list --host codex` or `kitsu role list --host claude` to inspect models, reasoning, skills, and advisers.
   [Set a specialist's model or reasoning](docs/roles.md#changing-a-model) with `kitsu role set`; your host selects the coordinator.
   Optionally [select existing skills](#use-your-existing-skills) for a role.
4. From a terminal, run `kitsu task start "Add an order CSV export" --host codex` (or `--host claude`).
   This explicitly opens the selected interactive host with Kitsu's task skill and your request.
   In an existing session, invoke `/kitsu:task` on Claude Code or ask Codex to use Kitsu's installed `task` skill.
   The host's coordinator chooses the workflow and delegates each stage.
5. Review the proposed specification and design when the task changes behavior, then follow implementation,
   tests, and review through the shared task record.

You can inspect the role instructions and a recorded task from the terminal:

```sh
kitsu role list --host codex
kitsu role text coordinator
kitsu task show <task>
kitsu usage show --task <task>
```

The [task workflow](docs/workflow.md) explains the full path and the shorter path for work whose behavior is already clear.
Task records and token totals appear as the host runs the workflow. The inspection commands above launch no agents.
For desktop use or incomplete CLI setup, run `kitsu task start "Add an order CSV export" --host codex --print`
and paste the printed handoff into your selected host. `--json` returns the prompt, proposed argv, and local diagnostics.
Both modes execute nothing and create no task record. The [CLI guide](docs/cli.md#check-setup-and-start-a-task)
explains ready, broken, and unknown setup evidence. The supporting Git gate can be configured separately.

### Send smaller observations

```sh
kitsu task start "Fix login retries" --host codex --compact
```

Compact mode selects observations: native host reads for short whole files and scoped searches
with small output, typed collection for broad or noisy retrieval, and bounded noisy check output.
Agents expand retained evidence when needed and keep original role instructions and skills
uncompressed. It ships in the existing binary, preserving reviews, approvals and check evidence.
[How to use it](docs/compact-context.md), [research and tradeoffs](docs/context-research.md).
Worker packets include their role and reading policy; keep coordinator guides out of
repeated assignments and use ordinary output for quiet checks. See the
[offline delivery checks](docs/benchmarks/compact-delivery-report.md); these measure bytes and calls, not provider tokens.
Fitting failed logs now return complete evidence, and QE packets include executable
check/log recipes and scoped optional-tool observations. For a smaller native
handoff, use `task prepare --dispatch --view launch`; `task dispatch <id>` reads
the original full response. The [handoff how-to](docs/compact-response-cycles.md)
explains safe sequential finish/preparation and the offline quality checks.
The [latest paid pair](docs/benchmarks/response-cycles-paid-report.md) recorded 1,655,932 ordinary and 1,467,207 compact tokens,
with all ten sessions closed and both attempts passing protected quality checks. This one pair does not establish general savings or quality equivalence.
The [revised paid pair](docs/benchmarks/compact-delivery-paid-report.md) recorded 1,982,129 ordinary and 1,872,131 compact tokens,
with passing frozen checks. Uncached input increased; no general quality equivalence or lower bill is established.
The [24-run Codex pilot](docs/benchmarks/codex-compact-pilot-report.md) found passing frozen tests
in both modes, additional defects on review, and less workflow completion with compact mode
under an eight-minute limit. Complete-task token savings remain unestablished.
The separate [selective evaluation](docs/benchmarks/selective-compact-report.md) includes offline
replay overhead and a rejected v2 charset reference. Its bounded canary stopped the first ordinary
run at 3.26M captured tokens; three runs stayed unstarted, leaving no new on/off savings evidence.
The [offline verification guide](docs/verification.md) covers historical defect calibration,
generated evidence-equivalence checks and bounded fuzzing without model calls.
Repositories can also opt in to [task behavioral verification](docs/verification.md#verify-task-behavior):
plan relevant declared checks, exercise actual behavior and retain source-bound attempt summaries before the final gate checks.

For an opted-in coordinator, common reads need no request JSON:

```sh
kitsu context read README.md --start 1 --end 30
kitsu context search 'retry' internal
kitsu task prepare fix-login --role developer --stage code --round 1 \
  --host codex --workflow small --assignment /absolute/path/assignment.txt
kitsu task finish <dispatch-id> --expect-checks pass
```

`prepare` returns a saved brief's path/hash, model, reasoning and baseline. The host starts
the fresh worker from that complete brief; `finish` checks its writes and existing evidence
without running tests again. Finish the prior interval before preparing another worker.
These commands launch no model and replace no review or approval. See the
[worked handoff and recovery steps](docs/compact-context.md#prepare-and-finish-a-worker).

For an opted-in coordinator, `task prepare ... --dispatch` also generates a native
launch description with the original packet reference and return shape. The host starts
the fresh worker; `task return` retains bounded claims separately from observed local
facts without finishing the interval. See [coordinator handoffs](docs/coordinator-overhead.md)
for examples, reduced polling/bookkeeping guidance and offline measurement limits.

Inspect coordinator state without repeatedly printing complete task records:

```sh
kitsu task show fix-login --compact
kitsu task show fix-login --since <snapshot-id>
```

The first command returns a bounded snapshot of stage, active interval, checks,
open recorded findings, policy keys and missing prerequisites. The second returns
only changed sections. Expand original references when needed; snapshots certify
no task completion. See [state snapshots and deltas](docs/coordinator-state.md).

Optional [workflow insights](docs/workflow-insights.md) add `usage show --diagnostics`,
source-linked uncertainty and troubleshooting notes, and task budget estimates or
sourced limits. Unknown usage remains unknown; recorded budgets authorize no paid
work and enforce no runtime cap.

The latest [Codex compact comparison](docs/benchmarks/workflow-insights-paid-report.md)
records actual coordinator and worker tokens on one real task. Both attempts
passed the frozen quality checks; compact used 21.1% more captured tokens in this
pair. Compact remains opt-in.

### Find a command

```sh
kitsu
kitsu help skill attach
kitsu config set host codex
kitsu role edit developer
```

The root and preferred groups show help when invoked alone. Help works before repository or
host setup; only explicit `role edit` and `config edit` open terminal forms. Save a private host
preference once, or override it with `--host`. The [CLI guide](docs/cli.md) covers the command
groups, accessible editors, completion, JSON, and quiet compatibility aliases.

Change a role's selection without editing TOML:

```sh
kitsu role set developer --host codex --model gpt-6.1-sol --reasoning max
kitsu role set developer --host claude --model opus --reasoning high
kitsu role set reviewer --host codex --reasoning high
kitsu role reset developer --host codex
```

Omit an option to keep that value. Reset removes repository overrides for that role and host;
model defaults and any personal skill selection apply again.

### Use your existing skills

List skills already available in supported local directories for your host, then copy an id from
the catalog to a role. This example assumes the repository contains the named skill:

```sh
kitsu skill list --host codex
kitsu skill attach codex:repo:.agents/skills/project-review --role reviewer --host codex
kitsu role list --host codex
kitsu skill resolve reviewer --host codex --json
kitsu skill clear --role reviewer --host codex
```

Discovery and resolution read the original `SKILL.md` files and preserve sibling scripts and
references. They install nothing and execute no skill code or model. The coordinator resolves
before each new worker and tells it to load the selected original instructions; a selection
receipt alone does not prove loading. Skills change no role write limit or approval rule.

The [skill guide](docs/skills.md) covers personal selections with `--global`, repository overrides,
duplicate names, explicit roots, portable receipts, and examples for your own Go, C++, and
JavaScript skills. Unsupported host catalog layouts need a local root; catalog completeness and
live host loading remain unverified.

### Ask an adviser

Advisers are an optional experiment. The normal workflow needs no adviser configuration, API
key, or adviser checkpoint. No adviser is enabled by default, and their effect on code quality,
cost, and completion time has not been evaluated. The examples below explicitly opt in.

For built-in **TypeSafe Jev** and **SubQ Snap**, run `kitsu adviser providers` to see presets,
set the provider's API-key environment variable, and explicitly configure role access.
The [native adviser guide](docs/advisers.md#built-in-decision-advisers) shows the short TOML
definitions and choice, score, and yes/no requests. Existing CLI advisers work alongside them.

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
kitsu role list --host codex
kitsu adviser bind advice-demo --workflow small
```

Ask a question from the terminal, or let the coordinator broker it for a role. This command invokes
the authenticated provider CLI and may consume your account's usage:

```sh
kitsu adviser ask second-opinion --task advice-demo --role coordinator --stage code \
  --request - <<'JSON'
{"version":1,"kind":"advice","question":"What failure cases should an order CSV export cover?"}
JSON
```

Read the answer, then use the returned consultation id to record how you addressed it:

```sh
kitsu adviser accept <consultation-id> --task advice-demo \
  --reason "Use these cases when planning the export tests."
kitsu adviser status advice-demo
```

The request, answer, execution outcome, and disposition are under
`openspec/changes/.advice/advice-demo/`. Full tasks keep them under
`openspec/changes/<task>/advisers/`; commit the record with the task. You can also
[compare named choices and require stage checkpoints](docs/advisers.md#ask-a-question-or-compare-choices).
Advice grants no approval and replaces no review. CLI adviser usage is unmeasured; native API
usage is reported separately when the provider supplies counts.

### Add the supporting Git gate

In a repository of yours, once `kitsu` is installed:

```sh
kitsu gate init                          # creates kitsu.toml, installs three git hooks
kitsu check add test -- <your test command>
kitsu check run                           # runs the checks and records the result
git add -A
git commit -m "..."              # allowed: the content has a passing run
```

Change a file and commit again without `kitsu check run`, and the commit is denied. The message names the check that has no
passing run and the command to run:

```text
kitsu: this commit is denied: its content has no recorded passing run of every declared check.
  test: not run on this content
kitsu: to produce the missing evidence, run:
  kitsu run
```

The [quick start](docs/quickstart.md) is the same five commands as a text that a test executes.

## Install

Run one command for your system. No clone, Go toolchain, or manual archive extraction is needed.

Linux and macOS:

```sh
curl -fsSL https://raw.githubusercontent.com/belevtsev/kitsu/main/install.sh | sh
```

Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/belevtsev/kitsu/main/install.ps1 | iex
```

The installer selects your platform and architecture, verifies the archive checksum before
replacing the executable, and sets up your user `PATH`. The default location is `$HOME/.local/bin`
on macOS/Linux or `%LOCALAPPDATA%\kitsu` on Windows. Open a new terminal, then run `kitsu version`.
The current installer selects `v0.0.1`.

Rerun the same command to install the version selected by the current installer. The
[installation guide](docs/release-install.md) covers version pinning, custom directories and
CI artifacts. Contributors can [build from source](docs/development.md#build-from-source).

Kitsu uses Git to work in a repository. On Windows its Git hooks use the `sh` supplied by Git for
Windows. The installer installs only Kitsu's CLI; [add the plugin](#for-a-coding-agent) through
your host's marketplace to use the coordinated workflow.

The hooks look for `kitsu` only in the entries of `PATH` that are absolute paths; see
[hooks](docs/hooks.md#when-kitsu-is-not-on-path). Existing `cw` users should follow the
[migration guide](docs/migration.md) before initializing Kitsu in an existing repository.

## For a coding agent

The plugin's `task` skill gives the coding-agent host a coordinator workflow for specification, design, tests,
implementation, review, and commit. Claude Code receives the seven roles as agents; on Codex, the coordinator uses
the texts printed by `kitsu role text <role>`. Execution and delegation are handled by the host.

The plugin also connects the workflow to supporting checks and token reporting:

| It adds | What that does |
| --- | --- |
| Context | It introduces the task workflow in a Git repository, then describes any configured supporting gate. |
| A guard | It stops a git call that skips the hooks, such as `git commit --no-verify`, a `kitsu` call that turns the gate off, and an edit of [workflow configuration](docs/operations.md#what-it-asks-about), a hook, the git configuration, or a Kitsu record. |
| Token notices | It gives `kitsu usage record` the notices that a role or the session has ended. |

Claude Code asks you before a call that the guard stops. Codex refuses it and gives the reason, because Codex cannot
ask from a hook; run the call yourself if you want it. The guard is a reminder, not a wall:
[operations](docs/operations.md#what-it-misses) lists what it misses.

The additional [`kitsu`](skills/kitsu/SKILL.md) skill explains the check-and-change loop, each gate denial,
and seven worked examples that tests replay against the CLI. The role workflow has not been run on Codex.

On Claude Code:

```text
/plugin install kitsu --marketplace belevtsev/kitsu
```

On Codex:

```sh
codex plugin marketplace add belevtsev/kitsu
codex plugin add kitsu@kitsu
```

Start a new session after installing or enabling the plugin. Codex requires manual review and
trust of the exact current hooks: open `/hooks` and review the Kitsu entries, including after an
update. Until trusted, its context, guard, and token hooks do not run. See the official
[plugin](https://learn.chatgpt.com/docs/plugins) and [hook](https://learn.chatgpt.com/docs/hooks) guides.
Claude Code's [plugin manager](https://code.claude.com/docs/en/discover-plugins) shows whether Kitsu is enabled;
restart the session or use `/reload-plugins` after a shell installation.

Then inspect local setup and start a task:

```sh
kitsu agent check --host codex
kitsu task start "Add an order CSV export" --host codex
```

Use `--host claude` for Claude Code. A successful local check does not verify authentication,
model access, hook trust, or live skill loading. Check those in the host session; the
[onboarding guide](docs/cli.md#check-setup-and-start-a-task) gives recovery steps. Kitsu performs no
automatic install or trust acceptance. The Git gate works independently of the plugin.

## How the gate decides

<img src="docs/diagrams/gate.svg" alt="How the gate decides. First the evidence: kitsu check run runs what has not passed and records each run. Then the decision, in enforce mode: git commit or git push starts one of three git hooks, the hook asks kitsu hook, and kitsu hook judges the commit or the pushed tip against the records. When every check passed on exactly this content, git goes on. When not, or when kitsu hook cannot tell, the commit or push is denied and says why: fix, then run again." width="100%">

1. **`kitsu check run` makes the evidence.** It runs each check of `kitsu.toml` that has no passing run on the working tree as
   it is on disk, and records every run with that content.
2. **git asks.** `kitsu gate init` installed three hooks: `pre-commit`, `pre-merge-commit` and `pre-push`. Each one asks
   `kitsu hook` about the content that git is about to record or send: the commit at a commit, and the new tip of each
   branch or tag at a push. A hook runs no check and waits for none.
3. **`kitsu hook` decides from the records.** When every declared check has a recorded passing run on exactly that
   content, git goes on. Otherwise the commit or the push is denied, also when `kitsu hook` cannot tell, and the message
   names each check that is not satisfied, or the cause.

The picture shows the gate in enforce mode, as `kitsu gate init` sets it. After `kitsu gate init --observe` the hooks record what
they would decide and allow every commit and push; see [observe mode](docs/hooks.md#observe-mode). At a push, a ref
is allowed without a judgement when the push deletes it, when it holds notes, or when it adds nothing to what the
remote already holds; see [pre-push](docs/hooks.md#pre-push).

[Hooks](docs/hooks.md) says what each hook judges, [evidence](docs/evidence.md) what a recorded passing run proves
and what it does not, and [operations](docs/operations.md) lists every command and every exit status.

The workflow, adviser records, token reports, and gate share the same CLI:

<img src="docs/diagrams/overview.svg" alt="Kitsu connects role coordination, optional advisers, usage reporting and the supporting Git gate. Opt-in context collection retains private worktree artifacts and prepares fresh briefs. Repository opt-in task verification plans targeted checks, publishes historical attempt summaries on success and then satisfies full gate checks with current local evidence. Advisers remain disabled by default; normal check evidence retains gate authority." width="100%">

The usage column shows host records. `kitsu usage show` also reports provider-supplied API adviser usage
separately; CLI adviser usage remains unmeasured. The [adviser flow](docs/advisers.md) shows the opt-in steps.

## State

- Kitsu versioning starts at `v0.0.1` after the historical releases, tags and attached assets
  were removed on 17 November 2026. Installer defaults, plugin manifests and CI archives use
  that version. The release workflow builds six archives, attests their provenance, and tests
  [installation](docs/release-install.md#what-ci-checks) on Linux, macOS and Windows.
- Local build, vet, and race-enabled tests passed on Linux. Lint and vulnerability checks passed for Linux,
  macOS, and Windows targets. See the [adviser change checks](openspec/changes/add-configurable-advisers/tests.md)
  and [installer validation](docs/release-install.md#what-ci-checks) for their scope.
- [Existing skill discovery and role selection](openspec/changes/add-existing-skills/tests.md) passed build, vet, and
  race tests on Linux after integration with native advisers, plus lint and vulnerability checks for all three targets.
  Loading selected skills in a live host remains unverified.
- The [grouped CLI, editors, and completion](openspec/changes/improve-cli-ux/tests.md) passed full Linux race tests
  and lint/vulnerability checks for all three targets. Editor PTY flows and all four completion shells were exercised;
  Windows PowerShell 5.1 runtime testing remains unavailable.
- [Agent onboarding](openspec/changes/add-agent-onboarding/tests.md) passed Linux race tests, fake-host terminal launches,
  and three-target lint/vulnerability checks. Darwin and Windows test binaries compiled; native runtime and interactive plugin/hook trust remain unverified.
- Whether the workflow improves code quality or saves tokens compared with one agent is **not measured**.
  Historical scratch runs on two small tasks recorded workflow usage, without a comparative quality result:
  [benchmarks](docs/benchmarks.md). The planned [evaluation](docs/evaluation.md) has not been run.
- [Compact context](openspec/changes/add-compact-context/tests.md) adds opt-in Go collection, worker briefs and bounded check results.
  Local race/regression and three-target lint/vulnerability checks passed; offline comparisons include JSON/page overhead and no live token-saving claim.
- [Typed context and saved handoffs](openspec/changes/streamline-compact-handoffs/tests.md) passed build, vet, full Linux race,
  three-target lint/vulnerability and record checks. Native live dispatch and token savings remain unverified; no further paid pilot was run.
- The [Codex-only compact pilot](docs/benchmarks/codex-compact-pilot-report.md) completed 24 real-task attempts with Sol/high.
  Both modes passed frozen tests, but independent probes confirmed defects in both charset patches. Observable workflow mechanics
  completed in 9/12 normal and 1/12 compact attempts; cutoffs left no complete token pairs. Compact mode remains opt-in.
- [Selective compact evaluation](docs/benchmarks/selective-compact-report.md) counts needed expansions and retries offline;
  some workloads use more bytes. Its Sol/high canary stopped at the budget reserve during the first ordinary run;
  the candidate passed independent tests, but no workflow or on/off pair completed.
- [Coordinator dispatch and returns](openspec/changes/reduce-coordinator-overhead/tests.md) passed local checks and a complete
  scripted review/repair/Git replay. The corrected v2 fixture uses more transport than its v1 reference; it does not establish token savings.
- [Coordinator state snapshots and deltas](openspec/changes/add-coordinator-state/tests.md) passed local build, vet, Linux race
  tests and three-target lint/vulnerability checks. Bounded observations grant no completion authority; live token savings remain unmeasured.
- Advisers are optional and experimental, disabled by default. They are tested with local fake executables and
  HTTPS fixtures; CLI examples were checked against provider help.
  No live adviser quality evaluation was run. Native usage is provider-reported; CLI usage is unmeasured.
  The model-routing recommendations are not benchmarked.
  The [production plan](openspec/production-plan.md) records earlier implementation and validation work.

## Known limits

- `git commit --no-verify` and `git push --no-verify` skip the hooks. So does a clone in which nobody ran `kitsu gate init`.
  A check in continuous integration, `kitsu check run --base <rev>`, is the backstop.
- A passing run proves that the check passed on that content on that machine. It proves nothing about what the check
  does not test.
- The hooks judge what git is about to record. They do not run the checks: `kitsu check run` does, before the commit.
- `kitsu` reads the records of Claude Code and Codex to count tokens. Their format is not documented by either host, so
  a count can be `unknown`.

More in [evidence](docs/evidence.md) and [hooks](docs/hooks.md).

## Documents

| Document | What it holds |
| --- | --- |
| [CLI guide](docs/cli.md) | Check agent setup, start a task, find commands, save host preferences, use editors and completion. |
| [CLI command reference](docs/cli-reference.md) | Generated help for command operands, options, defaults, and examples. |
| [Quick start](docs/quickstart.md) | The five commands in a new repository. A test executes them. |
| [Try it](docs/try-it.md) | Everything that can be tried by hand, with the output to expect, and what was never run. |
| [Operations](docs/operations.md) | Every command, `kitsu.toml`, the exit statuses and where `kitsu` keeps its records. |
| [Hooks](docs/hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](docs/evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Roles](docs/roles.md) | The seven roles, their models, what each may write, and `kitsu-roles.toml`. |
| [Skills](docs/skills.md) | Discover existing skills, select them per role, and resolve original sources for a worker brief. |
| [Advisers](docs/advisers.md) | Built-in decision models and CLI advisers, access by role, shared answers, and required checkpoints. |
| [The task workflow](docs/workflow.md) | The two paths of a task, its record under `openspec/changes/<task>/`, and `kitsu task`. |
| [Tokens](docs/tokens.md) | What `kitsu usage show` counts, what "workflow tokens" are, and when a count is unknown. |
| [Benchmarks](docs/benchmarks.md) | What `kitsu` costs: the time of a commit and of each command, and the tokens of an agent on two small tasks. |
| [Evaluation](docs/evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `kitsu` is recorded. |

[docs/README.md](docs/README.md) lists every document, and [CONTRIBUTING.md](CONTRIBUTING.md) says how to change
this repository.

## The previous workflow

Versions up to 2.0.96 were a larger workflow of skills and task records. It is kept on the branch `workflow-v3`.

## License

MIT, see [LICENSE](LICENSE). Third-party code: [THIRD_PARTY.md](THIRD_PARTY.md).
