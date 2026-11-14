# Go Dependency Management

Read the section needed for the current task. Commands are examples to adapt to the repository's permitted environment and selected toolchain; setup, broad audits, and external actions require their own task scope.


## Key Rules

- Track applicable `go.sum` changes with the dependency edit under the repository convention. `go.sum` records downloaded-module checksums; it is not the selected-version lockfile
- Use `govulncheck` when the task needs reachable-vulnerability evidence or the repository requires it for release
- Maintenance status, license compatibility, and stdlib alternatives are important considerations before adding a dependency — every dependency increases attack surface, maintenance burden, and binary size
- Use `go mod tidy` to reconcile an authorized dependency edit when needed, and inspect changes to the selected module graph

## go.mod & go.sum

### Essential Commands

| Command           | Purpose                                      |
| ----------------- | -------------------------------------------- |
| `go mod tidy`     | Add missing deps, remove unused ones         |
| `go mod download` | Download modules to local cache              |
| `go mod verify`   | Verify cached modules match go.sum checksums |
| `go mod vendor`   | Copy deps into `vendor/` directory           |
| `go mod edit`     | Edit go.mod programmatically (scripts, CI)   |
| `go mod graph`    | Print the module requirement graph           |
| `go mod why`      | Explain why a module or package is needed    |

### Vendoring

Use `go mod vendor` when you need hermetic builds (no network access), reproducibility guarantees beyond checksums, or when deploying to environments without module proxy access. CI pipelines and Docker builds sometimes benefit from vendoring. Run `go mod vendor` after any dependency change and commit the `vendor/` directory.

## Installing & Upgrading Dependencies

### Adding a Dependency

```bash
go get github.com/google/uuid          # Latest version
go get github.com/google/uuid@v1.6.0   # Specific version
go get github.com/google/uuid@latest   # Explicitly latest
go get github.com/google/uuid@<commit> # Specific commit (pseudo-version)
```

Before pinning a version, inspect the module's available versions, importers, and known vulnerabilities on pkg.go.dev → See `samber/cc-skills-golang@golang-pkg-go-dev` skill.

### Upgrading

```bash
go get -u ./...            # Upgrade ALL direct+indirect deps to latest minor/patch
go get -u=patch ./...      # Upgrade to latest patch only (safer)
go get github.com/pkg@v1.5 # Upgrade specific package
```

**Prefer `go get -u=patch`** for routine updates. Patch and minor updates are usually lower risk than major upgrades, but still require review. For dependency updates, run:

```bash
go get -u=patch ./...
go mod tidy
go test ./...
go vet ./...
govulncheck ./...   # or: go tool govulncheck ./...
```

Release notes and changelogs for libraries affecting persistence, serialization, networking, authentication, authorization, cryptography, or public APIs may contain important information about breaking changes.

### Removing a Dependency

```bash
go get github.com/google/uuid@none  # Mark for removal
go mod tidy                          # Clean up go.mod and go.sum
```

### Installing CLI Tools

For Go 1.24+ modules, pin executable tools in `go.mod` with `tool` directives. Do not create a new `tools.go` blank-import file unless the module must support Go <1.24.

```bash
# Add tools to the current module.
go get -tool github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest
go get -tool golang.org/x/vuln/cmd/govulncheck@latest
go get -tool golang.org/x/perf/cmd/benchstat@latest

# Run pinned tools reproducibly.
go tool golangci-lint run ./...
go tool govulncheck ./...
go tool benchstat old.txt new.txt

# Install all module-pinned tools into GOBIN/PATH when needed.
go install tool

# Update pinned tools deliberately, then review go.mod/go.sum.
go get -u tool
go mod tidy
```

`go.mod` shape for a module targeting Go 1.27 or newer. This is an example target, not a cap; keep the project's actual `go` directive and do not change it just to add tools.

```go.mod
module example.com/project

go 1.27

tool (
    github.com/golangci/golangci-lint/v2/cmd/golangci-lint
    golang.org/x/vuln/cmd/govulncheck
    golang.org/x/perf/cmd/benchstat
)
```

For Go <1.24 only, use the legacy `tools.go` blank-import workaround:

```go
//go:build tools

package tools

import (
    _ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"
    _ "golang.org/x/vuln/cmd/govulncheck"
)
```

Rule: Go 1.24+ = `tool` directives. Go <1.24 = `tools.go` fallback.

### Go 1.27 module target and tidy behavior

Do not infer the supported language version from the installed toolchain. If the project intentionally adopts Go 1.27 APIs or syntax, update the directive deliberately:

```bash
go mod edit -go=1.27
go mod tidy
go test ./...
```

For Go 1.27+ modules, `go mod tidy` consolidates duplicate `require` blocks into at most one direct and one indirect block. Review and keep the canonical result instead of hand-restoring redundant groups.

Go 1.27's default `stdversion` vet check reports standard-library symbols newer than the module's `go` directive. Do not suppress it; either keep compatible APIs or raise the supported version explicitly.

Before adding a third-party UUID dependency to a Go 1.27 module, check the standard `uuid` package. It covers common generation and parsing (`New`, `NewV4`, `NewV7`, `Parse`, `MustParse`). Keep or add a third-party implementation only for concrete missing capabilities such as namespace hashing, byte conversion helpers, or custom generators.

## Deep Dives

- **[Versioning & MVS](versioning.md)** — Semantic versioning rules (major.minor.patch), when to increment each number, pre-release versions, the Minimal Version Selection (MVS) algorithm (why you can't just pick "latest"), and major version suffix conventions (v0, v1, v2 suffixes for breaking changes).

- **[Auditing Dependencies](auditing.md)** — Vulnerability scanning with `govulncheck`, tracking outdated dependencies, analyzing which dependencies make the binary large (`goweight`), and distinguishing test-only vs binary dependencies to keep `go.mod` clean.

- **[Dependency Conflicts & Resolution](conflicts.md)** — Diagnosing version conflicts (what `go get` does when you request incompatible versions), resolution strategies (`replace` directives for local development, `exclude` for broken versions, `retract` for published versions that should be skipped), and workflows for conflicts across your dependency tree.

- **[Go Workspaces](workspaces.md)** — `go.work` files for multi-module development (e.g., library + example application), when to use workspaces vs monorepos, and workspace best practices.

- **[Automated Dependency Updates](automated-updates.md)** — Setting up Dependabot or Renovate for automatic dependency update PRs, auto-merge strategies (when to merge automatically vs require review), and handling security updates.

- **[Visualizing the Dependency Graph](visualization.md)** — `go mod graph` to inspect the full dependency tree, `modgraphviz` to visualize it, and interactive tools to find which dependency chains cause bloat.

## Quick Reference

```bash
# Start a new module
go mod init github.com/user/project

# Add a dependency
go get github.com/google/uuid@v1.6.0

# Upgrade all deps (patch only, safer)
go get -u=patch ./...

# Remove unused deps
go mod tidy

# Check for vulnerabilities
govulncheck ./...   # or: go tool govulncheck ./...

# Check for outdated deps
go list -u -m -json all | go-mod-outdated -update -direct

# Analyze binary size by dependency
goweight

# Understand why a dep exists
go mod why -m github.com/some/module

# Visualize dependency graph
go mod graph | modgraphviz | dot -Tpng -o deps.png

# Verify checksums
go mod verify
```
