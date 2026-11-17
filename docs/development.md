# Development

Kitsu's CLI is one Go module: the command is `cmd/kitsu`, with its packages under `internal/`.
The root `install.sh` and `install.ps1` bootstrap artifact installation without Go.

## Build from source

This is the contributor path. To use Kitsu without a checkout or Go, follow
[release installation](release-install.md).

Install Git and the Go version named by `go.mod` (currently Go 1.27), then clone the source:

```sh
git clone https://github.com/belevtsev/kitsu.git
cd kitsu
```

On macOS or Linux:

```sh
GOWORK=off go install ./cmd/kitsu
```

On Windows, in PowerShell:

```powershell
$env:GOWORK = "off"
go install ./cmd/kitsu
```

Go writes the executable to `GOBIN`, or to the `bin` directory under `GOPATH` when `GOBIN` is
unset. Put that directory on `PATH`, open a new terminal, and run `kitsu version`.

Keep built executables outside the repository root: Git does not ignore a root `kitsu`, so it
becomes part of the content every check is judged on. [Try it](try-it.md) shows an isolated build.

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

`govulncheck` reads its database from the network, and installers download release artifacts.
Explicitly enabled native advisers may contact provider APIs. Help, skill discovery, completion,
agent diagnostics, task handoff preparation, and the first-party gate and token packages operate
offline. Default `task start` explicitly opens the selected interactive host, which can contact
its provider. Configured checks and adviser CLIs can use their own networks.

## Tests

Compact context tests use local Git repositories and fake check executables; they invoke no
paid host session. They cover source containment, raw-byte/cache freshness, exact budgets,
UTF-8, cursor reconstruction, receipt-before-baseline ordering, unchanged check outcomes,
large retained logs and concurrent evidence changes. The engine has a separate 2,000-line
production-code budget; existing component and prompt limits stay unchanged.

### Compact context benchmarks

```sh
GOWORK=off go test ./internal/cli -run '^TestCompactWorkflow' -count=1
GOWORK=off go test ./internal/cli -run '^$' -bench '^BenchmarkCompactWorkflows$' -benchtime=1x -count=5
```

