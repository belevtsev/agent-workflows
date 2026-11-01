---
name: golang-data-structures
description: Choose or fix Go slices, maps, containers, and generic collections using their copy semantics, allocation costs, and access patterns.
license: MIT
metadata:
  author: samber
  version: 1.2.0
  openclaw:
    emoji: 🗃
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent Bash(godig:*) Bash(gopls:*) LSP mcp__gopls__* mcp__context7__resolve-library-id mcp__context7__query-docs
---

# Go data structures

Choose a representation using required operations, size, ownership, and access patterns. Do not infer performance from a structure's name alone.

- Slices copy a header and can share backing storage; capacity growth is an implementation detail. Cloning a container is shallow unless elements are copied too.
- Initialize maps before writing. Concurrent reads are safe only while no goroutine mutates the map without synchronization.
- Preserve nil-versus-empty and ordering contracts at API boundaries. Map iteration order is not stable.
- Fixed-size values suit arrays; queues, heaps, and linked lists have different locality and removal costs. Preallocate when the size estimate is useful and bounded.
- `uintptr` does not keep an object alive. Follow supported `unsafe.Pointer` conversions rather than storing pointers as integers.

Use [slice internals](references/slice-internals.md), [map internals](references/map-internals.md), [containers](references/containers.md), [generics](references/generics.md), or [pointer types](references/pointers.md) for the representation being changed. [Selection and copy examples](references/practices.md) retain the broader comparison tables.
