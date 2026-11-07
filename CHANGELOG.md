# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- `cw task abandon --task DIR --reason TEXT` writes a terminal `abandonment` revision; an abandoned record accepts no further revisions and never gates `git commit` or `git push`. `cw` releases before this one cannot read an abandoned record.
- The commit gate asks (`permissionDecision: "ask"`) before a commit or push with no task record whose change has two of: more than 300 changed lines, more than one top-level directory, code changed without tests; the plugin option `small_task_prompt=false` turns this off. It also asks before a push of a verified repository whose working tree has uncommitted tracked changes.
- `codebase-design` reference "building blocks": reuse before writing, four kinds of block (value, decision, adapter, composition) with the test layer for each; the architect, implementer and test-engineer contracts now check for it.
- Folder guides: `skills/README.md` (catalog kept in sync with each SKILL.md by `catalog_test.go`), `docs/README.md` (index), `docs/agents.md` (agent roster and contracts), `third_party/README.md` (vendored packs and the Go fork); `.github/CODEOWNERS`, Dependabot for Actions and Go modules, `.editorconfig`.
- The architect, implementer and test-engineer agents under `agents/`, with role contracts in `skills/task-orchestration/references/contracts/`, an end-to-end testing reference in `skills/test-strategy/references/e2e.md`, the policy roles `architect`, `implementer` and `test_engineer`, and eval suites under `evals/`.
- `cw hook pre-publish` and `cw hook session-start` with `hooks/hooks.json`: the Claude Code plugin loads `global/AGENTS.md` at session start (optionally with a cached `cw doctor --summary`) and denies `git commit` and `git push` while an open substantial task record for the repository has no passing `cw task verify` on the current source bytes; `cw task verify` now records that content digest as `verification.source_sha256`, which `cw` releases before this one cannot read, so hosts that share a task store need the same `cw`. The gate fails open and the plugin option `enforce_verify=false` disables it. See `docs/hooks.md`.
- Root MIT `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` and GitHub issue and pull request templates.
- Plugin manifests for both hosts: `.claude-plugin/` (Claude Code marketplace and plugin) and `.codex-plugin/` plus `.agents/plugins/marketplace.json` (Codex). Install with `/plugin install agent-workflows --marketplace belevtsev/agent-workflows` in Claude Code or `codex plugin marketplace add belevtsev/agent-workflows` in Codex; the Go pack is the `cc-skills-golang` plugin in the same marketplace.
- `third_party/cc-skills-golang/UPSTREAM` records the fork's upstream version and commit and how to refresh it.
- `cw keys set NAME | unset NAME | status` saves, removes and reports the adviser keys (`TYPESAFE_API_KEY`, `SUBQ_API_KEY`, `OPENAI_API_KEY`); `set` reads the value from standard input or `--from-env VAR`, never from the command line.

### Changed

- `cw task start` refuses a substantial plan without a check bound to a requirement. `cw task verify` on a changed substantial task needs a passed check run at the current source and, when the selected path holds `go.mod`, a passed `go test` run whose `./...`, `./pkg` or `./dir/...` pattern covers each changed non-test package. The rules live in the language-neutral registry `internal/langpack`, with Go as its first pack. Open substantial tasks whose plan has no checks cannot meet this and must be restarted.
- Worker findings carry `severity` (`blocker`, `major` or `minor`), required in review handoffs; findings recorded earlier read as `major`. `cw task verify` lists every rejected blocker or major finding as a limitation (first, as `rejected_findings`, with `--summary`), and a rejected blocker is a problem until a later accepted independent review, whose bundle lists the rejection, completes without re-raising it.
- Worker agent types are namespaced under the plugin (`agent-workflows:cw-reviewer` and friends); the task-orchestration skill carries a `Plugin root:` line so briefs can name absolute skill paths.
- The always-on instruction block (`global/AGENTS.md`, 7,865 to 2,176 bytes) and the `task-orchestration` skill core (`SKILL.md`, 21,293 to 5,473 bytes) carry only the substantial-task threshold and the decision flow; every rule lives in one reference file, test authors and the GitHub worker are dispatched for substantial tasks or on request, and `docs/workflow.md` links the references instead of repeating them.
- Releases are published from pushed `v*` tags instead of on every push to main.
- `.gitignore` no longer hides macOS Finder duplicate files (names with a ` 2` suffix).
- Rendered diagram HTML is no longer tracked. The JSON specs remain.
- `cw consult` and `cw task start` resolve the model policy as `--policy`, then `$CW_MODEL_POLICY`, then `~/.agents/skills/task-orchestration/model-policy.yaml` when present, then the policy embedded in `cw`; the installed release's `current/skills/task-orchestration/model-policy.yaml` is no longer read. The runtime reads only `version`, `consultation.deadline_seconds` (1 to 3600) and `consultation.providers` from it (`internal/modelpolicy`).
- The configuration directory of the runtime commands is `--config-dir`, then `$XDG_CONFIG_HOME/agent-workflows`, then `~/.config/agent-workflows` (`internal/userconfig`); a recorded installation's directory is no longer consulted. `preferences.json` is read for `advisers.disabled` only, and a top-level member other than `version`, `hosts`, `shared` and `advisers` is refused.
- `cw task`, `cw consult`, `cw doctor`, `cw graphify` and `cw metrics` no longer depend on `internal/workflow`.

### Fixed

- The commit gate stays shut while any open substantial record for the repository lacks a passing `cw task verify`; a newer record no longer hides an older open one, and the deny reason names every gating record with its verify and abandon commands. A verified record is finished, and stops gating, once another record starts, after that verification, from exactly the bytes it was verified on.
- The commit gate ages records by their latest revision instead of their start, so a task worked on for more than 14 days keeps gating.
- From a linked worktree of a record's checkout, the commit gate compares the linked worktree's source with the verified digest instead of the main checkout's.
- The commit gate's hook runs for every Bash command that names git (`Bash(*git*)`) and recognises `git -C DIR`, `command [-p] git`, `env [options] git` (following `env -C DIR`), `sudo`, `timeout`/`nice`/`stdbuf` wrappers, `$(...)` substitutions before git, `sh -c '...'`, `eval` and `\git`.
- The session-start hook says that the verify gate is inactive when `cw` is not on `PATH`.

### Removed

- The `db-postgres` third-party skill, because its license was ambiguous. The suite now has 36 registrations with 81 entrypoints.

### Fixed

- `TestRunningChecksRetainEveryTerminalSlotAtRevisionLimit` no longer fails intermittently under `-race`: the check-barrier helpers wait for the test's own lifetime instead of a fixed 15-second accept deadline, and report a check that exits before its barrier instead of timing out.

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...HEAD
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
