---
name: golang-structs-interfaces
description: Design or revise Go structs, method sets, interfaces, embedding, and receiver choices while preserving copying, serialization, and API semantics.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 🧩
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

# Go types and interfaces

Design around the consumer's required behavior and the type's ownership. Keep established exported contracts unless an API change is requested.

- Extract interfaces where a consumer needs substitution or a stable contract; use the smallest useful method set. Preserve standard interface signatures.
- A typed nil stored in an interface is non-nil. Check assertions when a dynamic type is not guaranteed.
- Receiver choices affect method sets, copying, and mutation. Do not copy values containing synchronization state after use; a copied container can still share its underlying data.
- Embedding promotes methods and may unintentionally expose API or change interface satisfaction. A named field keeps forwarding explicit.
- Serialization tags and zero values are part of behavior. Preserve them when reorganizing fields or replacing `any` with a concrete or generic API.

Read [type design examples](references/practices.md) for method sets, embedding, compile-time checks, tags, generics, and copy prevention. Verify renames against reflection and external consumers as well as compiler-visible callers.
