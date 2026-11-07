---
name: observability
description: Language-neutral principles for actionable errors, logs, and metrics at boundaries, with privacy and without noise.
disable-model-invocation: true
---

# Observability

Use this when a change adds or reviews errors, logs, metrics or traces, or when a failure was hard to diagnose. It holds principles that apply in any language, with Go examples. For Go libraries and setup (`log/slog` handlers, Prometheus, OpenTelemetry, pprof, alerts, dashboards), use `golang-observability`.

Every signal must answer a question someone will actually ask: an operator during an incident, a user whose command failed, or a developer reading a bug report. If you can't name the question and who acts on the answer, don't add the signal.

## Instrument the boundaries

Record where work enters and leaves the process, not inside every function:
- requests and commands in, with their outcome and duration;
- calls out to databases, services, files and child processes;
- messages consumed and produced, and background jobs started and finished;
- process start, configuration loaded, readiness, and shutdown.

Inside the boundary, return errors with context and let the boundary report them once.

## Errors carry the context

An error should say what failed, on what, and why, so the reader doesn't need the log to understand it. Each layer adds what its caller doesn't already know, and nothing else:

```go
func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err) // err already names the path
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %s: %w", path, err)
	}
	return cfg, nil
}
```

- **Report each error once.** Log it or return it, not both: logging at every layer turns one failure into five lines with no new facts. The boundary that handles the error reports it.
- **Keep chains inspectable.** Wrap with `%w`, so callers can still match a cause with `errors.Is` or `errors.As` (`golang-error-handling`).
- **Make user-facing errors actionable.** A CLI prints what failed, the likely cause, and what to do next, on stderr, with a non-zero exit status. A stack trace is for developers, behind a verbose flag.
- **Never hide a failure.** A logged-and-dropped error, a fallback nobody asked for, and retries that exhaust and then report success all hide one.

## Logs are events someone acts on

One structured line per event at the boundary: a stable message, plus fields for the operation, identifiers, outcome and duration.

```go
// The job runner is the boundary that handles the error: it reports the
// outcome once and moves on to the next job, instead of returning it upward.
start := time.Now()
err := h.process(ctx, job)
attrs := []slog.Attr{
	slog.String("job_id", job.ID),
	slog.String("kind", job.Kind),
	slog.Duration("duration", time.Since(start)),
}
if err != nil {
	h.log.LogAttrs(ctx, slog.LevelError, "job failed", append(attrs, slog.Any("error", err))...)
	return
}
h.log.LogAttrs(ctx, slog.LevelInfo, "job done", attrs...)
```

- **Error** means someone must act, and **Warn** means degraded but handled. **Info** is for lifecycle events and one line per unit of work at most. **Debug** is off by default.
- Never log inside a hot loop. Count the items and log the summary, or sample.
- Rate-limit or deduplicate a repeating failure: one line with a count beats ten thousand identical lines.
- Message text and field names are a contract once dashboards and alerts read them. Rename them only with the same care as an API field.
- Leave health checks and expected misses out of the logs. They are noise that hides real events.

## Metrics answer "how much" and "how often"

- At request boundaries, record **rate, errors and duration**. For resources, record **utilization, saturation and errors**: pool in use, queue depth, and the age of the oldest item.
- Count the work you shed or drop, and the retries you make. Silent drops are the hardest failures to find.
- **Keep label values bounded.** Never use user IDs, raw paths with IDs, error strings or timestamps as labels. Map them to a small fixed set:

```go
// outcome maps any error to one of a few label values, so a new error message
// never creates a new time series.
func outcome(err error) string {
	switch {
	case err == nil:
		return "ok"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "error"
	}
}
```

- Distinguish liveness (the process runs), readiness (it can serve now) and success (the business outcome happened). A green health check with failing requests is a monitoring bug.

## Traces follow causality

Propagate trace context across every asynchronous hop: queues, goroutines, scheduled jobs. Otherwise the trace ends where the problem starts. Put spans on boundaries and slow calls, not on every function.

## Privacy

Telemetry is copied, retained and read by many people. Never record secrets, tokens, credentials, session cookies, full request or response bodies, or personal data that the operator doesn't need.
- Log fields from an allowlist. Never dump a whole struct, request or environment.
- Redact by type, so passing the value as a log attribute can't leak it:

```go
// Token prints as its last four characters wherever it is logged.
type Token string

func (t Token) LogValue() slog.Value {
	if len(t) <= 4 {
		return slog.StringValue("****")
	}
	return slog.StringValue("****" + string(t[len(t)-4:]))
}
```

- That covers only `slog` attributes of that type. `fmt`, `log.Printf` and a struct that holds a `Token` field, logged with `slog.Any`, still print the raw value. So keep the allowlist too.
- URLs carry credentials in user info and query strings. Strip them before logging.

## Verify

Trigger the failure path in a test or locally, and read what a person would see: the error text, the log line, the metric. Check that it names the operation and the object, contains no secret, and appears once. For a new metric label, list its possible values; the list must be finite and short. Reuse the project's existing logging, metrics and tracing stack.
