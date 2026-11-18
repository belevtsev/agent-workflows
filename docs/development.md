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
its provider. Explicit publication confirmation reads the remote through Git and, for a PR,
uses the authenticated GitHub CLI. Configured checks and adviser CLIs can use their own networks.

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
| Plugin manifests | `claude plugin validate .claude-plugin/marketplace.json` and `claude plugin validate .claude-plugin/plugin.json --strict` |

A thorough run is started by hand, with the input `thorough` of the workflow:
`gh workflow run ci.yml --ref <branch> -f thorough=true`. It uses the race
detector on macOS and Windows too and sets `KITSU_SLOW_TESTS=1`, so the test that
waits ten minutes for the default timeout runs. Start one after a change to
code written for one system: the first such run found a fault on Windows that
no other run showed.

## Releases

Kitsu versioning starts at `v0.0.1` after the 17 November 2026 reset. The installer defaults,
plugin manifests, CI archive labels and tagged install examples must agree on the version being released.

The **Release** workflow accepts a semantic version tag on protected `main`. It requires
a successful CI run on `main` for the exact dispatch commit and confirms that the shell and
PowerShell examples use the supplied tag.

1. Read-only jobs test, lint and check vulnerabilities with caches disabled. GoReleaser builds six
   platform archives and their SBOMs locally. A single artifact holds the matching plugin, both
   installer scripts, release notes and a checksum manifest for the archives, SBOMs, plugin and installers.
2. Linux, macOS, and Windows on amd64 and arm64 install the exact artifact with Go and Git absent
   from the installer's PATH. The tests check the CLI tag and revision and the matching plugin.
   Windows covers PowerShell 5.1 and 7.
3. A job with attestation permissions signs the archives, plugin, installer scripts and
   checksum manifest. The protected publisher creates a tag at the tested commit, uploads the
   files to a draft, and compares GitHub's asset digests against the bundle before publishing.
   A tag such as `v0.1.0-rc.1` produces a pre-release and leaves the latest stable download on
   the previous release. The release notes come from that version's section of `CHANGELOG.md`,
   or from `Unreleased` when the section is absent.

See the [release bundle tool](../internal/releasecmd/README.md) for assembly and draft verification.

The CI workflow also produces preview archives and tests the installers. The release workflow
rebuilds the publication bundle from the exact dispatch commit. Preparing either bundle requires
only read access to the repository.

### Configure release protection

A repository admin sets up these controls before the first release using this workflow.

- Protect `main` and require CI before merge. Create a `release` environment that allows
  deployments only from the branch `main`, requires a release reviewer, and disables admin
  bypass of protection. Enable self-review prevention when there are two release maintainers.
  [GitHub deployment environments](https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments)
- Create a dedicated GitHub App installed only on `belevtsev/kitsu` with repository permissions
  Contents write and Administration read. Store its client ID as the environment variable
  `KITSU_RELEASE_APP_CLIENT_ID` and its private key as the environment secret
  `KITSU_RELEASE_APP_PRIVATE_KEY`. The job uses these to mint a short-lived installation token.
  [GitHub App authentication in workflows](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/making-authenticated-api-requests-with-a-github-app-in-a-github-actions-workflow)
- Create an active tag ruleset for `refs/tags/v*` that restricts creation. Add the release App as
  its sole bypass actor. Create a second active ruleset for the same pattern that restricts
  updates and deletions with no bypass actors. [GitHub tag rulesets](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/creating-rulesets-for-a-repository)
- Enable **Immutable releases** in repository settings. The publisher checks the setting before
  creating the tag or draft. [GitHub immutable releases](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases)

To try the archives of a commit before it is tagged:

```sh
gh run download <run of CI> --name kitsu-archives --dir kitsu-archives
```

To publish:

1. Choose the release version and intended scope. Align the release-install guide's shell and
   PowerShell tag lines, installer defaults, plugin/marketplace manifests and version-consistency
   tests. Update README/security publication text and the changelog for that version.
2. Merge the branch into `main` and wait until the run of CI for the merge
   commit has passed.
3. Dispatch `gh workflow run release.yml --ref main -f tag=<tag>` while that commit is at
   the tip of `main`. Check the workflow revision and tag when approving the release deployment.
4. Confirm the release is immutable and try the README install command to check the reported
   version and revision. See [provenance verification](release-install.md#verify-release-provenance).

If an upload fails, rerun the failed jobs on the original execution so the workflow keeps the
same revision. A draft can resume when its tag, revision, and existing asset digests match the
tested bundle. Inspect a conflicting tag or draft before retrying.

If reusing a deleted version name, review the existing changelog heading before tagging:
`release-notes.sh` prefers that version's section over `Unreleased`, so historical notes could
otherwise become the new release description. Current `TestPreparedReleaseVersions` also rejects
the former `cw v3.0.0-beta.1` name; reusing it requires an intentional update to that check and
the version contract, not just recreating a tag.

When preparing a release, give the section `Unreleased` of the changelog the heading
of that version, and start a new section `Unreleased` above it.

CI reads the single `tag=v<version>` line from the release installation guide to name its archives.
The release workflow requires that line and the matching PowerShell assignment to match the dispatch tag.
Keep both examples aligned with the installer and manifest versions even while no release exists.
Until publication, the installers can use a downloaded
[`kitsu-archives` CI artifact](release-install.md#install-a-ci-artifact-before-publication) without
a checkout or Go. Use scripts and archives from the same artifact, and verify the commit shown
by `kitsu version`; multiple CI builds can share the same version label. Preparing the installers
does not create a tag or release.

The [native Linux setup replay](benchmarks/kitsu-product-audit-2026-11-18.md#native-host-discovery-and-git-delivery) installs a local archive, checks plugin discovery in both hosts and resumes a verified CLI commit.
