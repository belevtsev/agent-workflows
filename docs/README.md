# Documents

Start with the first two. The rest is reference.

| Document | What it holds |
| --- | --- |
| [Quick start](quickstart.md) | The five commands in a new repository. A test executes them. |
| [Try it](try-it.md) | Everything on this branch that can be tried by hand, with the output to expect, and what was never run. |
| [Migration](migration.md) | Moving from `cw` to Gatecub while preserving old records and customized hooks. |
| [Release installation](release-install.md) | Archive installation prepared for the unpublished Gatecub beta. |
| [Brand assets](brand/README.md) | The mascot, banner, palette, and reproducible artwork brief. |
| [Benchmarks](benchmarks.md) | Historical measurements of `cw`: hook time, command time, and tokens on two small tasks. |
| [Operations](operations.md) | Every command, `gatecub.toml`, the exit statuses and where `gatecub` keeps its records. |
| [Hooks](hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Roles](roles.md) | The seven roles, their models, what each may write, and `gatecub-roles.toml`. |
| [The task workflow](workflow.md) | The two paths of a task, its record under `openspec/changes/<task>/`, and `gatecub change`. |
| [Tokens](tokens.md) | What `gatecub tokens` counts, what "workflow tokens" are, and when a count is unknown. |
| [Evaluation](evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `gatecub` is recorded. |
| [What other tools taught us](other-tools.md) | What users report about hook managers, agent guardrails, specification workflows and skills, with sources, and what `gatecub` did about each finding. |
| [Development](development.md) | How to build, test and release `gatecub`. |

Elsewhere in the repository:

- [README](../README.md): what `gatecub` is, how to install it, and its known limits.
- [The skill `gatecub`](../skills/gatecub/SKILL.md): how a coding agent works in a gated
  repository, with seven worked examples that a test replays against `gatecub`.
- [The production plan](../openspec/production-plan.md): what was built, how
  each part was checked, and what is left for the owner.
- [The specifications](../openspec/changes/): the requirements and scenarios
  of the gate and of the roles.
- [Security](../SECURITY.md), [contributing](../CONTRIBUTING.md),
  [changelog](../CHANGELOG.md), [third-party notices](../THIRD_PARTY.md).
