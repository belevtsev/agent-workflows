---
name: golang-code-style
description: Improve Go control flow and source readability when a task asks for style cleanup, clarity review, or project formatting conventions.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: 🎨
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

# Go code clarity

Apply the project's established formatter and conventions to the requested files. Prefer changes that make control flow or ownership easier to understand; avoid unrelated formatting churn or comments that merely justify a style preference.

- Preserve nil-versus-empty behavior at serialization boundaries; a nil slice is valid Go and may be intentional.
- Use keyed struct literals when field order is not the API. Break long expressions at semantic boundaries, without changing short-circuit evaluation or shadowing.
- Reduce nesting where it clarifies failure paths. Named conditions should explain domain meaning, not hide order-sensitive work.
- An exported rename can break consumers; visibility and method sets are API decisions, not formatting.

Read [examples and detailed conventions](references/practices.md) only for the relevant clarity issue. [Conditional-expression and argument examples](references/details.md) expand the cases where a rewrite can obscure behavior. Repository and user conventions take precedence over these defaults.
