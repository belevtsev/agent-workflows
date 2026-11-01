# Instrumenting for on-call

Instrumentation is written with the feature, the same way tests are. If a feature ships without telemetry, the first reported bug becomes archaeology instead of a query.

## Start from questions

Telemetry without a question is noise. Before adding any, write down two to four questions on-call will ask about this feature, then check that every signal answers one. If you can't name the questions, you'll log everything and learn nothing.

```text
Feature: payment retry
1. What fraction of payments succeed first time, and after a retry?
2. When a payment fails for good, why?
3. Is the provider slower than usual?
```

Metrics tell you that something is wrong, traces tell you where, and logs tell you why.

## Name the entry point

A correlation ID identifies a run. It doesn't say which code path started it. When a scheduler, a replay endpoint, and a manual command all feed one log, their lines are interchangeable, and working out which produced one means cross-reading other records that may no longer exist. Stamp the entry point where the run starts:

```go
// runLogger is the one place a run's logger is built, so every line carries both
// fields. Pass both across queues and calls, so a worker never has to guess.
func runLogger(base *slog.Logger, entryPoint, runID string) *slog.Logger {
	return base.With("entry_point", entryPoint, "run_id", runID)
}
```

## Verify the telemetry itself

Instrumentation is code, so it can be wrong. Before calling the work done, trigger the paths and look at the real output:

- Force an error, find it in the logs by its ID, and confirm the fields are structured.
- Send test traffic, and confirm the metric series appear with the expected labels and sane values.
- Follow one request across services, and check for broken spans.
- Fire each new alert once by lowering its threshold, and confirm it reaches the right channel and its runbook link works.
- Induce a failure and locate it from telemetry alone, without reading the source.

## Runbooks

An alert needs a runbook, even three lines: what it means, what to check first, and who to call. Add steps only when the first check isn't enough to decide. A short runbook covering the three common causes beats a long one that gets skimmed. Update it when closing any incident that used it, because a stale runbook builds false confidence.

_Adapted from `observability-and-instrumentation` in `addyosmani/agent-skills`, and rewritten for Go._
