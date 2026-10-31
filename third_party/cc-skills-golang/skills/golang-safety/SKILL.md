---
name: golang-safety
description: Find or fix Go panic and corruption risks involving typed nils, shared backing arrays, maps, integer boundaries, unsafe code, and resource cleanup.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: 🛡
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

# Go correctness hazards

Trace the changed value's ownership and lifetime before choosing a defensive fix. Preserve intended nil, zero-value, serialization, and failure behavior.

- A typed nil in an interface is non-nil. Initialize maps before writing; nil slices can be valid.
- `append` may reuse backing storage; a copied slice or map is not an independent copy of its elements. Return or retain copies when ownership requires isolation.
- Concurrent map reads are safe only without unsynchronized mutation. Lazy initialization also needs synchronization when accessed concurrently.
- Validate bounds before indexing, allocating, or narrowing integers. Float tolerance depends on the domain; it does not repair exact-money or identifier semantics.
- `defer` runs at function exit, not the end of a loop iteration. Account for close errors when they affect data durability or the promised result.
- Do not copy live locks or keep Go pointers alive through `uintptr`. A successful type assertion or compiler check proves only the property it checked.

Use [nil safety](references/nil-safety.md), [slice/map safety](references/slice-map-safety.md), or [numeric, resource, and assertion examples](references/practices.md) for the hazard in scope. Verify the observable failure path with appropriate focused evidence.
