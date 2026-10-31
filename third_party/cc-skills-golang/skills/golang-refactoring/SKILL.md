---
name: golang-refactoring
description: Restructure Go code while preserving observable behavior, accounting for callers, reflection, exported APIs, and verification of the changed paths.
license: MIT
metadata:
  author: samber
  version: 1.1.1
  openclaw:
    emoji: ♻️
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - gopls
    install:
    - kind: go
      package: golang.org/x/tools/gopls@latest
      bins:
      - gopls
    - kind: go
      package: golang.org/x/perf/cmd/benchstat@latest
      bins:
      - benchstat
    skill-library-version: 0.20.0
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang. Requires gopls and git.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Bash(gh:*) Bash(gopls:*) Bash(benchstat:*) LSP mcp__gopls__* Agent AskUserQuestion EnterWorktree ExitWorktree WebFetch WebSearch
---

# Go refactoring

Preserve the requested observable behavior while changing structure. Identify the purpose and affected callers, then make a coherent, reviewable change within existing authorization. Refactoring does not automatically require a new approval, branch, commit, PR, or agent team.

- Use semantic tools for broad renames and method-set changes when available. Investigate a rejected transform; do not assume a textual rewrite is equivalent.
- Check reflection, struct tags, templates, generated code, and external consumers that local symbol tools cannot see.
- Keep behavior changes distinguishable from mechanical moves. Separate them when that improves review or verification; use the user's requested delivery scope.
- For a package move, assess import cycles, initialization order, API paths, and rollout. Type aliases can preserve identity during a staged migration when that compatibility is needed.
- Preserve unrelated work. A failed check calls for diagnosis and repair of this change, not an automatic repository-wide revert.

Read [tooling](references/go-tooling.md) or the [transform catalog](references/catalog.md) for exact mechanics. Use [structural changes](references/structural.md) for package/API moves, [verification selection](references/safety-net.md) for uncertain behavior coverage, and [staging workflow](references/workflow.md) only for a multi-step migration. No fixed coverage threshold replaces inspection of the actual changed paths.

Run the relevant allowed checks and required repository gates. Add race evidence for concurrency changes or comparable measurements for a performance-sensitive path; a routine rename does not require an assurance campaign.
