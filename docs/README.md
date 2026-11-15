# Documents

Start with the task workflow and roles to coordinate coding agents. The Git gate is a supporting check.

| Document | What it holds |
| --- | --- |
| [CLI guide](cli.md) | Find commands, save a host preference, use explicit editors, and enable completion. |
| [CLI command reference](cli-reference.md) | Generated command help with operands, options, defaults, and examples. |
| [The task workflow](workflow.md) | Coordinate a task through specification, design, tests, implementation, review, and commit, using a shared task record. |
| [Roles](roles.md) | The seven roles, their models, what each may write, and `kitsu-roles.toml`. |
| [Skills](skills.md) | Discover existing skills, select them per role, and resolve original sources for a worker brief. |
| [Advisers](advisers.md) | Opt into experimental decision models or CLI advisers, with explicit role access; none are enabled by default. |
| [Git gate quick start](quickstart.md) | The five commands that add check evidence and Git hooks to a repository. A test executes them. |
| [Try it](try-it.md) | Everything on this branch that can be tried by hand, with the output to expect, and what was never run. |
| [Migration](migration.md) | Moving from `cw` to Kitsu while preserving old records and customized hooks. |
| [Release installation](release-install.md) | One-command installers, version selection, and verified release or CI artifacts. |
| [Brand assets](brand/README.md) | The mascot, banner, palette, and reproducible artwork brief. |
| [Benchmarks](benchmarks.md) | Historical measurements of `cw`: hook time, command time, and tokens on two small tasks. |
| [Operations](operations.md) | Every command, `kitsu.toml`, the exit statuses and where `kitsu` keeps its records. |
| [Hooks](hooks.md) | The three git hooks, a hook shared with another tool, the plugin's hooks, and the known limits. |
| [Evidence](evidence.md) | What a recorded passing run is, what it proves and what it does not. |
| [Tokens](tokens.md) | What `kitsu tokens` counts, what "workflow tokens" are, and when a count is unknown. |
| [Evaluation](evaluation.md) | The cases only a model run can show, and the comparison with one agent. No case was run; one first trial of the skill `kitsu` is recorded. |
| [What other tools taught us](other-tools.md) | What users report about hook managers, agent guardrails, specification workflows and skills, with sources, and what `kitsu` did about each finding. |
| [Development](development.md) | How to build, test and release `kitsu`. |

Elsewhere in the repository:

- [README](../README.md): what `kitsu` is, how to install it, and its known limits.
- [The skill `kitsu`](../skills/kitsu/SKILL.md): how a coding agent works in a gated
  repository, with seven worked examples that a test replays against `kitsu`.
- [The production plan](../openspec/production-plan.md): what was built, how
  each part was checked, and what is left for the owner.
- [The specifications](../openspec/changes/): the requirements and scenarios
  of the gate and of the roles.
- [Security](../SECURITY.md), [contributing](../CONTRIBUTING.md),
  [changelog](../CHANGELOG.md), [third-party notices](../THIRD_PARTY.md).
