---
name: golang-naming
description: Choose or revise Go package, type, method, variable, and test names while preserving exported API compatibility and local conventions.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 🏷
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

# Go naming

Choose names in the context of their call sites and the project's existing API. Preserve exact names the user requested. Exported names, reflection, tags, and serialized field names can be compatibility boundaries; inspect those uses before renaming.

Use MixedCaps and consistent initialisms, avoid package stuttering, and let local scope determine useful name length. Boolean names should read naturally; a prefix is useful when it clarifies meaning, not a universal requirement. Follow canonical interface method signatures such as `String() string` and `Read([]byte) (int, error)`.

Read only the category needed:

- [Packages and files](references/packages-files.md): package names, aliases, filenames.
- [Identifiers](references/identifiers.md): variables, receivers, initialisms, booleans.
- [Functions and methods](references/functions-methods.md): constructors, options, getters.
- [Types and errors](references/types-errors.md): interfaces, enums, sentinel/type names.
- [Test names](references/testing.md): tests, helpers, and subcases.
- [Examples](references/practices.md): quick comparisons and common naming mistakes.
