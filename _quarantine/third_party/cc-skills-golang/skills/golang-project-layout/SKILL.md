---
name: golang-project-layout
description: Create or reorganize a Go module or workspace with package boundaries, entrypoints, test placement, and configuration suited to its actual scope.
license: MIT
metadata:
  author: samber
  version: 1.4.0
  openclaw:
    emoji: 📁
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
    install: []
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent AskUserQuestion
---

# Go project layout

Match structure to the requested project and existing conventions. A small program can stay flat. Introduce packages, dependency injection, and layers for actual ownership or reuse needs; a routine setup need not pause for architectural choices already clear from the task.

Keep executable entrypoints and importable packages distinct. Respect `internal` visibility, import-cycle constraints, module paths, and major-version suffixes. For an existing project, directory moves may affect initialization, generated code, external consumers, and build scripts.

- [Directory examples](references/directory-layouts.md): small programs, services, and libraries.
- [Workspace behavior](references/workspaces.md): multiple modules and local replacements.
- [Test layout](references/testing-layout.md): colocated tests, fixtures, and benchmarks.
- [Application configuration](references/config.md): only when Cobra/Viper is chosen.

Create only the files needed for the requested deliverable. Adding a Go project or reorganizing packages does not require agent-configuration changes or additional skill activation. Project guidance, lint setup, and CI are separate deliverables when requested.
