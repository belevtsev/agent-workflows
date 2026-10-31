---
name: golang-documentation
description: Write or review Go API comments, examples, README, contribution, or release documentation against the actual code and supported workflows.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 📝
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch
---

# Go documentation

Write the documentation the task requests from the actual API, commands, and supported environment. Keep observed behavior separate from proposals and unverified examples. Reuse existing validation evidence; writing documentation does not itself require a new test campaign.

Explain caller obligations and observable behavior that signatures cannot show: ownership, lifetime, cancellation, errors, ordering, concurrency safety, and compatibility. Verify symbol names and command paths. Release notes describe effects on users without inflating a narrow fix.

Read only the needed format:

- [Code comments](references/code-comments.md): package/API comments, examples, deprecation.
- [Project documents](references/project-docs.md): README, CONTRIBUTING, changelog, delivery.
- [Library documentation](references/library.md) or [application documentation](references/application.md): audience-specific needs.
- [Examples and templates](references/practices.md): detailed samples and links to existing assets.

Add ancillary files, deployment setup, or generated documentation only when those deliverables are in scope. Templates are starting points, not a list of files every project must acquire.
