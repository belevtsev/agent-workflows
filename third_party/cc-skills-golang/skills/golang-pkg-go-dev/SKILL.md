---
name: golang-pkg-go-dev
description: Look up published Go package documentation, symbols, versions, licenses, importers, and known vulnerabilities using available godig interfaces.
license: MIT
metadata:
  author: samber
  version: 1.4.0
  openclaw:
    emoji: 🔎
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - godig
    install:
    - kind: go
      package: github.com/samber/godig/cmd/godig@latest
      bins:
      - godig
    skill-library-version: 0.2.0
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness. Requires the godig CLI (go install github.com/samber/godig/cmd/godig@latest) or access to a godig MCP server, and internet access to reach the pkg.go.dev API.
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Bash(godig:*) Agent
---

# Published Go package lookup

Use available godig CLI or MCP capabilities for published package facts: documentation, versions, symbols, examples, licenses, importers, and known vulnerabilities. Pin the requested package/version when possible and retain the source and version in the answer.

Published-index results do not describe local `replace` directives, unpublished source, or actual call sites. A vulnerability associated with a package/version is not proof that the current build reaches it. Use local source or gopls for resolved-build questions and the repository's security workflow for a reachability claim.

Read [godig commands and setup](references/commands.md) for the required operation and [sample output](references/sample-output.md) when interpreting output. Installation and server registration are optional setup steps when in scope, not prerequisites for every lookup; use available official documentation as a fallback. Bound or filter large output, and parallelize only independent read-only lookups.

For tool boundaries, see [tool selection](../golang-how-to/references/tool-selection.md). If a tool fails, report its outcome accurately and prepare an issue summary if useful; submit an external issue only when the user authorized it.
