---
name: golang-design-patterns
description: Choose Go API or package design patterns for a concrete ownership, construction, resource lifecycle, or resilience problem.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 🏗
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent AskUserQuestion
---

# Go API and lifecycle design

Choose a pattern for the concrete problem in the task. Follow the existing architecture unless changing it is part of the request. Prefer explicit ownership and construction over new framework layers.

- Validate construction inputs before publishing a usable object. Preserve zero-value and constructor contracts when changing options or builders.
- Give resources an owner, a close path, and an error policy. Close durable writers explicitly when the close result determines success.
- Bound external calls and retries. Cancellation must interrupt backoff; retries need a safe repeat or idempotency contract.
- Make shutdown ordering explicit: stop admission, finish or cancel owned work, then release dependencies.
- For stateful changes, identify acknowledgement, persistence, and retry boundaries before choosing an abstraction.

Read [construction and resource examples](references/practices.md) for a specific pattern, [resource management](references/resource-management.md) for cleanup and shutdown, or [data handling](references/data-handling.md) for streaming and representation choices. For an architecture decision, start with [architecture](references/architecture.md), then the relevant [clean](references/clean-architecture.md), [hexagonal](references/hexagonal-architecture.md), or [DDD](references/ddd.md) reference.
