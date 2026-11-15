# Development

`cw` is one Go module. The command is `cmd/cw`; everything else is under
`internal/`.

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
  and anchors resolve, the changelog keeps short lines, no Markdown file
  holds a Mermaid diagram, and every diagram under `docs/diagrams/` has its
  spec, an SVG that is the export of that spec as far as an SVG records it,
  and a document that shows the SVG as an image. The test reads what the SVG
  records about itself; it renders nothing, so
  [a moved box or a rerouted arrow](../CONTRIBUTING.md#diagrams) is not found.

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
| Release archives | Builds the six archives, an SBOM for each and `SHA256SUMS` as a release does, named for the tag that the README installs, and the text of the release. It publishes nothing: the artifact `cw-archives` of the run holds them |
| Install without Go / ubuntu-latest, macos-latest, windows-latest | Follows the first install instruction of the README on a `PATH` without `go`, with those archives served from the runner, and compares what `cw version` prints with the tag and the commit. Then it follows the instruction again with one byte of the archive changed, which must install nothing. On Windows it does both in PowerShell 7 and in Windows PowerShell 5.1 |
| Plugin manifests | `claude plugin validate .` |

A thorough run is started by hand, with the input `thorough` of the workflow:
`gh workflow run ci.yml --ref <branch> -f thorough=true`. It uses the race
detector on macOS and Windows too and sets `CW_SLOW_TESTS=1`, so the test that
waits ten minutes for the default timeout runs. Start one after a change to
code written for one system: the first such run found a fault on Windows that
no other run showed.

## Releases

A release is made when the owner pushes a `v*` tag. `.github/workflows/release.yml`
then does four things, each only when the one before it passed:

1. It refuses a tag whose commit is not on `main`, or for which no run of CI
   started by a push or by hand has passed (`.github/scripts/release-guard.sh`),
   and a tag that the install instructions of the README do not name.
2. It runs the tests with the race detector, the lint and the vulnerability
   check.
3. It builds with `.goreleaser.yaml` the archives for linux, darwin and windows
   on amd64 and arm64, an SBOM for each and `SHA256SUMS`, uploads them to a
   draft release on GitHub, attests the build of each archive, and only then
   makes the release public. A tag with a
   hyphen, such as `v3.0.0-beta.1`, becomes a pre-release. The text of the
   release is the section of `CHANGELOG.md` for that version, or the section
   `Unreleased` while the changelog has none
   (`.github/scripts/release-notes.sh`).
4. It follows the first install instruction of the README on Linux, macOS and
   Windows, exactly as written, on a `PATH` without `go`. This fails when the
   README names another tag.

The four steps ran once, for the tag `v3.0.0-beta.1` on 15 November 2026, and
each passed. What that run could not show is a refusal by step 1: the guard
has refused a commit only in the test of its script.

CI runs the commands of step 2 on every commit. Its job `Release archives`
builds the six archives in the same way, and the same text, from the commit,
and its jobs `Install without Go` install from them. Those archives are not byte for byte
the ones a release publishes: Go writes the version of the module into the
program, and that is the tag only once the tag exists.

To try the archives of a commit before it is tagged:

```sh
gh run download <run of CI> --name cw-archives --dir cw-archives
```

To publish, the owner does two things:

1. Merge the branch into `main` and wait until the run of CI for the merge
   commit has passed.
2. Tag that commit with the tag the README names and push the tag:
   `git tag <tag> <commit>` and `git push origin <tag>`.

After a release, give the section `Unreleased` of the changelog the heading
of that version, and start a new section `Unreleased` above it.

The README, `.claude-plugin/plugin.json`, `.claude-plugin/marketplace.json` and
`.codex-plugin/plugin.json` name the version; change all of them together.
