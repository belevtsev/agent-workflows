---
name: golang-dependency-management
description: Change or diagnose Go module dependencies, version conflicts, replacements, and workspaces while preserving reproducible resolution.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 📦
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - govulncheck
    install:
    - kind: go
      package: golang.org/x/vuln/cmd/govulncheck@latest
      bins:
      - govulncheck
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent Bash(govulncheck:*) AskUserQuestion
---

# Go dependency resolution

Work from the actual module/workspace, selected Go toolchain, and pinned dependencies. Distinguish local replacements from published module resolution and version-selection rules from downloaded checksums.

- Keep `go.mod` and applicable `go.sum` changes reviewable. Dependency authorization covers the requested scope, not an unbounded `go get -u` upgrade.
- Use the standard library or an existing dependency when it meets the requirement. For a new choice, evaluate maintained APIs, compatibility, license, and meaningful security evidence.
- Inspect the selected version and transitive changes. Use `go mod tidy` when necessary to reconcile an authorized dependency edit, not for a read-only question.
- A `go.work` or `replace` can hide a consumer compatibility failure. Validate published resolution when the claim depends on it.
- Distinguish known package vulnerabilities from code paths reachable in this build. Required security gates remain required; ordinary module inspection is not a full audit.

Use [versioning and MVS](references/versioning.md), [conflict resolution](references/conflicts.md), [workspaces](references/workspaces.md), or [command examples](references/practices.md) for the task. Read [auditing](references/auditing.md), [automated updates](references/automated-updates.md), or [graph visualization](references/visualization.md) only when that workflow is requested or needed.
