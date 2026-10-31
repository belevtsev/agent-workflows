---
name: golang-concurrency
description: Design or review Go goroutine ownership, synchronization, channel closure, and bounded workers to prevent races, deadlocks, and shutdown leaks.
license: MIT
metadata:
  author: samber
  version: 1.2.1
  openclaw:
    emoji: ⚡
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

# Go concurrency ownership

For the changed concurrent operation, establish who starts it, who stops it, which state is shared, and how completion or failure reaches the owner.

- Every owned goroutine needs an exit path and a way to join or otherwise account for completion. Bound goroutine and queue growth.
- Channel closure belongs to the component that can prove no sender remains. Cancellation must unblock sends, receives, and other waits where the operation promises cancellation.
- Mutexes protect invariants across the whole critical section; atomics protect specific accesses, not an arbitrary multi-field protocol. Do not copy synchronization primitives after use.
- Register work before a waiter can observe completion. Propagate worker errors and cancel sibling work when required by the operation's semantics.
- Keep shutdown order and acknowledgement ownership explicit; a send completing is not necessarily an external operation completing.

Select [channels and select](references/channels-and-select.md), [synchronization primitives](references/sync-primitives.md), or [pipelines and workers](references/pipelines.md) for the mechanism being changed. [Decision tables and examples](references/practices.md) compare the primitives and retain version-specific notes.

Verify the changed ordering, cancellation, and shutdown behavior with appropriate deterministic or race evidence in the repository's permitted environment. Broaden checks when failures or the change's reach justify it.
