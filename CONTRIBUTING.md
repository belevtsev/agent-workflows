# Contributing

Thanks for helping improve `cw`. This file explains how to build, test and propose changes.

Everyone who takes part follows the [code of conduct](CODE_OF_CONDUCT.md).

## Build and test

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

Lint and check reachable vulnerabilities with the versions CI pins, for the three release platforms, as CI does:

```sh
GOWORK=off go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.15.0
GOWORK=off go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
GOWORK=off golangci-lint run ./... && GOWORK=off GOOS=darwin golangci-lint run ./... && GOWORK=off GOOS=windows golangci-lint run ./...
GOWORK=off govulncheck ./... && GOWORK=off GOOS=darwin govulncheck ./... && GOWORK=off GOOS=windows govulncheck ./...
```

`GOWORK=off go test ./internal/repocheck` checks the Markdown links, the anchors and the changelog on its own. CI runs gofmt, vet and the tests on Linux, macOS and Windows, golangci-lint and govulncheck, and validates the plugin manifests. See [development](docs/development.md) for details.

## Go-only automation

First-party automation is written in Go. If you need a helper, add it to the `cw` command or to a test.

## Diagrams

archify is the only diagram tool; do not add diagram sources in any other format, and no Mermaid. It is not part of this repository: get it from the branch `workflow-v3` (`third_party/archify`). Each diagram is a JSON spec under `docs/diagrams/` with a static SVG export next to it. Edit the spec, never the SVG, and commit both together.

## Pull requests

- One topic per pull request.
- Pull requests stay under about 600 changed lines, unless most of the change is deletion. Split a larger change into several pull requests and say so in each description.
- Behaviour changes need tests.
- Add an entry under `## [Unreleased]` in `CHANGELOG.md`, in Breaking, Added, Changed, Fixed or Removed: one bullet of at most two lines that says what changed for a user and links the doc with the detail; a Breaking bullet names the action a user takes. No line above the first released version may exceed 200 characters (`go test ./internal/repocheck` checks it).
- Update docs when commands or install behaviour change.
- Run the build, vet, test, lint and vulnerability commands above before requesting review.

Releases are published when a `v*` tag is pushed. Maintainers create tags; contributors do not need to.

## Built with itself

`cw.toml` declares this repository's checks: build, vet, test, lint, and `cw change check` on the task records under `openspec/changes/`. Run `cw init` in your clone to install the hooks; then a commit needs a passing `cw run`. Every commit written with an agent names its model in a `Co-Authored-By` trailer. Whether the gate or the roles improve results has not been measured yet.

## License

By contributing, you agree that your contributions are licensed under the MIT License in `LICENSE`.
