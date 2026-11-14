---
name: golang-gopls
description: Use available gopls tools to inspect the resolved Go build, trace symbols and callers, diagnose code, or perform semantic refactoring.
license: MIT
metadata:
  author: samber
  version: 1.1.0
  openclaw:
    emoji: 🛰️
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
    skill-library-version: 0.22.0
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness. Requires the gopls binary (go install golang.org/x/tools/gopls@latest) v0.20+ on PATH.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent Bash(gopls:*) LSP mcp__gopls__*
---

# Go semantic code intelligence

Use an already available gopls interface for questions about the locally resolved Go build: definitions, references, method sets, call relationships, diagnostics, and semantic rewrites. Exact text searches remain useful for known files, tags, templates, and non-Go references.

Select the available MCP, native LSP, or CLI interface for the operation. Confirm the workspace, build tags, generated files, and module replacements when they affect the result. Saved-file MCP queries cannot see unsaved editor buffers. A missing symbol can be a build-selection issue, not proof of absence.

Inspect relevant callers before changing a contract and check diagnostics after a coherent edit. Semantic rename does not cover every reflection, wire-name, or external-consumer dependency. A navigation request does not require vulnerability scanning, server registration, or tool installation.

- [Capability matrix](references/matrix.md): map the desired operation to available interfaces.
- [Features and gotchas](references/features.md): navigation and transformations.
- [MCP tools](references/mcp.md), [CLI commands](references/cli.md), or [settings](references/settings.md): use only the interface needed; setup is conditional on an authorized setup task.
- [Interface comparison and workflow examples](references/practices.md): retained tool documentation.
- [Tool boundaries](../golang-how-to/references/tool-selection.md): local code, published package facts, and vulnerability reachability.
