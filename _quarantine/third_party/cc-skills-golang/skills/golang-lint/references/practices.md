# Go Linting

Read the section needed for the current task. Commands are examples to adapt to the repository's permitted environment and selected toolchain; setup, broad audits, and external actions require their own task scope.


## Overview

`golangci-lint` is the standard Go linting tool. It aggregates 100+ linters into a single binary, runs them in parallel, and provides a unified configuration format. Use it according to the project's development and CI workflow.

When the project uses `.golangci.yml`, treat it as the source of truth for its enabled linters and configuration. See the [recommended configuration](../assets/.golangci.yml) for a production-ready setup with 48 linters enabled.

## Quick Reference

```bash
# Run all configured linters
golangci-lint run ./...

# Auto-fix issues where possible
golangci-lint run --fix ./...

# Format code (golangci-lint v2+)
golangci-lint fmt ./...

# Run a single linter only
golangci-lint run --enable-only govet ./...

# List all available linters
golangci-lint linters

# Verbose output with timing info
golangci-lint run --verbose ./...
```

## Configuration

The [recommended .golangci.yml](../assets/.golangci.yml) provides a production-ready setup with 33 linters. For configuration details, linter categories, and per-linter descriptions, see the **[linter reference](linter-reference.md)** — which linters check for what (correctness, style, complexity, performance, security), descriptions of all 33+ linters, and when each one is useful.

## Suppressing Lint Warnings

Use `//nolint` directives sparingly — fix the root cause first.

```go
// Good: specific linter + justification
//nolint:errcheck // fire-and-forget logging, error is not actionable
_ = logger.Sync()

// Bad: blanket suppression without reason
//nolint
_ = logger.Sync()
```

Rules:

1. **//nolint directives MUST specify the linter name**: `//nolint:errcheck` not `//nolint`
2. **//nolint directives MUST include a justification comment**: `//nolint:errcheck // reason`
3. **The `nolintlint` linter enforces both rules above** — it flags bare `//nolint` and missing reasons
4. **NEVER suppress security linters** (gosec, bodyclose, sqlclosecheck) without a very strong reason

For comprehensive patterns and examples, see **[nolint directives](nolint-directives.md)** — when to suppress, how to write justifications, patterns for per-line vs per-function suppression, and anti-patterns.

## Interpreting Output

Each issue follows this format:

```
path/to/file.go:42:10: message describing the issue (linter-name)
```

The linter name in parentheses tells you which linter flagged it. Use this to:

- Look up the linter in the [reference](linter-reference.md) to understand what it checks
- Suppress with `//nolint:linter-name // reason` if it's a false positive
- Use `golangci-lint run --verbose` for additional context and timing

## Common Issues

| Problem | Solution |
| --- | --- |
| "deadline exceeded" | Set or increase `run.timeout` in `.golangci.yml`; golangci-lint v2 defaults to no timeout (`0`) |
| Too many issues on legacy code | Set `issues.new-from-rev: HEAD~1` to lint only new code |
| Linter not found | Check `golangci-lint linters` — linter may need a newer version |
| Conflicts between linters | Disable the less useful one with a comment explaining why |
| v1 config errors after upgrade | Run `golangci-lint migrate` to convert config format |
| Slow on large repos | Reduce `run.concurrency` or exclude paths with `linters.exclusions.paths` / `formatters.exclusions.paths` |

## Go 1.27 compatibility gate

`go test` runs the `stdversion` vet analyzer by default in Go 1.27. Keep the module's `go` directive aligned with every standard-library symbol the code uses; do not suppress the diagnostic to hide an accidental baseline increase.

Before introducing Go 1.27 generic methods, verify the complete source-tool pipeline with a representative fixture:

```bash
go version
gopls version
golangci-lint version
go test ./...
golangci-lint run ./...
```

A golangci-lint binary built with Go 1.27 may still embed an older parser or `x/tools` revision. Treat successful parsing, type checking, and configured-linter execution as the compatibility proof. If a required tool fails, keep code on compatible syntax and report the blocker; never disable linting simply to accept new syntax.

