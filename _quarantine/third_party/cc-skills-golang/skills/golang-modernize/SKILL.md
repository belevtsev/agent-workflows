---
name: golang-modernize
description: Plan or apply a requested Go language, standard-library, or tooling modernization within the selected version and compatibility constraints.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 🔄
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
    install: []
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch WebSearch AskUserQuestion EnterWorktree ExitWorktree
---

# Go modernization

Use the project's selected Go version and compatibility requirements as the boundary. Read `go.mod`, relevant workspace/toolchain settings, and existing migration decisions. Check official release notes for the specific replacement when version support or semantics are uncertain.

Modernize the requested scope. Existing code or a dependency warning does not authorize a repository-wide upgrade, new configuration, or a tool installation. Respect existing ignore decisions; record new ones only when that documentation is requested.

For each material rewrite, identify changed semantics: loop capture, error matching, JSON wire behavior, timer behavior, randomness, cleanup, or public API. Keep compatibility-sensitive behavior covered by focused evidence in the repository's permitted environment. A language-version change also needs compatible source tools and CI.

Read [version-specific examples](references/versions.md) for the chosen target, [tooling](references/tooling.md) for an authorized tooling upgrade, or [migration catalog](references/practices.md) for deprecations and prioritization. Do not run `go mod tidy` or tests merely to offer a suggestion.
