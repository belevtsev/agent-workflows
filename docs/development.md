# Development

`cw` is one Go module. The command is `cmd/cw`; everything else is under
`internal/`. Files under `_quarantine/` left the core: Go ignores that
directory, and no check reads it.

## Checks

Run these before a commit. CI runs the same ones.

```sh
go build ./...
go vet ./...
go test -race -count=1 ./...
GOOS=darwin go vet ./... && GOOS=windows go vet ./...
golangci-lint run ./...
govulncheck ./...
```

The versions CI pins:

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
```

`govulncheck` reads its database from the network. Nothing else here does,
and `cw` itself never does.

## Tests

- Tests of a package sit beside it.
- Tests under `scenarios/` build `cw` and drive it and git in temporary
  repositories. Each is written from a scenario of a specification under
  `openspec/`, by someone other than the author of the code it checks. Set
  `CW_TEST_BINARY` to test a binary you built yourself.
- `internal/repocheck` checks the repository's own documents: relative links
  and anchors resolve, the changelog keeps short lines, and no Markdown file
  holds a Mermaid diagram.

A test is named after the behaviour it checks.

## Continuous integration

`.github/workflows/ci.yml` runs on pull requests and on `main`:

| Job | What it does |
| --- | --- |
| Native Go / ubuntu-latest | gofmt, `go vet`, `go test -race` |
| Native Go / macos-latest | gofmt, `go vet`, `go test` |
| Native Go / windows-latest | gofmt, `go vet`, `go test` |
| Lint and vulnerabilities | `golangci-lint` and `govulncheck` for linux, darwin and windows |
| Gate / cw run | Builds `cw` and runs the checks of `cw.toml` with `cw run --base` against the base revision |
| Plugin manifests | `claude plugin validate .` |

A thorough run is started by hand, with the input `thorough` of the workflow:
`gh workflow run ci.yml --ref <branch> -f thorough=true`. It uses the race
detector on macOS and Windows too and sets `CW_SLOW_TESTS=1`, so the test that
waits ten minutes for the default timeout runs. Start one after a change to
code written for one system: the first such run found a fault on Windows that
no other run showed.

## Releases

A release is made when the owner pushes a `v*` tag. `.goreleaser.yaml` builds
archives for linux, darwin and windows on amd64 and arm64, and `SHA256SUMS`.