These compare normal and compact exploration, role handoffs and noisy checks on the same
fixtures, charging request bytes and all required expansion responses. Report `calls/op`,
`request-B/op`, `response-B/op`, `expansions/op`, `cache-hits/op` and latency; cache hits here
are local artifacts. Include regressions and fixture limits, not only reductions. Repeated
measurements describe local latency variability, not statistically established model savings.
The [research protocol](context-research.md#measurement-protocol) defines separate opt-in live
host comparisons. Automated acceptance starts no paid model run.

The opt-in [Codex pilot harness](codex-compact-pilot.md) freezes real tasks, isolates
solvers and independent graders, captures descendant usage and prepares blind review.
Ordinary tests exercise fake processes only. The [completed pilot report](benchmarks/codex-compact-pilot-report.md)
keeps frozen test outcomes, posthoc defects, protocol evidence and incomplete usage separate.

Typed context and [saved worker handoffs](compact-context.md#prepare-and-finish-a-worker)
have local regression tests for parser/schema repairs, instruction/assignment fidelity,
private storage, receipt ordering, role-policy changes, write violations and current red/green
check evidence. They use fake check programs and invoke no models. Worker dispatch and local
stage bookkeeping have a separate 1,200-line production budget; the context engine's existing
2,000-line budget is unchanged. This implementation is not another paid pilot and supplies
no new token-saving result.

### Other test coverage

- Tests of a package sit beside it.
- Tests under `scenarios/` build `kitsu` and drive it and git in temporary
  repositories. Each is written from a scenario of a specification under
  `openspec/`, by someone other than the author of the code it checks. Set
  `KITSU_TEST_BINARY` to test a binary you built yourself.
- `internal/repocheck` checks the repository's own documents: relative links
  and anchors resolve, the changelog keeps short lines, no Markdown file
  holds a Mermaid diagram, and every diagram under `docs/diagrams/` has its
  spec, an SVG that is the export of that spec as far as an SVG records it,
  and a document that shows the SVG as an image. The test reads what the SVG
  records about itself; it renders nothing, so
  [a moved box or a rerouted arrow](../CONTRIBUTING.md#diagrams) is not found.

A test is named after the behaviour it checks.

Onboarding tests use isolated host configuration and fake executables to check installation,
enablement, hook disable settings, truthful unknown runtime evidence, handoff-only modes, and
literal interactive launch arguments. They do not authenticate or start live models. The
[onboarding change record](../openspec/changes/add-agent-onboarding/tests.md) separates those
checks from any observed real host behavior. Regenerate command documentation after metadata
changes with:

```sh
GOWORK=off go test ./internal/cli -run TestUXCommandReferenceIsGenerated -update-cli-reference
```

## Continuous integration

`.github/workflows/ci.yml` runs on pull requests and on `main`:

| Job | What it does |
| --- | --- |
| Native Go / ubuntu-latest | gofmt, `go vet`, `go test -race` |
| Native Go / macos-latest | gofmt, `go vet`, `go test` |
| Native Go / windows-latest | gofmt, `go vet`, `go test` |
| Lint and vulnerabilities | `golangci-lint` and `govulncheck` for linux, darwin and windows |
| Gate / kitsu run | Builds `kitsu` and runs the checks of `kitsu.toml` with `kitsu run --base` against the base revision |
| Release archives | Builds six archives, an SBOM for each, both installers, `SHA256SUMS`, and release notes. It publishes nothing: the artifact `kitsu-archives` holds them |
| Install without Go / ubuntu-latest, macos-latest, windows-latest | The `scenarios/installers` harness runs downloaded scripts with an isolated home and a `PATH` without Go or Git. It tests real archives through `KITSU_INSTALL_ARCHIVES`, version/revision, checksum rejection, updates, and `PATH`. Windows runs both PowerShell 7 and Windows PowerShell 5.1 |
| Plugin manifests | `claude plugin validate .` |

A thorough run is started by hand, with the input `thorough` of the workflow:
`gh workflow run ci.yml --ref <branch> -f thorough=true`. It uses the race
detector on macOS and Windows too and sets `KITSU_SLOW_TESTS=1`, so the test that
waits ten minutes for the default timeout runs. Start one after a change to
code written for one system: the first such run found a fault on Windows that
no other run showed.

## Releases

A release is made when the owner pushes a `v*` tag. `.github/workflows/release.yml`
then does four things, each only when the one before it passed:

1. It refuses a tag whose commit is not on `main`, or for which no run of CI
   started by a push or by hand has passed (`.github/scripts/release-guard.sh`),
   and a tag that `docs/release-install.md` does not name.
2. It runs the tests with the race detector, the lint and the vulnerability
   check.
3. It builds with `.goreleaser.yaml` the archives for linux, darwin and windows
   on amd64 and arm64, an SBOM for each, both installer scripts, and `SHA256SUMS`, uploads them to a
   draft release on GitHub, attests the build of each archive, and only then
   makes the release public. A tag with a
   hyphen, such as `v3.0.0-beta.2`, becomes a pre-release. The text of the
   release is the section of `CHANGELOG.md` for that version, or the section
   `Unreleased` while the changelog has none
   (`.github/scripts/release-notes.sh`).
4. It tests the published installer assets on Linux, macOS, and Windows through
   `scenarios/installers`, with Go and Git absent from the install environment's `PATH`, and
   verifies the installed version and revision. Windows covers PowerShell 5.1 and 7.

The former `cw` release pipeline ran once for `v3.0.0-beta.1` on 15 November
2026; all four steps passed using that version's README instructions. That is
historical evidence, not a published Kitsu release. Kitsu `v3.0.0-beta.2`
is prepared but unpublished. The guard has refused a commit only in its tests.

CI runs the commands of step 2 on every commit. Its job `Release archives`
builds the six archives in the same way, and the same text, from the commit,
and its jobs `Install without Go` install from them. Those archives are not byte for byte
the ones a release publishes: Go writes the version of the module into the
program, and that is the tag only once the tag exists.

To try the archives of a commit before it is tagged:

```sh
gh run download <run of CI> --name kitsu-archives --dir kitsu-archives
```

To publish, the owner does two things:

1. Merge the branch into `main` and wait until the run of CI for the merge
   commit has passed.
2. Tag that commit with the tag `docs/release-install.md` names and push the tag:
   `git tag <tag> <commit>` and `git push origin <tag>`.

After a release, give the section `Unreleased` of the changelog the heading
of that version, and start a new section `Unreleased` above it.

The README, release installation guide, installer defaults, and plugin/marketplace manifests
name the version; change them together. After the first Kitsu release is live and its installation
jobs pass, remove the unpublished notices. Until then, the installers can use a downloaded
[`kitsu-archives` CI artifact](release-install.md#install-a-ci-artifact-before-publication) without
a checkout or Go. Never point a Kitsu command at the old `cw` archives. Preparing the installers
does not tag or publish a release.
