# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Folder guides: `skills/README.md` (catalog kept in sync with each SKILL.md by `catalog_test.go`), `docs/README.md` (index), `docs/agents.md` (agent roster and contracts), `third_party/README.md` (vendored packs and the Go fork); `.github/CODEOWNERS`, Dependabot for Actions and Go modules, `.editorconfig`.
- The architect, implementer and test-engineer agents under `agents/`, with role contracts in `skills/task-orchestration/references/contracts/`, an end-to-end testing reference in `skills/test-strategy/references/e2e.md`, the policy roles `architect`, `implementer` and `test_engineer`, and eval suites under `evals/`.
- Root MIT `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` and GitHub issue and pull request templates.
- Plugin manifests for both hosts: `.claude-plugin/` (Claude Code marketplace and plugin) and `.codex-plugin/` plus `.agents/plugins/marketplace.json` (Codex). Install with `/plugin install agent-workflows --marketplace belevtsev/agent-workflows` in Claude Code or `codex plugin marketplace add belevtsev/agent-workflows` in Codex; the Go pack is the `cc-skills-golang` plugin in the same marketplace.
- `third_party/cc-skills-golang/UPSTREAM` records the fork's upstream version and commit and how to refresh it.
- `cw keys set NAME | unset NAME | status` saves, removes and reports the adviser keys (`TYPESAFE_API_KEY`, `SUBQ_API_KEY`, `OPENAI_API_KEY`); `set` reads the value from standard input or `--from-env VAR`, never from the command line.

### Changed

- Worker agent types are namespaced under the plugin (`agent-workflows:cw-reviewer` and friends); the task-orchestration skill carries a `Plugin root:` line so briefs can name absolute skill paths.
- The always-on instruction block (`global/AGENTS.md`, 7,865 to 2,176 bytes) and the `task-orchestration` skill core (`SKILL.md`, 21,293 to 5,473 bytes) carry only the substantial-task threshold and the decision flow; every rule lives in one reference file, test authors and the GitHub worker are dispatched for substantial tasks or on request, and `docs/workflow.md` links the references instead of repeating them.
- Releases are published from pushed `v*` tags instead of on every push to main.
- `.gitignore` no longer hides macOS Finder duplicate files (names with a ` 2` suffix).
- Rendered diagram HTML is no longer tracked. The JSON specs remain.
- `cw consult` and `cw task start` resolve the model policy as `--policy`, then `$CW_MODEL_POLICY`, then `~/.agents/skills/task-orchestration/model-policy.yaml` when present, then the policy embedded in `cw`; the installed release's `current/skills/task-orchestration/model-policy.yaml` is no longer read. The runtime reads only `version`, `consultation.deadline_seconds` (1 to 3600) and `consultation.providers` from it (`internal/modelpolicy`).
- The configuration directory of the runtime commands is `--config-dir`, then `$XDG_CONFIG_HOME/agent-workflows`, then `~/.config/agent-workflows` (`internal/userconfig`); a recorded installation's directory is no longer consulted. `preferences.json` is read for `advisers.disabled` only, and a top-level member other than `version`, `hosts`, `shared` and `advisers` is refused.
- `cw task`, `cw consult`, `cw doctor`, `cw graphify` and `cw metrics` no longer depend on `internal/workflow`.

### Removed

- The `db-postgres` third-party skill, because its license was ambiguous. The suite now has 36 registrations with 81 entrypoints.

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...HEAD
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
