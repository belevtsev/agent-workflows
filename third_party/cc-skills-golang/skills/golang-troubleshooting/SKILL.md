---
name: golang-troubleshooting
description: Investigate a concrete Go failure, panic, deadlock, leak, or unexpected result using reproduction, runtime evidence, and a verified causal explanation.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 🔍
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - dlv
    install:
    - kind: go
      package: github.com/go-delve/delve/cmd/dlv@latest
      bins:
      - dlv
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Bash(dlv:*) Agent WebFetch WebSearch AskUserQuestion
---

# Go failure diagnosis

Start from the concrete symptom, revision, environment, and available error or runtime evidence. Trace the failing data and ownership path through callers before assigning a cause or severity.

Form a falsifiable explanation and choose the smallest observation that distinguishes it from alternatives. Reproduce when practical; if the original environment is unavailable, state which parts are inferred and use focused evidence rather than claiming reproduction. A necessary mitigation can be useful while the root cause remains unresolved.

Fix the demonstrated cause within the task's scope. Verify the affected behavior and relevant failure path in the permitted environment. Do not introduce code comments, broad scans, tool installation, or production instrumentation just to record an investigation.

Read the reference matching the symptom:

- [Common Go bugs](references/common-go-bugs.md) or [compilation](references/compilation.md).
- [Concurrency](references/concurrency-debug.md), [performance](references/performance-debug.md), or the full [pprof reference](references/pprof.md).
- [Diagnostic tools](references/diagnostic-tools.md) for a specific missing observation.
- [Production debugging](references/production-debug.md) when live evidence is authorized.
- [Testing a failure](references/testing-debug.md) or [methodology](references/methodology.md) when the investigation needs those techniques.
- [Review flags](references/code-review-flags.md) for a requested review.
