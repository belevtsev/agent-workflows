---
name: golang-lint
description: Configure golangci-lint or resolve specific Go lint findings, using the project configuration, supported toolchain, and justified suppressions.
license: MIT
metadata:
  author: samber
  version: 1.4.1
  openclaw:
    emoji: 🧹
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - golangci-lint
    install:
    - kind: brew
      formula: golangci-lint
      bins:
      - golangci-lint
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    - .golangci.yml
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent
---

# Go lint configuration and findings

Start from the project's existing configuration, selected Go version, and actual linter output. Preserve the agreed rule set; resolving a warning does not authorize replacing the configuration or enabling an unrelated suite.

- Check the linter's explanation and the surrounding behavior before applying a fix. Inspect the diff from automatic fixes.
- Scope suppressions to a named linter and explain the local reason. Resource, security, and error-handling warnings need a demonstrated false positive or intentional policy, not a blanket exclusion.
- Treat compiler, formatter, analyzer, generator, and CI compatibility separately when introducing new language syntax.
- Run the applicable lint command in the repository's permitted environment. Recheck after a material fix; passing lint is not proof of runtime correctness.

Use the [linter reference](references/linter-reference.md), [suppression reference](references/nolint-directives.md), or [command and configuration examples](references/practices.md) as needed. The [bundled configuration](assets/.golangci.yml) is an optional starting point for a requested setup, not a requirement for every project.
