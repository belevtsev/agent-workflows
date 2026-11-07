# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- Root MIT `LICENSE`, `CONTRIBUTING.md`, `SECURITY.md`, `CHANGELOG.md` and GitHub issue and pull request templates.
- Plugin manifests for both hosts: `.claude-plugin/` (Claude Code marketplace and plugin) and `.codex-plugin/` plus `.agents/plugins/marketplace.json` (Codex). Install with `/plugin install agent-workflows --marketplace belevtsev/agent-workflows` in Claude Code or `codex plugin marketplace add belevtsev/agent-workflows` in Codex; the Go pack is the `cc-skills-golang` plugin in the same marketplace.
- `third_party/cc-skills-golang/UPSTREAM` records the fork's upstream version and commit and how to refresh it.

### Changed

- The always-on instruction block (`global/AGENTS.md`, 7,865 to 2,176 bytes) and the `task-orchestration` skill core (`SKILL.md`, 21,293 to 5,473 bytes) carry only the substantial-task threshold and the decision flow; every rule lives in one reference file, test authors and the GitHub worker are dispatched for substantial tasks or on request, and `docs/workflow.md` links the references instead of repeating them.
- Releases are published from pushed `v*` tags instead of on every push to main.
- `.gitignore` no longer hides macOS Finder duplicate files (names with a ` 2` suffix).
- Rendered diagram HTML is no longer tracked. The JSON specs remain.

### Removed

- The `db-postgres` third-party skill, because its license was ambiguous. The suite now has 36 registrations with 81 entrypoints.

## [2.0.96] - 2026-11-07

Last release published automatically from main.

[Unreleased]: https://github.com/belevtsev/agent-workflows/compare/v2.0.96...HEAD
[2.0.96]: https://github.com/belevtsev/agent-workflows/releases/tag/v2.0.96
