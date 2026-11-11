# Go development and releases

`cw` and its development automation use the Go version pinned in go.mod.
First-party code, including the helpers skills call, is Go. Third-party skills under
third_party/ keep their own dependencies. No Go module vendor tree is present.
The resource-only third_party module boundary keeps bundled example Go files
out of `cw`'s build and test package discovery.

`cmd/cw` is a thin command entrypoint over `internal/cli`, which has injectable
I/O. The runtime packages are `internal/taskflow`, `internal/consult`,
`internal/projectctx`, `internal/graphify`, `internal/outcomes` and
`internal/usagelog`; `internal/codexinstall` implements `cw install --host codex`
and `cw uninstall --host codex`. `cmd/cwdev` holds the maintainer
evaluation.

## Verification

```sh
GOWORK=off go build ./... && GOWORK=off go vet ./... && GOWORK=off go test -race -count=1 ./...
```

Use `go test` with a focused package or test filter while developing. Tests use
temporary homes, synthetic Git repositories and local HTTP servers. They never
change the user's configuration, consult hosted decision models, or require service
credentials.

### Repository content checks

```sh
GOWORK=off go test ./internal/skillcheck
```

`internal/skillcheck` checks the repository content rather than Go code:

- each skill's `name` is at most 64 lowercase letters, digits and hyphens and
  matches its directory, and its `description` has 1 to 1024 characters;
- relative Markdown links in skills, top-level Markdown files, `docs/` and
  `.github/` resolve;
- `disable-model-invocation` in `SKILL.md` agrees with
  `policy.allow_implicit_invocation` in `agents/openai.yaml`;
- the paths in `skills-manifest.json` and the plugin manifests exist;
- the model policy loads;
- the pack manifests under `packs/` load, name only evidence rules built into
  `cw`, reference skills that exist, and equal the copies embedded in `cw`;
- outside `skills/` and `third_party/` there is no `.sh`, `.py`, `.bash`, `.zsh`
  or `requirements*.txt` file and no `#!` script for `sh`, `bash`, `zsh` or
  Python, except each eval case's `evals/**/scaffold.sh` that `claude plugin eval`
  runs; first-party skills hold no `.py` file; and the only file under `.agents`
  is `.agents/plugins/marketplace.json`;
- every registration holds at least one `SKILL.md`, and `archify`, `code-review`,
  `graphify`, `task-orchestration` and `test-strategy` exist and stay
  discoverable;
- the skill routing and import-provenance fixtures under
  `internal/skillcheck/testdata` hold their contracts.

First-party automation must use Go. Script resources supplied by skills are
exempt. Do not add Python tests, environment preparation, or release shell
scripts.

Backend decision fixtures live in `internal/skillcheck/testdata/backend-skills`;
give evaluators raw inputs without their expected decisions. Model evaluations and
fresh discovery are separate from native test assertions; `ci.yml` does not call
hosted models or receive service credentials. Only the separate agent-eval
workflow does, as described under Continuous integration.

### Consultation

`cw consult` lives in internal/consult. Its tests use local HTTP servers and synthetic
records to check configured participation (including `not_configured` and
`no_advisers_configured`), deadlines, partial failures, and evidence-bound
adjudication without service keys:

```sh
GOWORK=off go test ./internal/consult
```

internal/consult/testdata holds goldens for version-4 records, wire requests,
digests and validation messages. They freeze the current record contract, so a
change there is a format change. Only the current version is readable; earlier
record versions are rejected with an error that names version 4.

Consultation records evolve separately and have one current format, version 4,
whose providers are exactly the configured participation set. Model policies must
be version 3. Tests must distinguish `not_configured`, unavailable, partially
refused and all-refused advice, cover `no_advisers_configured` without network
access, and reject task checkpoints that differ from the task's adviser binding.
They must also enforce the adjudication rules for typed evidence, investigations
and untrusted agreement.

### Multilingual evaluation

The multilingual corpus is a skill resource, and `cwdev eval multilingual` is its
runner:

```sh
GOWORK=off go run ./cmd/cwdev eval multilingual
```

