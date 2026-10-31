---
name: golang-error-handling
description: Design or review Go error contracts, wrapping, chain inspection, cleanup failures, and panic boundaries so callers can handle failures correctly.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: ⚠
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent
---

# Go error contracts

Make failure behavior useful to the caller. Preserve documented sentinel, typed-error, retry, and partial-success contracts.

- Add context with `%w` when callers should inspect the underlying cause; wrapping also exposes that cause as part of the API.
- Use `errors.Is` for chain matching and `errors.As` for typed inspection. Use newer alternatives only when the selected Go version supports them.
- Do not return a typed nil as `error`. Distinguish cancellation, timeouts, validation errors, and transient failures when the caller acts on them.
- Give each error a handling owner. Avoid duplicate log-and-return reporting; deliberate boundary logging should add necessary information and preserve the caller's policy.
- Account for cleanup and durability failures. Return or combine independent errors when they affect the promised result; do not silently replace the original cause.
- Expected failures return errors. Recovery belongs at an owned boundary with an explicit policy; it does not make corrupted state safe to continue.

Read [creation](references/error-creation.md), [wrapping and inspection](references/error-wrapping.md), or [handling and logging](references/error-handling.md) for the relevant contract. [Summary examples](references/practices.md) retain additional comparisons.
