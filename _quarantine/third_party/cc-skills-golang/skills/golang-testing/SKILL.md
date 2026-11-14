---
name: golang-testing
description: Design or repair Go tests for observable behavior, selecting focused unit, integration, concurrency, fuzz, or regression techniques as needed.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 🧪
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - gotests
    install:
    - kind: go
      package: github.com/cweill/gotests/gotests@latest
      bins:
      - gotests
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent Bash(gotests:*) AskUserQuestion
---

# Go tests for observable behavior

Choose tests for the behavior at risk and the evidence missing from the current task. Follow the repository's commands, toolchain, build tags, and execution environment; examples here do not override them. Honor an explicit prohibition on running tests and report the limit.

- Exercise public behavior and failure contracts, including partial effects, retry, cleanup, and cancellation when those are relevant. Coverage percentages alone do not prove the contract.
- Keep tests independently runnable. Use named subtests when they clarify cases, and bind assertions and cleanup to the correct subtest.
- Parallelize only when state and fixtures are isolated. Process-global changes, environment mutations, and unsafe mocks can invalidate parallel tests.
- Prefer observable synchronization or an injected clock over sleeps. Use version-supported `testing/synctest` for suitable timer/goroutine cases, with separate evidence for real I/O boundaries.
- Verify that regressions fail for the intended reason. Choose focused tests first; race, integration, fuzz, and benchmark runs depend on the changed risk and required repository gates.

Read [test patterns and versioned helpers](references/practices.md) for table tests, synctest, cleanup, fuzzing, and coverage. Use [HTTP testing](references/http-testing.md), [mocking](references/mocking.md), [integration fixtures](references/integration-testing.md), or [timeout helpers](references/helpers.md) only for those mechanisms. Existing scripts and examples remain available through the detailed references.

For a deeper Go fuzz target or bounded campaign, optionally resolve `test-strategy`
from the installed catalog and read its `references/go-fuzzing.md`. In this suite's
source, [Go fuzzing](../../../../skills/test-strategy/references/go-fuzzing.md) is
the fallback. Load it only for fuzzing; this skill's basic guidance works alone.
Preserve no-tests scope and do not automatically install tools or dependencies.