Offline tests check complete translations, withheld expected answers,
deterministic scoring and safe bounded execution using synthetic transports. Live
runs (`--live --output-dir PATH`) call only configured providers and report the
others as `not_run` with reason `not_configured`. Run the explicit hosted
evaluation separately, following the
[evaluation guide](../skills/task-orchestration/references/adviser-providers.md#multilingual-evaluation).
Keep its reports outside source, freeze corpus/oracle hashes before requests, and
retain incorrect or unavailable results without automatic retries. A live baseline
measures only its labeled cases; it is not a CI assertion or production-quality
qualification.

### Tasks, diagnostics and metrics

Global project diagnostics, task evidence and measured-outcome aggregation live in
native Go packages. Tests use isolated multi-language repositories and owned child
processes. Protect project-independent dispatch, dirty/untracked/branch source
identities, stale completion rejection, actual failed/cancelled checks,
consultation task/phase/source bindings, safe external record storage and metric
unknowns/comparability. Metadata-only doctor checks never invoke optional tools or
hosted providers. Real-project read-only diagnostics, synthetic acceptance pilots
and productivity comparisons are separate evidence; preserve that distinction in
reports.

Task records use version 3 for the adviser binding, typed worker assignments,
results and coordinator acceptance. An older task record is refused with guidance
to start a new task;
never silently upgrade an old task or permit a version change inside its chain.
Tests must cover wrong task/brief/agent bindings, ownership overlap and unsafe
paths, false check claims, incomplete or cancelled work, finding dispositions,
independent review identity, and stale source/check/worker evidence.
Substantial-change gates apply to new tasks; small and unchanged read-only tasks
keep their existing path. Use project-neutral fixtures, including a non-Go
project, for API and CLI coverage. Worker records remain separate from adviser
records.

Changes to the independent test-engineer workflow also need bounded Codex and
Claude smokes outside CI: give fresh authors only an approved contract and test
scaffold, retain their authored patch identity, then execute their tests against
unseen correct and defective implementations. Inspect initial context, available
tool transcripts and effective model/effort; report enforcement and telemetry
limits explicitly. Exercise red-test handoffs, ownership conflicts, missing
contracts/seams, changed interfaces, contaminated feedback and prohibited assertion
weakening. These are synthetic workflow checks, not measured project-quality
improvements, and they must not modify personal host configuration.

### Graphify

`cw graphify` lives in internal/graphify:

```sh
GOWORK=off go test ./internal/graphify
```

Its synthetic repositories and fake local runtime verify tracked-source scope,
cache identity, stale/incomplete refusal, fixed CLI modes, and environment
isolation without installing Graphify or using hosted models. A real local smoke
check with the pinned runtime verifies extraction and query behavior separately;
it does not establish resolved Go call paths. Keep generated graph caches outside
source. The [runtime setup](operations.md#optional-graphify-runtime) is explicit
per device; `cw` must never provision it.

### Archify

To change a diagram under `docs/diagrams/`, edit its JSON spec, render it with
archify `finalize`, export the SVG from the archify viewer (**Export → SVG ·
Auto**) and commit that SVG with the spec; see
[Diagrams](../CONTRIBUTING.md#diagrams) for the command.

With Node.js available, run the bundled portable Archify tests from its package:

```sh
node --test third_party/archify/test/*.test.mjs
```

This includes available Chrome/Chromium browser gates. Missing browser or optional
generator packages must be reported separately; skipped gates are unverified.
Keep render output outside the package. Archify's update helpers must make no HTTP
requests, cache writes, or installed-file changes. Hash/runtime closure and
preserved licenses are covered by the import-provenance checks in
`internal/skillcheck`; `cw` never provisions installed prerequisites.

## Continuous integration

`.github/workflows/ci.yml` runs on pushes to main and on pull requests:

| Job | Steps |
| --- | --- |
| `Native Go / ubuntu-latest` | gofmt check, `go vet ./...`, `go test -race -count=1 ./...` |
| `Native Go / macos-latest` | gofmt check, `go vet ./...`, `go test -count=1 ./...` |
| `Lint and vulnerabilities` | golangci-lint v2.15.0 with [`.golangci.yml`](../.golangci.yml), for Linux and then `GOOS=darwin`; `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...` |
| `Plugin manifests` | `claude plugin validate .` and `claude plugin validate ./third_party/cc-skills-golang` |

The lint job builds the pinned golangci-lint with the Go toolchain that go.mod
selects, so the linter can load this module. `.golangci.yml` enables govet,
staticcheck, errcheck, ineffassign, unused, gosec and misspell, and gives the
reason for each exclusion; a suppressed finding in code carries a
`//nolint:<linter> // reason` comment. govulncheck fails the job when the
standard library of that toolchain or a dependency has a vulnerability that
the code can reach; the fix is a newer toolchain or module version. The
[contributing guide](../CONTRIBUTING.md#build-and-test) has the local commands.

The race detector runs on Linux only. Only obsolete pull request runs are
cancelled. `ci.yml` makes no release and receives no service credentials.

`.github/workflows/evals.yml` is separate: it runs the paid `claude plugin eval`
suites under `evals/` on demand and weekly, receives the `ANTHROPIC_API_KEY`
secret, and skips with a notice when that secret is not set. It never runs on
pushes or pull requests. See `evals/README.md`.

## Releases

A release is published from a pushed `v*` tag, never from a push to main:

```sh
git tag v3.0.0
git push origin v3.0.0
```

`.github/workflows/release.yml` runs the tests on the tagged commit and then
goreleaser with [`.goreleaser.yaml`](../.goreleaser.yaml). It publishes
`cw_<tag>_<os>_<arch>.tar.gz` for darwin and linux on amd64 and arm64 (each with
`LICENSE`, `THIRD_PARTY.md` and `licenses/`), `SHA256SUMS`, and a changelog from
the commit history. The binaries are built with CGO disabled and trimmed paths, and
`cw version` reports the tag, commit and commit date. Record the user-facing
changes in [CHANGELOG.md](../CHANGELOG.md) before tagging.

Keep credentials, machine state, evaluations, and audit outputs outside source.
Preserve all licenses/notices and maintain THIRD_PARTY.md. The manifest defines
36 registrations and 81 entrypoints.
