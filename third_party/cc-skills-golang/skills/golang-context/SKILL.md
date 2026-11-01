---
name: golang-context
description: Fix or design Go context propagation, deadlines, and cancellation ownership across request paths, goroutines, and bounded background work.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: 🔗
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

# Go context ownership

Propagate the caller's context through the request's services, storage, and external calls. Accept it explicitly, conventionally as `ctx context.Context`, and avoid replacing it with a background context inside a request path.

- Call the cancel function on every path unless its ownership is explicitly transferred.
- A deadline does not interrupt arbitrary work: blocking operations and loops must observe cancellation or have another bounded exit.
- Give work that must outlive a request a distinct owner, bounded lifetime, and shutdown path. `WithoutCancel` removes the inherited cancellation/deadline and is not a background-work supervisor.
- Context values carry request metadata using private key types, not required function parameters or hidden service dependencies.
- Context cancellation does not undo a committed external side effect. Preserve the acknowledgement and retry policy at that boundary.

Read [cancellation and deadlines](references/cancellation.md), [values and tracing](references/values-tracing.md), or [HTTP and service calls](references/http-services.md) for that part of the path. [Propagation examples](references/practices.md) illustrate the basic API shape.
