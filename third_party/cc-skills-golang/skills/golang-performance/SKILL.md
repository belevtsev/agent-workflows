---
name: golang-performance
description: Optimize a measured Go bottleneck using allocation, CPU, I/O, caching, or runtime techniques, with comparable evidence of benefit.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 🏎
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch Bash(benchstat:*) Bash(fieldalignment:*) Bash(staticcheck:*) Bash(curl:*) Bash(fgprof:*) Bash(perf:*) WebSearch AskUserQuestion EnterWorktree ExitWorktree
---

# Go performance changes

Start from the requested performance objective and evidence about the bottleneck. Separate CPU, allocation/GC, lock contention, I/O wait, and external-service latency before selecting an optimization.

Use comparable workloads, toolchains, hardware, concurrency, and warm/cold conditions. Change one causal factor at a time where practical, compare repeated measurements, and report uncertainty as well as the effect. Run competing benchmarks serially on shared hardware; parallel implementation does not make parallel measurement valid.

Preserve correctness, ownership, memory bounds, and cancellation. A cache needs explicit freshness/invalidation and capacity behavior; a pool must not leak state across users. Avoid runtime tuning that masks unbounded work.

Read [memory](references/memory.md), [CPU](references/cpu.md), [I/O](references/io-networking.md), [runtime](references/runtime.md), [caching](references/caching.md), or [cache failure modes](references/cache-failures.md) for the measured cause. [Measurement examples](references/practices.md) retain the investigation loop and [observability](references/observability.md) covers production signals. Use the benchmark specialist when comparison mechanics are needed. A plausible improvement is not a measured result.
