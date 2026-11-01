---
name: golang-dependency-injection
description: Choose or refactor Go dependency wiring and lifecycle ownership, comparing constructor injection with a DI library when that decision is in scope.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: 🔌
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch mcp__context7__resolve-library-id mcp__context7__query-docs AskUserQuestion
---

# Go dependency wiring

Use explicit dependencies to make construction and lifecycle understandable. Keep the existing DI choice unless evaluating or changing it is part of the task. Manual constructor injection is often enough; a container is useful only for a concrete graph or lifecycle need.

Keep container access at the composition root. Pass the required dependencies into services rather than passing a service locator. Define ownership of startup failures, partial construction cleanup, shutdown order, scopes, and overrides. Interface boundaries should serve consumers and tests, not merely mirror every concrete type.

Read [manual construction](references/manual-di.md) or the selected library example: [Wire](references/google-wire.md), [dig/Fx](references/uber-dig-fx.md), [samber/do](references/samber-do.md). [Comparison and test examples](references/practices.md) help when choosing or migrating an approach. Consult a library-specific skill only when its API or lifecycle mechanics are actually needed.
