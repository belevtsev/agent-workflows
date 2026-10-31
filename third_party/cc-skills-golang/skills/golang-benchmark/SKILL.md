---
name: golang-benchmark
description: Measure Go performance with benchmarks, pprof, traces, and benchstat when a task needs a reproducible baseline, comparison, or regression threshold.
license: MIT
metadata:
  author: samber
  version: 1.3.0
  openclaw:
    emoji: 📊
    homepage: https://github.com/samber/cc-skills-golang
    requires:
      bins:
      - go
      - benchstat
    install:
    - kind: go
      package: golang.org/x/perf/cmd/benchstat@latest
      bins:
      - benchstat
  upstream:
    user-invocable: true
    compatibility: Designed for Claude Code, Codex or similar harness, and for projects using Golang.
    paths:
    - '**/*.go'
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch Bash(benchstat:*) Bash(benchdiff:*) Bash(cob:*) Bash(gobenchdata:*) Bash(curl:*) mcp__context7__resolve-library-id mcp__context7__query-docs WebSearch AskUserQuestion EnterWorktree ExitWorktree
---

# Go performance measurement

Measure the requested behavior under recorded conditions: revision, toolchain, platform, workload, data size, concurrency, command, and relevant configuration. Keep setup and fixture work outside the timed operation when it is not part of the behavior being measured.

Use the benchmark API supported by the module's Go version. Prevent dead-code elimination and accidental state reuse. Record latency and allocation metrics that answer the task; a microbenchmark does not establish end-to-end throughput.

Compare repeated compatible runs with benchstat or equivalent statistical evidence. Run variants serially on shared hardware, account for noise and warmup, and distinguish statistical significance from a meaningful regression. Profiles show different quantities: cumulative allocation, retained heap, CPU samples, blocking, and timeline evidence are not interchangeable.

Read [benchmark patterns and commands](references/practices.md), [benchstat](references/benchstat.md), [pprof](references/pprof.md), or [trace](references/trace.md) for the operation. [Compiler analysis](references/compiler-analysis.md) and [diagnostic tools](references/tools.md) answer narrower questions. Use [CI regression detection](references/ci-regression.md) only for that requested workflow, and [investigation sessions](references/investigation-session.md) for production diagnosis.

Execute commands in the repository's permitted environment. Preserve useful raw results outside source unless the task asks for checked-in evidence; a benchmark request does not imply a commit or CI change.
