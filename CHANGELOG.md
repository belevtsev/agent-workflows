# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Breaking

- The v2 installer is removed: install the Claude Code or Codex plugin instead; `cw install --host claude`
  now points at the plugin ([install][install]).
- The module path gains `/v3`: install with `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@main`
  and import `github.com/belevtsev/agent-workflows/v3/...` ([get cw][getcw]).
- Worker agents lose the `cw-` prefix and policy role `testing` is split by stage: dispatch
  `agent-workflows:reviewer`, `:test-engineer` and `:github-worker` ([agent types][types]).
- Task records gain members that an older `cw` cannot read, and its hook skips them instead of gating:
  run the same `cw` on every host and hook that shares a task store ([compatibility][compat]).
- `cw task start` refuses a substantial plan without a check bound to a requirement: add one, and
  restart open tasks that have none ([start a task][start]).
- In Go modules, `cw task verify` applies the `go.package-test/v2` rule: add direct `go test -race`
  checks covering each changed package, or opt out with `cw pack remove go` ([Go rule][start]).
- `cw task verify` needs a proven PASS for each mapped test and an independent test-engineer author
  for each changed Go test file: have the test engineer write them ([test authorship][authorship]).
- Nothing writes the hosts' model and effort settings any more: set them yourself in `settings.json`
  or `config.toml` ([local configuration](docs/workflow.md#local-configuration)).

### Added

- Claude Code and Codex plugins in one marketplace, with the Go skills as the `cc-skills-golang`
  plugin ([install][install]).
- `cw install --host codex` links the skills and writes the `AGENTS.md` block from a checkout;
  `cw uninstall --host codex` removes both ([Codex](docs/operations.md#codex-install-and-uninstall)).
- Release archives for darwin and linux on amd64 and arm64, with `SHA256SUMS` ([get cw][getcw]).
- Seven worker agents, each with a role contract: spec-writer, architect, implementer, test-engineer,
  reviewer, github-worker and the opt-in adversarial-tester ([agents](docs/agents.md)).
- A Claude Code hook denies `git commit` and `git push` while an open substantial task has no passing
  `cw task verify`, and asks before large unrecorded changes ([hooks](docs/hooks.md)).
- The gate's [threat model](SECURITY.md#threat-model) and [known bypasses](docs/hooks.md#known-bypasses):
  it guards a cooperative agent, not against one that sets out to bypass it.
- `cw publish` commits, pushes and opens or edits the pull request of a verified task; the coordinator
  publishes with it ([publish][publish]).
- `cw task add-checks` adds covering `go test` checks after a task merges its base branch
  ([merging the base](docs/operations.md#merging-the-base-branch-during-a-task)).
- `cw task abandon` closes a task so it stops gating ([which records gate](docs/hooks.md#which-records-gate)).
- Requirement traceability: test-engineer briefs declare `requirements`, handoffs map them to tests,
  and `verify` judges each mapped test ([handoffs][assign]).
- `cw task trace`, `cw task authorship` and `cw task timeline` report requirement coverage, test
  authorship and wall time without changing the record ([diagnostics][diag]).
- Review findings carry a `severity`, and `verify` lists rejected `blocker` and `major` findings
  ([acceptance][accept]).
- Language packs: the built-in Go pack, on by default, and `cw pack list|add|remove`
  ([language packs](README.md#language-packs)).
- `cw keys set|unset|status` manages adviser keys, never from the command line; disable an adviser in
  `preferences.json` ([adviser keys](docs/operations.md#adviser-keys)).
- `cw metrics import --append` keeps a cost history that outlives transcripts, and `cw metrics history`
  reports it ([cost history](docs/operations.md#cost-history)).
- The `codebase-design` [building blocks](skills/codebase-design/references/building-blocks.md)
  reference, which the architect, implementer and test-engineer contracts apply.
- Eval suites for six of the agents, checked offline by `go test ./evals/...` and run weekly in CI
  ([evals](evals/README.md#what-ci-runs)).
- `LICENSE` (MIT), `CONTRIBUTING.md`, `SECURITY.md`, issue and pull request templates, CODEOWNERS,
  Dependabot and folder guides ([docs index](docs/README.md)).

### Changed

- Substantial tasks review each stage (spec, design, tests, code), send findings back to its author, cap
  revision rounds ([review stages][stages]) and fix a wrong contract by [amendment][amend].
- Code review checks the source against the contract, `design.md` and the coverage profile, with a
  failing test for each behaviour finding ([code review][review]).
- The reviewer and test engineer start without CLAUDE.md files on Claude Code 2.1.271 or newer; briefs
  quote the rules they need ([agent types][types]).
- In a repository with `docs/decisions/`, the architect also writes an ADR for a choice that is costly
  to reverse ([architect](skills/task-orchestration/references/contracts/architect.md#required-outputs)).
- `cw task start` refuses a checkpoint reason that claims no advisers while the task binds some
  ([checkpoints](skills/task-orchestration/references/consultation-helper.md#task-checkpoints)).
- The always-on `global/AGENTS.md` block and the `task-orchestration` skill keep only the threshold and
  the decision flow; the rules live in its references ([skill](skills/task-orchestration/SKILL.md)).
- `cw` no longer reads its model policy or configuration from an installed release; see the lookup
  order in [model policy](docs/operations.md#model-policy) and [paths](docs/operations.md#paths).
- Diagrams are archify specs with committed SVGs; Mermaid is gone ([diagrams](CONTRIBUTING.md#diagrams)).
- CI runs gofmt, vet and tests on Linux and macOS; releases come only from pushed `v*` tags
  ([releases](docs/development.md#releases)).

### Fixed

- Source capture reports a Git probe that does not finish cleanly instead of treating the repository
  as a plain folder ([diagnostics][diag]).
- `cw metrics import --composition` no longer slows down on long or deeply nested Bash commands.

### Removed

- The v2 installer: `install.sh`, the setup wizard, `cw status|update|rollback|recover`,
  `cw install|uninstall` for Claude Code, and the release snapshots and state files.
- `cwdev check`, `cwdev release` and `cwdev sandbox` ([development](docs/development.md)).
- The `db-postgres` third-party skill, whose license was ambiguous.
- Archify's rendered HTML examples and the Go pack's `EVALUATIONS.md`, 13 MB of the plugin download;
  `archify examples --output-dir <dir>` renders the examples ([third-party notices](THIRD_PARTY.md)).

[install]: docs/operations.md#install
[getcw]: docs/operations.md#get-cw
[types]: skills/task-orchestration/references/hosts.md#worker-agent-types
[compat]: docs/hooks.md#which-source-counts
[start]: skills/task-orchestration/references/global-workflow.md#start-and-bind-a-task
[authorship]: skills/task-orchestration/references/worker-handoffs.md#test-rules-and-test-authorship
[publish]: skills/task-orchestration/references/github-worker.md#publish-with-cw-publish
[assign]: skills/task-orchestration/references/worker-handoffs.md#assign-before-dispatch
[diag]: docs/operations.md#global-project-diagnostics-and-evidence
[accept]: skills/task-orchestration/references/worker-handoffs.md#return-and-accept-evidence
[stages]: skills/task-orchestration/references/worker-handoffs.md#review-stages
[review]: skills/task-orchestration/references/contracts/code-review.md#uncovered-changed-blocks
[amend]: skills/task-orchestration/references/contracts/README.md#contract-amendment

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...HEAD
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
