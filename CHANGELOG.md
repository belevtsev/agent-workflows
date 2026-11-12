# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Changed

- The commit gate's decision moved out of the CLI into `internal/gate`, so contributors unit-test gate rules with a fake git,
  clock and task store; hook decisions and texts are unchanged ([development](docs/development.md)).

### Fixed

- Worker agents can no longer dispatch subagents or run workflows, and the README states the minimum Claude Code version:
  2.1.292, for per-dispatch `effort`; below 2.1.271 `omitClaudeMd` is also ignored silently ([agents](docs/agents.md)).

## [3.0.0-rc.1] - 2026-11-12

### Breaking

- The v2 installer is removed: install the Claude Code or Codex plugin instead; `cw install --host claude`
  now points at the plugin ([install][install]).
- v2.x is end-of-life: no fixes, no upgrade or cleanup path. Install v3 fresh, keep `workflow-tasks/`,
  `credentials.json` and `preferences.json`, and remove other v2 files by hand ([paths](docs/operations.md#paths)).
- The module path gains `/v3`: install with `go install github.com/belevtsev/agent-workflows/v3/cmd/cw@v3.0.0-rc.1`
  and import `github.com/belevtsev/agent-workflows/v3/...` ([get cw][getcw]).
- Worker agents lose the `cw-` prefix and policy role `testing` is split by stage: dispatch
  `agent-workflows:reviewer`, `:test-engineer` and `:github-worker` ([agent types][types]).
- Task records gain members that an older `cw` cannot read, and its hook skips them instead of gating:
  run the same `cw` on every host and hook that shares a task store ([compatibility][compat]).
- `cw task start` refuses a substantial plan without a check bound to a requirement: add one, and
  restart open tasks that have none ([start a task][start]).
- New tasks in Go modules get the `go.package-test/v2` rule in `cw task verify`: add direct `go test -race`
  checks covering each changed package, or opt out with `cw pack remove go` ([Go rule][start]).
- `cw task verify` needs a proven PASS for each mapped test and an independent test-engineer author
  for each changed Go test file: have the test engineer write them ([test authorship][authorship]).
- Nothing writes the hosts' model and effort settings any more: set them yourself in `settings.json`
  or `config.toml` ([local configuration](docs/workflow.md#local-configuration)).
- `cw task start` refuses a not-required checkpoint reason that claims no advisers while the task binds
  some: reword the reason or keep the checkpoints required ([checkpoints][checkpoints]).
- `cw task assign` needs `requirements` on a brief with a `contract` exactly when the plan has a
  per-test-credited `go test -json` check: declare them then, and only then ([briefs][assign]).
- A key variable such as `OPENAI_API_KEY` alone no longer binds an adviser: opt in with `advisers.enabled`, `cw keys set`
  or `--policy`; tasks already started keep their binding, and `cw doctor` names a key without an opt-in ([configure advisers](docs/advisers.md#configure-advisers)).

### Added

- Claude Code and Codex plugins in one marketplace, with the Go skills as the `cc-skills-golang`
  plugin ([install][install]).
- `cw install|uninstall --host codex` links or removes a checkout's skills and `AGENTS.md` block
  ([Codex](docs/operations.md#codex-install-and-uninstall)).
- Release archives for macOS and Linux (amd64, arm64) with `SHA256SUMS` ([get cw][getcw]).
- Seven worker agents, each with a role contract: spec-writer, architect, implementer, test-engineer,
  reviewer, github-worker and the opt-in adversarial-tester ([agents](docs/agents.md)).
- A Claude Code hook denies `git commit` and `git push` while an open substantial task has no passing
  `cw task verify`, and asks before large unrecorded changes ([hooks](docs/hooks.md)).
- The gate's [threat model](SECURITY.md#threat-model) and [known bypasses](docs/hooks.md#known-bypasses):
  it guards a cooperative agent, not against a hostile one.
- `cw publish` commits, pushes and opens or edits the pull request of a verified task
  ([publish][publish]).
- `cw task add-checks` adds covering `go test` checks after a task merges its base branch
  ([merging the base](docs/operations.md#merging-the-base-branch-during-a-task)).
- `cw task abandon` closes a task so it stops gating ([which records gate](docs/hooks.md#which-records-gate)).
- Requirement traceability: handoffs map a test engineer's `requirements` to tests, and `verify`
  judges each mapped test ([handoffs][assign]).
- `cw task trace`, `cw task authorship` and `cw task timeline` report requirement coverage, test
  authorship and wall time, read-only ([diagnostics][diag]).
- Review findings carry a `severity`, and `verify` lists rejected `blocker` and `major` findings
  ([acceptance][accept]).
- Language packs: the built-in Go pack, on by default, and `cw pack list|add|remove` ([packs][start]).
- `cw keys set|unset|status` manages adviser keys, never from the command line; disable an adviser in
  `preferences.json` ([adviser keys](docs/operations.md#adviser-keys)).
- `cw metrics import --append` keeps a cost history that outlives transcripts, and `cw metrics history`
  reports it ([cost history](docs/operations.md#cost-history)).
- The `codebase-design` [building blocks](skills/codebase-design/references/building-blocks.md)
  reference, which the architect, implementer and test-engineer contracts apply.
- Eval suites for six of the agents, checked offline by `go test ./evals/...` and run weekly in CI
  ([evals](evals/README.md#what-ci-runs)).
- `LICENSE` (MIT), `CODE_OF_CONDUCT.md`, `CONTRIBUTING.md`, `SECURITY.md`, issue and pull request
  templates, CODEOWNERS, Dependabot and folder guides ([contributing](CONTRIBUTING.md)).

### Changed

- Substantial tasks review each stage (spec, design, tests, code), send findings back to its author, cap
  revision rounds ([review stages][stages]) and fix a wrong contract by [amendment][amend].
- Code review checks the source against the contract, `design.md` and the coverage profile, with a
  failing test for each behaviour finding ([code review][review]).
- The reviewer and test engineer start without CLAUDE.md files (Claude Code 2.1.271 or newer); briefs
  quote the rules they need ([agent types][types]).
- In a repository with `docs/decisions/`, the architect also writes an ADR for a choice that is costly
  to reverse ([architect](skills/task-orchestration/references/contracts/architect.md#required-outputs)).
- The always-on `global/AGENTS.md` block and the `task-orchestration` skill core keep only the decision
  flow; the rules live in references ([skill](skills/task-orchestration/SKILL.md)).
- `cw` no longer reads its model policy or configuration from an installed release
  ([policy](docs/operations.md#model-policy), [paths](docs/operations.md#paths)).
- Diagrams are archify specs with committed SVGs; Mermaid is gone ([diagrams](CONTRIBUTING.md#diagrams)).
- `cw` runs on macOS and Linux; Windows is not supported ([get cw][getcw]).
- CI runs gofmt, vet and tests on Linux and macOS, plus golangci-lint and govulncheck for both
  ([CI](docs/development.md#continuous-integration)); releases come only from pushed `v*` tags and
  build with go1.27.4, which fixes 11 reachable stdlib vulnerabilities ([releases](docs/development.md#releases)).
- Both plugins and the marketplace carry version `3.0.0-rc.1`; install `cw` from the `v3.0.0-rc.1` release instead of `@main` ([get cw][getcw]).
- The README's first task is a substantial one, with a line for small changes and the planned, unmeasured per-task
  cost caps ([first task](README.md#your-first-task)).
- Without `cw` on `PATH`, the session-start hook prints its install command, and `global/AGENTS.md` and the
  task-orchestration skill say to stop and give it ([session start](docs/hooks.md#session-start)).
- The overview diagram ends with `cw publish`, with the GitHub worker as fallback, and shows the opt-in adversarial
  tester ([how it works](README.md#how-it-works)).
- Security fixes go to the newest v3 release, release candidates included ([supported versions](SECURITY.md#supported-versions)).

### Fixed

- Source capture reports a Git probe that does not finish cleanly instead of treating the repository
  as a plain folder ([diagnostics][diag]).
- `cw metrics import --composition` no longer slows down on long or deeply nested Bash commands
  ([diagnostics][diag]).
- `cw consult` reports a failed close of its output record instead of exiting 0 with a record that
  may be incomplete.
- `THIRD_PARTY.md` lists go-toml v2.4.3, the version `cw` links; a test keeps the table in step with `go.mod`.

### Removed

- The v2 installer: `install.sh`, the setup wizard, `cw status|update|rollback|recover` and the
  release snapshots ([install][install]).
- `cwdev check`, `cwdev release` and `cwdev sandbox` ([development](docs/development.md)).
- The `db-postgres` third-party skill, whose license was ambiguous
  ([license rule](CONTRIBUTING.md#proposing-or-changing-a-skill)).
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
[checkpoints]: skills/task-orchestration/references/consultation-helper.md#task-checkpoints

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v3.0.0-rc.1...HEAD
[3.0.0-rc.1]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...v3.0.0-rc.1
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
