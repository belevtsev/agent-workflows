# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `codebase-design` reference "building blocks": reuse before writing, four kinds of block (value, decision, adapter, composition) with the test layer for each; the architect, implementer and test-engineer contracts now check for it.
- The architect, implementer and test-engineer agents under `agents/`, with role contracts in `skills/task-orchestration/references/contracts/`, an end-to-end testing reference in `skills/test-strategy/references/e2e.md`, the policy roles `architect`, `implementer` and `test_engineer`, and eval suites under `evals/`.
- `cw hook pre-publish` and `cw hook session-start` with `hooks/hooks.json`: the Claude Code plugin loads `global/AGENTS.md` at session start (optionally with a cached `cw doctor --summary`) and denies `git commit` and `git push` while the repository's newest substantial task record has no passing `cw task verify` on the current source bytes; `cw task verify` now records that content digest as `verification.source_sha256`, which `cw` releases before this one cannot read, so hosts that share a task store need the same `cw`. The gate fails open and the plugin option `enforce_verify=false` disables it. See `docs/hooks.md`.
- Root MIT `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` and GitHub issue and pull request templates.
- Plugin manifests for both hosts: `.claude-plugin/` (Claude Code marketplace and plugin) and `.codex-plugin/` plus `.agents/plugins/marketplace.json` (Codex). Install with `/plugin install agent-workflows --marketplace belevtsev/agent-workflows` in Claude Code or `codex plugin marketplace add belevtsev/agent-workflows` in Codex; the Go pack is the `cc-skills-golang` plugin in the same marketplace.
- `third_party/cc-skills-golang/UPSTREAM` records the fork's upstream version and commit and how to refresh it.
- `cw keys set NAME | unset NAME | status` saves, removes and reports the adviser keys (`TYPESAFE_API_KEY`, `SUBQ_API_KEY`, `OPENAI_API_KEY`); `set` reads the value from standard input or `--from-env VAR`, never from the command line.

### Changed

- `cw task start` refuses a substantial plan without a check bound to a requirement. `cw task verify` on a changed substantial task needs the latest run of some check passed at the current source and, when the selected path holds `go.mod`, a passed direct `go test` run whose `./...`, `./pkg` or `./dir/...` pattern covers each changed package (any file in its directory or `testdata/`); a changed `go.mod`, `go.sum`, `go.work` or `go.work.sum` needs `./...`. Runs through wrappers (`make`, scripts) or using `-c`, `-n`, `-list`, `-exec`, `-run`, `-skip`, `-C` or an unknown flag (in argv or the recorded `GOFLAGS`), `GO.EXE` and Windows-style `.\pkg` patterns are not credited. The rules live in the language-neutral registry `internal/langpack`, with Go as its first pack. Open substantial tasks whose plan has no checks, or, in a Go module, whose checks run tests only through wrappers or with those flags, cannot meet this: add a direct `go test` check and restart them.
- Worker findings carry `severity` (`blocker`, `major` or `minor`), required in review handoffs; findings recorded earlier read as `major`. `cw task verify` lists every rejected blocker or major finding as a limitation (first, as `rejected_findings`, with `--summary`), and a rejected blocker is a problem until a later accepted independent review, whose bundle lists the rejection, completes without re-raising it.
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
