# Documents

Start with the first two. The rest is reference.

| Document | What it holds |
| --- | --- |
| [Quick start](quickstart.md) | The five commands in a new repository. A test executes them. |
| [Try it](try-it.md) | Everything on this branch that can be tried by hand, with the output to expect, and what was never run. |
| [Benchmarks](benchmarks.md) | What `cw` costs: the time of a commit and of each command against the size of a repository, and the tokens of an agent with the gate and with the roles on two small tasks. |
| [Operations](operations.md) | Every command, `cw.toml`, the exit statuses and where `cw` keeps its records. |
| [Hooks](hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Roles](roles.md) | The seven roles, their models, what each may write, and `cw-roles.toml`. |
| [The task workflow](workflow.md) | The two paths of a task, its record under `openspec/changes/<task>/`, and `cw change`. |
| [Tokens](tokens.md) | What `cw tokens` counts, what "workflow tokens" are, and when a count is unknown. |
| [Evaluation](evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `cw` is recorded. |
| [What other tools taught us](other-tools.md) | What users report about hook managers, agent guardrails, specification workflows and skills, with sources, and what `cw` did about each finding. |
| [Development](development.md) | How to build, test and release `cw`. |

Elsewhere in the repository:

- [README](../README.md): what `cw` is, how to install it, and its known limits.
- [The skill `cw`](../skills/cw/SKILL.md): how a coding agent works in a gated
  repository, with seven worked examples that a test replays against `cw`.
- [The production plan](../openspec/production-plan.md): what was built, how
  each part was checked, and what is left for the owner.
- [The specifications](../openspec/changes/): the requirements and scenarios
  of the gate and of the roles.
- [Security](../SECURITY.md), [contributing](../CONTRIBUTING.md),
  [changelog](../CHANGELOG.md), [third-party notices](../THIRD_PARTY.md).
