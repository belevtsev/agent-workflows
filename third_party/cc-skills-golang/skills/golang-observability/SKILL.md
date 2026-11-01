---
name: golang-observability
description: Add or diagnose Go logs, metrics, traces, profiling, or alerts for a specific operational question, preserving privacy and bounded overhead.
license: MIT
metadata:
  author: samber
  version: 1.3.1
  openclaw:
    emoji: 📡
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
    allowed-tools: Read Edit Write Glob Grep Bash(go:*) Bash(golangci-lint:*) Bash(git:*) Agent WebFetch WebSearch AskUserQuestion
---

# Go operational signals

Choose the signal needed to answer the task's operational question: logs for events, metrics for aggregate behavior, traces for request paths, and profiles for runtime cost. Reuse the project's existing stack and conventions.

Keep labels and event dimensions bounded, correlate across asynchronous boundaries deliberately, and avoid secrets or personal data in telemetry. Protect diagnostic endpoints. Sampling, retention, and profiling overhead should fit the workload and authorization for live instrumentation.

Distinguish process health from readiness and business success. Track failure and saturation where the user needs to act; alerts need an actionable condition and enough duration to avoid flapping. Adding one feature does not mandate a new telemetry product or analytics pipeline.

Read [logging](references/logging.md), [metrics](references/metrics.md), [tracing](references/tracing.md), [profiling](references/profiling.md), [alerting](references/alerting.md), or [dashboards](references/dashboards.md) for the requested signal. For a new feature or a new alert, read [instrumenting for on-call](references/on-call-instrumentation.md). [Correlation examples](references/practices.md) cover combined signals and logger migration. Read [user analytics](references/rum.md) only when analytics is explicitly in scope and its data collection is authorized.
